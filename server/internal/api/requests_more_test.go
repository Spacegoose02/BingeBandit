package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

// Seasons, 4K, weekly limits, auto-approval, kids asking a parent, the
// plugin's progress reports, problem reports and the cleanup list.
func TestRequestsBeyondTheBasics(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	dana, _ := st.CreateUser("dana", string(hash), true)
	sam, _ := st.CreateUser("sam", string(hash), false)
	ivy, _ := st.CreateUser("ivy", string(hash), false)
	kid, err := st.CreateProfile(sam, "Milo")
	if err != nil {
		t.Fatal(err)
	}
	st.SetKid(kid.ID, true)
	for tok, id := range map[string]int64{"dana": dana.ID, "sam": sam.ID, "ivy": ivy.ID, "milo": kid.ID} {
		st.CreateSession(tok, id, "Web", "")
	}
	shows := &store.Library{Name: "Shows", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(shows)
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(movies)
	for _, u := range []*store.User{sam, ivy, kid} {
		st.SetUserLibraries(u.ID, true, nil)
	}
	plugin := &store.Plugin{ID: "fetcher", Kind: "managed", Enabled: true}
	st.AddPlugin(plugin)
	srv := &Server{Store: st}
	h := srv.Router(context.Background())
	call := func(token, method, path, body string, out any) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Requested-With", "BingeBandit")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}
	notes := func(uid int64) []*store.Notification {
		list, _, _ := st.Notifications(uid, 50)
		return list
	}

	// The server has season 1 of Severance.
	show, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "show", Title: "Severance", MatchKey: "show:severance"})
	st.SetMatch(show, 95396)
	one, two := 1, 2
	season1, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "season", ParentID: show, ShowID: show, Title: "Season 1", SeasonNumber: &one, MatchKey: "season:severance:1"})
	ep, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "episode", ParentID: season1, ShowID: show, Title: "Good News About Hell",
		SeasonNumber: &one, EpisodeNumber: &one, MatchKey: "ep:severance:1:1"})

	// Season 1 is there, so asking for it (or the whole show) is refused; season 2 isn't.
	if c := call("sam", "POST", "/api/requests", `{"kind":"show","tmdbId":95396,"title":"Severance","seasons":[1]}`, nil); c != http.StatusConflict {
		t.Errorf("season on the server: %d", c)
	}
	var req store.TitleRequest
	if c := call("sam", "POST", "/api/requests", `{"kind":"show","tmdbId":95396,"title":"Severance","seasons":[2]}`, &req); c != http.StatusCreated ||
		len(req.Seasons) != 1 || req.Seasons[0] != 2 || req.Status != "pending" {
		t.Fatalf("season 2: %d %+v", c, req)
	}
	// Ivy wants season 3 too: one request covering both.
	var joined store.TitleRequest
	call("ivy", "POST", "/api/requests", `{"kind":"show","tmdbId":95396,"title":"Severance","seasons":[3]}`, &joined)
	if joined.ID != req.ID || len(joined.Seasons) != 2 || joined.People != 2 {
		t.Fatalf("joined: %+v", joined)
	}

	// The plugin reports progress, which everyone sees.
	call("dana", "POST", "/api/admin/requests/"+itoa(req.ID)+"/approve", "", nil)
	var waiting []store.TitleRequest
	call(plugin.Key, "GET", "/api/plugin/v1/requests?status=approved", "", &waiting)
	if len(waiting) != 1 || len(waiting[0].Seasons) != 2 {
		t.Fatalf("plugin sees: %+v", waiting)
	}
	eta := time.Now().Add(20 * time.Minute).Unix()
	if c := call(plugin.Key, "PUT", "/api/plugin/v1/requests/"+itoa(req.ID), `{"status":"sent","stage":"downloading","progress":0.43,"eta":`+itoa(eta)+`}`, nil); c != http.StatusOK {
		t.Fatalf("progress: %d", c)
	}
	if c := call(plugin.Key, "PUT", "/api/plugin/v1/requests/"+itoa(req.ID), `{"status":"sent","stage":"teleporting"}`, nil); c != http.StatusBadRequest {
		t.Errorf("bad stage: %d", c)
	}
	var mine struct {
		Requests []store.TitleRequest `json:"requests"`
	}
	call("ivy", "GET", "/api/requests", "", &mine)
	if len(mine.Requests) != 1 || mine.Requests[0].Stage != "downloading" || mine.Requests[0].Progress != 0.43 || mine.Requests[0].ETA != eta {
		t.Fatalf("ivy sees: %+v", mine.Requests)
	}

	// Season 2 arriving isn't enough; season 3 too is.
	season2, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "season", ParentID: show, ShowID: show, Title: "Season 2", SeasonNumber: &two, MatchKey: "season:severance:2"})
	st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "episode", ParentID: season2, ShowID: show, Title: "Hello, Ms. Cobel", SeasonNumber: &two, EpisodeNumber: &one, MatchKey: "ep:severance:2:1"})
	srv.markArrivedRequests()
	if got, _ := st.TitleRequest(req.ID); got.Status != "sent" {
		t.Fatalf("with season 3 missing: %s", got.Status)
	}
	three := 3
	season3, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "season", ParentID: show, ShowID: show, Title: "Season 3", SeasonNumber: &three, MatchKey: "season:severance:3"})
	st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "episode", ParentID: season3, ShowID: show, Title: "?", SeasonNumber: &three, EpisodeNumber: &one, MatchKey: "ep:severance:3:1"})
	srv.markArrivedRequests()
	if got, _ := st.TitleRequest(req.ID); got.Status != "available" || got.Progress != 0 || got.AvailableAt == 0 {
		t.Fatalf("all seasons in: %+v", got)
	}
	// Asking for season 4 later starts the same request over.
	var again store.TitleRequest
	call("sam", "POST", "/api/requests", `{"kind":"show","tmdbId":95396,"title":"Severance","seasons":[4]}`, &again)
	if again.ID != req.ID || again.Status != "pending" || len(again.Seasons) != 3 {
		t.Fatalf("season 4: %+v", again)
	}

	// 4K is off until an admin turns it on.
	if c := call("ivy", "POST", "/api/requests", `{"kind":"movie","tmdbId":1,"title":"A","fourK":true}`, nil); c != http.StatusForbidden {
		t.Errorf("4K off: %d", c)
	}

	// Weekly limits: one movie for Ivy; the second is refused with when it frees up.
	if c := call("dana", "PUT", "/api/admin/requests/rules/"+itoa(ivy.ID), `{"movieLimit":1,"showLimit":-1}`, nil); c != http.StatusOK {
		t.Fatalf("rules: %d", c)
	}
	call("ivy", "POST", "/api/requests", `{"kind":"movie","tmdbId":10,"title":"Ten"}`, nil)
	var limit struct {
		Error string `json:"error"`
	}
	if c := call("ivy", "POST", "/api/requests", `{"kind":"movie","tmdbId":11,"title":"Eleven"}`, &limit); c != http.StatusTooManyRequests ||
		!strings.Contains(limit.Error, "1 movie requests") {
		t.Errorf("over the limit: %d %+v", c, limit)
	}
	// Joining someone else's movie request is still a new ask; her own isn't.
	if c := call("ivy", "POST", "/api/requests", `{"kind":"movie","tmdbId":10,"title":"Ten"}`, nil); c != http.StatusOK {
		t.Errorf("asking again for her own: %d", c)
	}

	// Auto-approve skips the queue.
	call("dana", "PUT", "/api/admin/requests/rules/"+itoa(sam.ID), `{"autoApprove":true,"movieLimit":-1,"showLimit":-1}`, nil)
	var auto store.TitleRequest
	call("sam", "POST", "/api/requests", `{"kind":"movie","tmdbId":20,"title":"Twenty"}`, &auto)
	if auto.Status != "approved" {
		t.Errorf("auto-approved: %+v", auto)
	}

	// Kids ask a parent once the admin allows it.
	if c := call("milo", "POST", "/api/requests", `{"kind":"movie","tmdbId":30,"title":"Bluey: The Movie"}`, nil); c != http.StatusForbidden {
		t.Errorf("kid with kids off: %d", c)
	}
	st.SetSettings(map[string]string{store.SettingRequestKids: "true"})
	var kidReq store.TitleRequest
	if c := call("milo", "POST", "/api/requests", `{"kind":"movie","tmdbId":30,"title":"Bluey: The Movie"}`, &kidReq); c != http.StatusCreated || kidReq.Status != "parent" {
		t.Fatalf("kid request: %d %+v", c, kidReq)
	}
	if n := notes(sam.ID); len(n) == 0 || !strings.Contains(n[0].Title, "Milo would like Bluey") {
		t.Errorf("parent told: %+v", n)
	}
	for _, n := range notes(dana.ID) {
		if strings.Contains(n.Title, "Bluey") {
			t.Errorf("admin told before the parent said yes: %+v", n)
		}
	}
	var household struct {
		Household []store.TitleRequest `json:"household"`
	}
	call("sam", "GET", "/api/requests", "", &household)
	if len(household.Household) != 1 {
		t.Fatalf("parent's list: %+v", household)
	}
	if c := call("ivy", "POST", "/api/requests/"+itoa(kidReq.ID)+"/parent", "", nil); c != http.StatusNotFound {
		t.Errorf("not her kid: %d", c)
	}
	var yes store.TitleRequest
	if c := call("sam", "POST", "/api/requests/"+itoa(kidReq.ID)+"/parent", "", &yes); c != http.StatusOK || yes.Status != "approved" {
		t.Fatalf("parent says yes (auto-approved parent): %d %+v", c, yes)
	}

	// Problem reports: Ivy reports bad sound; the admin hears and resolves it.
	var p store.ProblemReport
	if c := call("ivy", "POST", "/api/items/"+itoa(ep)+"/problems", `{"kind":"audio","note":"out of sync","position":612}`, &p); c != http.StatusCreated ||
		p.ShowTitle != "Severance" || p.TMDBID != 95396 || p.Status != "open" {
		t.Fatalf("report: %d %+v", c, p)
	}
	if c := call("ivy", "POST", "/api/items/"+itoa(ep)+"/problems", `{"kind":"loud"}`, nil); c != http.StatusBadRequest {
		t.Errorf("bad kind: %d", c)
	}
	if n := notes(dana.ID); len(n) == 0 || !strings.Contains(n[0].Title, "sound problem: Severance S01E01") {
		t.Errorf("admin told of the problem: %+v", n)
	}
	// Refetching needs a plugin that handles it.
	if c := call("dana", "POST", "/api/admin/problems/"+itoa(p.ID), `{"action":"refetch"}`, nil); c != http.StatusConflict {
		t.Errorf("refetch without a plugin: %d", c)
	}
	if c := call("dana", "POST", "/api/admin/problems/"+itoa(p.ID), `{"action":"resolve","reply":"Swapped the file"}`, nil); c != http.StatusOK {
		t.Fatalf("resolve: %d", c)
	}
	if n := notes(ivy.ID); len(n) == 0 || n[0].Body != "Swapped the file" {
		t.Errorf("reporter told: %+v", n)
	}
	var open []store.ProblemReport
	call("dana", "GET", "/api/problems?all=1", "", &open)
	if len(open) != 0 {
		t.Errorf("resolved still listed: %+v", open)
	}

	// Cleanup: a requested movie nobody who asked has watched.
	movie, _ := st.UpsertItem(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Twenty", MatchKey: "movie:twenty"})
	st.SetMatch(movie, 20)
	srv.markArrivedRequests()
	var unwatched struct {
		Requests []store.TitleRequest `json:"requests"`
	}
	call("dana", "GET", "/api/admin/requests/unwatched?days=1", "", &unwatched)
	if len(unwatched.Requests) != 0 {
		t.Errorf("listed before the days are up: %+v", unwatched)
	}
	st.SetRequestAvailableAt(auto.ID, time.Now().AddDate(0, 0, -40).Unix())
	call("dana", "GET", "/api/admin/requests/unwatched?days=30", "", &unwatched)
	if len(unwatched.Requests) != 1 {
		t.Fatalf("unwatched: %+v", unwatched)
	}
	st.StartPlay(&store.Play{UserID: sam.ID, ItemID: movie, Title: "Twenty", Kind: "movie", StartedAt: time.Now().Unix(), StoppedAt: time.Now().Unix()})
	call("dana", "GET", "/api/admin/requests/unwatched?days=30", "", &unwatched)
	if len(unwatched.Requests) != 0 {
		t.Errorf("watched still listed: %+v", unwatched)
	}
}
