package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

func TestTitleRequests(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	dana, _ := st.CreateUser("dana", string(hash), true)
	sam, _ := st.CreateUser("sam", string(hash), false)
	ivy, _ := st.CreateUser("ivy", string(hash), false)
	kid, _ := st.CreateUser("kid", string(hash), false)
	st.SetKid(kid.ID, true)
	for tok, id := range map[string]int64{"dana": dana.ID, "sam": sam.ID, "ivy": ivy.ID, "kid": kid.ID} {
		st.CreateSession(tok, id, "Web", "")
	}
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(movies)
	st.SetUserLibraries(sam.ID, true, nil)
	st.SetUserLibraries(ivy.ID, true, nil)
	plugin := &store.Plugin{ID: "fetcher", Kind: "managed", Enabled: true}
	if err := st.AddPlugin(plugin); err != nil {
		t.Fatal(err)
	}
	h := (&Server{Store: st}).Router(context.Background())
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
	unread := func(uid int64) []*store.Notification {
		t.Helper()
		list, _, _ := st.Notifications(uid, 50)
		return list
	}

	// Sam asks for a movie; the admin hears about it.
	parasite := `{"kind":"movie","tmdbId":496243,"title":"Parasite","year":2019,"poster":"/api/images/tmdb/abc.jpg","note":"for movie night"}`
	var req store.TitleRequest
	if c := call("sam", "POST", "/api/requests", parasite, &req); c != http.StatusCreated || req.Status != "pending" || req.Poster != "/api/images/tmdb/abc.jpg" || !req.Mine {
		t.Fatalf("request: %d %+v", c, req)
	}
	if n := unread(dana.ID); len(n) != 1 || n[0].Kind != "request" || !strings.Contains(n[0].Title, "sam requested Parasite (2019)") {
		t.Fatalf("admin notification: %+v", n)
	}
	// Ivy asks for the same one: she joins Sam's request.
	var again store.TitleRequest
	if c := call("ivy", "POST", "/api/requests", parasite, &again); c != http.StatusOK || again.ID != req.ID || again.People != 2 {
		t.Fatalf("second request: %d %+v", c, again)
	}
	// Kids can't ask, and nobody can with requests turned off.
	if c := call("kid", "POST", "/api/requests", parasite, nil); c != http.StatusForbidden {
		t.Errorf("kid request: %d", c)
	}
	// A bad poster is dropped, not stored.
	var odd store.TitleRequest
	call("sam", "POST", "/api/requests", `{"kind":"show","tmdbId":1396,"title":"Breaking Bad","poster":"https://evil.example/x.jpg"}`, &odd)
	if odd.Poster != "" || odd.Kind != "show" {
		t.Errorf("poster from elsewhere: %+v", odd)
	}

	// Lists: each person sees theirs, admins can see all.
	var mine struct {
		Requests   []store.TitleRequest `json:"requests"`
		CanRequest bool                 `json:"canRequest"`
		Handlers   []string             `json:"handlers"`
	}
	call("ivy", "GET", "/api/requests", "", &mine)
	if len(mine.Requests) != 1 || !mine.CanRequest || mine.Handlers != nil {
		t.Errorf("ivy's requests: %+v", mine)
	}
	call("dana", "GET", "/api/requests?all=1", "", &mine)
	if len(mine.Requests) != 2 || mine.Handlers == nil {
		t.Errorf("all requests: %+v", mine)
	}
	call("kid", "GET", "/api/requests", "", &mine)
	if mine.CanRequest {
		t.Error("kids can request")
	}

	// Only admins approve; the fetching plugin sees it then.
	if c := call("sam", "POST", "/api/admin/requests/"+itoa(req.ID)+"/approve", "", nil); c != http.StatusForbidden {
		t.Errorf("sam approving: %d", c)
	}
	var waiting []store.TitleRequest
	call(plugin.Key, "GET", "/api/plugin/v1/requests?status=approved", "", &waiting)
	if len(waiting) != 0 {
		t.Errorf("approved before approving: %+v", waiting)
	}
	if c := call("dana", "POST", "/api/admin/requests/"+itoa(req.ID)+"/approve", "", nil); c != http.StatusOK {
		t.Fatalf("approve: %d", c)
	}
	// Approved requests can't be withdrawn.
	if c := call("sam", "DELETE", "/api/requests/"+itoa(req.ID), "", nil); c != http.StatusConflict {
		t.Errorf("withdraw approved: %d", c)
	}
	call(plugin.Key, "GET", "/api/plugin/v1/requests?status=approved", "", &waiting)
	if len(waiting) != 1 || waiting[0].TMDBID != 496243 {
		t.Fatalf("waiting for the plugin: %+v", waiting)
	}
	if c := call(plugin.Key, "PUT", "/api/plugin/v1/requests/"+itoa(req.ID), `{"status":"sent","link":"javascript:alert(1)"}`, nil); c != http.StatusBadRequest {
		t.Errorf("bad link: %d", c)
	}
	var sent store.TitleRequest
	if c := call(plugin.Key, "PUT", "/api/plugin/v1/requests/"+itoa(req.ID), `{"status":"sent","link":"http://cinemask.local/movie/496243"}`, &sent); c != http.StatusOK ||
		sent.Status != "sent" || sent.Link != "http://cinemask.local/movie/496243" {
		t.Fatalf("sent: %d %+v", c, sent)
	}

	// The movie shows up: everyone who asked hears, with a link to it.
	movie, _ := st.UpsertItem(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Parasite", Year: 2019, MatchKey: "movie:parasite:2019"})
	st.SetMatch(movie, 496243)
	(&Server{Store: st}).markArrivedRequests()
	if got, _ := st.TitleRequest(req.ID); got.Status != "available" {
		t.Fatalf("after it arrived: %+v", got)
	}
	for _, uid := range []int64{sam.ID, ivy.ID} {
		n := unread(uid)
		if len(n) == 0 || n[0].Link != "/item/"+itoa(movie) || !strings.Contains(n[0].Title, "ready to watch") {
			t.Errorf("user %d told: %+v", uid, n)
		}
	}
	// A title already on the server can't be requested again.
	if c := call("ivy", "POST", "/api/requests", parasite, nil); c != http.StatusConflict {
		t.Errorf("request what's there: %d", c)
	}
	// The plugin can't change a request that's no longer waiting on it.
	if c := call(plugin.Key, "PUT", "/api/plugin/v1/requests/"+itoa(req.ID), `{"status":"failed"}`, nil); c != http.StatusConflict {
		t.Errorf("update available: %d", c)
	}

	// Declining tells the person why; they can then withdraw it.
	if c := call("dana", "POST", "/api/admin/requests/"+itoa(odd.ID)+"/decline", `{"reason":"not in 4K yet"}`, nil); c != http.StatusOK {
		t.Fatalf("decline: %d", c)
	}
	if n := unread(sam.ID); n[0].Body != "not in 4K yet" || !strings.Contains(n[0].Title, "declined") {
		t.Errorf("decline notification: %+v", n[0])
	}
	if c := call("ivy", "DELETE", "/api/requests/"+itoa(odd.ID), "", nil); c != http.StatusNotFound {
		t.Errorf("withdraw someone else's: %d", c)
	}
	if c := call("sam", "DELETE", "/api/requests/"+itoa(odd.ID), "", nil); c != http.StatusNoContent {
		t.Errorf("withdraw: %d", c)
	}
	if _, err := st.TitleRequest(odd.ID); err != store.ErrNotFound {
		t.Errorf("withdrawn request kept: %v", err)
	}

	// An admin's own request is approved straight away.
	var own store.TitleRequest
	call("dana", "POST", "/api/requests", `{"kind":"movie","tmdbId":603,"title":"The Matrix","year":1999}`, &own)
	if own.Status != "approved" {
		t.Errorf("admin's request: %+v", own)
	}

	st.SetSettings(map[string]string{store.SettingRequests: "false"})
	if c := call("sam", "POST", "/api/requests", `{"kind":"movie","tmdbId":680,"title":"Pulp Fiction"}`, nil); c != http.StatusForbidden {
		t.Errorf("requests off: %d", c)
	}
}
