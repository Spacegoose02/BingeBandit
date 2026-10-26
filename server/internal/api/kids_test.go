package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"bingebandit/internal/livetv"
	"bingebandit/internal/store"
)

func TestKidChannels(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	parent, err := st.CreateUser("parent", "", false)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.CreateProfile(parent, "Kids")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetKid(kid.ID, true); err != nil {
		t.Fatal(err)
	}
	adminKey, _, _ := st.CreateAPIKey(parent.ID, "parent") // a plain account, so it sees everything
	kidKey, _, err := st.CreateAPIKey(kid.ID, "Plex")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []*store.Channel{
		{Number: 1, Name: "News", Group: "News", Enabled: true},
		{Number: 2, Name: "Cartoons", Group: "Kids", Enabled: true},
		{Number: 3, Name: "PBS", Group: "Local", Enabled: true},
		{Number: 4, Name: "Toons West", Group: "Kids", Enabled: true},
	} {
		if err := st.CreateChannel(c, nil); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{Store: st, Live: livetv.NewManager(st, "ffmpeg", "ffprobe", t.TempDir())}
	h := s.Router(context.Background())
	names := func(key string) []string {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/live/channels", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("channels: %d %s", rec.Code, rec.Body)
		}
		var list []liveChannel
		json.Unmarshal(rec.Body.Bytes(), &list)
		out := []string{}
		for _, c := range list {
			out = append(out, c.Name)
		}
		return out
	}

	if got := names(kidKey); len(got) != 0 {
		t.Fatalf("kid sees channels before any are approved: %v", got)
	}
	if err := st.SetKidGroup("Kids", true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetKidChannels([]int64{3}, true); err != nil {
		t.Fatal(err)
	}
	if got := names(kidKey); len(got) != 3 || got[0] != "Cartoons" || got[1] != "PBS" || got[2] != "Toons West" {
		t.Fatalf("kid sees %v", got)
	}
	if got := names(adminKey); len(got) != 4 {
		t.Fatalf("parent sees %v", got)
	}
	// Unapproved channels can't be opened directly either, and the Plex
	// tuner lineup follows the same rule.
	if _, err := st.Channel(kid.ID, 1); err != store.ErrNotFound {
		t.Fatalf("kid opened an unapproved channel: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/live/tuner/"+kidKey+"/lineup.json", nil))
	var lineup []struct{ GuideName string }
	json.Unmarshal(rec.Body.Bytes(), &lineup)
	if len(lineup) != 3 {
		t.Fatalf("kid's tuner lineup: %s", rec.Body)
	}

	groups, ids, err := st.KidApprovals()
	if err != nil || len(groups) != 3 || len(ids) != 1 || ids[0] != 3 {
		t.Fatalf("approvals: %+v %v %v", groups, ids, err)
	}
	for _, g := range groups {
		if g.Approved != (g.Name == "Kids") {
			t.Errorf("group %+v", g)
		}
	}
	if err := st.SetKid(kid.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := names(kidKey); len(got) != 4 {
		t.Fatalf("after kid mode is off: %v", got)
	}
}

func TestKidTitles(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	parent, _ := st.CreateUser("dana", "", false)
	other, _ := st.CreateUser("sam", "", false)
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	tv := &store.Library{Name: "TV", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(movies)
	st.CreateLibrary(tv)
	kid, _ := st.CreateProfile(parent, "Kids")
	st.SetKid(kid.ID, true)
	st.SetUserLibraries(kid.ID, false, nil)
	st.CreateSession("dana", parent.ID, "Web", "")
	st.CreateSession("sam", other.ID, "Web", "")
	st.CreateSession("kid", kid.ID, "Web", "")
	add := func(it *store.Item) int64 {
		t.Helper()
		id, err := st.UpsertItem(it)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	moana := add(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Moana", SortTitle: "Moana", MatchKey: "m1"})
	alien := add(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Alien", SortTitle: "Alien", MatchKey: "m2"})
	bluey := add(&store.Item{LibraryID: tv.ID, Kind: "show", Title: "Bluey", SortTitle: "Bluey", MatchKey: "s1"})
	season := add(&store.Item{LibraryID: tv.ID, Kind: "season", ParentID: bluey, ShowID: bluey, Title: "Season 1", MatchKey: "s1/1"})
	ep := add(&store.Item{LibraryID: tv.ID, Kind: "episode", ParentID: season, ShowID: bluey, Title: "Magic Xylophone", MatchKey: "s1/1/1"})

	h := (&Server{Store: st}).Router(context.Background())
	call := func(token, method, path, body string, out any) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}
	id := func(n int64) string { return strconv.FormatInt(n, 10) }

	if c := call("kid", "GET", "/api/items/"+id(moana), "", nil); c != http.StatusNotFound {
		t.Fatalf("kid sees an unapproved movie: %d", c)
	}
	var kids []kidTitleState
	if c := call("dana", "GET", "/api/items/"+id(moana)+"/kids", "", &kids); c != http.StatusOK || len(kids) != 1 || kids[0].ID != kid.ID || kids[0].Approved {
		t.Fatalf("kids for parent: %d %+v", c, kids)
	}
	call("sam", "GET", "/api/items/"+id(moana)+"/kids", "", &kids)
	if len(kids) != 0 {
		t.Fatalf("another account sees dana's kid: %+v", kids)
	}
	// Someone else can't approve for dana's kid.
	call("sam", "PUT", "/api/items/"+id(alien)+"/kids", `{"userIds":[`+id(kid.ID)+`]}`, nil)
	for _, it := range []int64{moana, bluey} {
		if c := call("dana", "PUT", "/api/items/"+id(it)+"/kids", `{"userIds":[`+id(kid.ID)+`]}`, nil); c != http.StatusNoContent {
			t.Fatalf("approve: %d", c)
		}
	}
	if c := call("kid", "PUT", "/api/items/"+id(alien)+"/kids", `{"userIds":[`+id(kid.ID)+`]}`, nil); c != http.StatusNotFound {
		t.Fatalf("kid approved for itself: %d", c)
	}

	var list []store.Item
	call("kid", "GET", "/api/kids/titles?kind=movie", "", &list)
	if len(list) != 1 || list[0].Title != "Moana" {
		t.Fatalf("kids movies: %+v", list)
	}
	call("kid", "GET", "/api/kids/titles?kind=show", "", &list)
	if len(list) != 1 || list[0].Title != "Bluey" {
		t.Fatalf("kids shows: %+v", list)
	}
	// An approved show brings its seasons and episodes; nothing else opens up.
	for _, it := range []int64{moana, bluey, season, ep} {
		if c := call("kid", "GET", "/api/items/"+id(it), "", nil); c != http.StatusOK {
			t.Errorf("kid can't open approved item %d: %d", it, c)
		}
	}
	if c := call("kid", "GET", "/api/items/"+id(alien), "", nil); c != http.StatusNotFound {
		t.Fatalf("kid sees Alien: %d", c)
	}
	// Taking it away.
	call("dana", "PUT", "/api/items/"+id(moana)+"/kids", `{"userIds":[]}`, nil)
	if c := call("kid", "GET", "/api/items/"+id(moana), "", nil); c != http.StatusNotFound {
		t.Fatalf("approval not removed: %d", c)
	}
}
