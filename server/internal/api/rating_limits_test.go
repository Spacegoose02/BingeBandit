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

	"bingebandit/internal/store"
)

func TestRatingLimits(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	parent, _ := st.CreateUser("dana", "", false)
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	tv := &store.Library{Name: "TV", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(movies)
	st.CreateLibrary(tv)
	kid, _ := st.CreateProfile(parent, "Kids")
	st.SetKid(kid.ID, true)
	teen, _ := st.CreateProfile(parent, "Teen")
	st.CreateSession("dana", parent.ID, "Web", "")
	st.CreateSession("kid", kid.ID, "Web", "")
	st.CreateSession("teen", teen.ID, "Web", "")
	add := func(it *store.Item, rating string) int64 {
		t.Helper()
		id, err := st.UpsertItem(it)
		if err != nil {
			t.Fatal(err)
		}
		if rating != "" {
			if err := st.UpdateMetadata(id, &store.Item{Title: it.Title, ContentRating: rating}); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	moana := add(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Moana", SortTitle: "Moana", MatchKey: "m1"}, "PG")
	alien := add(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Alien", SortTitle: "Alien", MatchKey: "m2"}, "R")
	cars := add(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Cars", SortTitle: "Cars", MatchKey: "m3"}, "Rated G")
	home := add(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Home Video", SortTitle: "Home Video", MatchKey: "m4"}, "")
	bluey := add(&store.Item{LibraryID: tv.ID, Kind: "show", Title: "Bluey", SortTitle: "Bluey", MatchKey: "s1"}, "TV-Y")
	season := add(&store.Item{LibraryID: tv.ID, Kind: "season", ParentID: bluey, ShowID: bluey, Title: "Season 1", MatchKey: "s1/1"}, "")
	ep := add(&store.Item{LibraryID: tv.ID, Kind: "episode", ParentID: season, ShowID: bluey, Title: "Magic Xylophone", MatchKey: "s1/1/1"}, "")
	got := add(&store.Item{LibraryID: tv.ID, Kind: "show", Title: "Dragons", SortTitle: "Dragons", MatchKey: "s2"}, "TV-MA")
	gotEp := add(&store.Item{LibraryID: tv.ID, Kind: "episode", ParentID: got, ShowID: got, Title: "Pilot", MatchKey: "s2/1/1"}, "")

	h := (&Server{Store: st}).Router(context.Background())
	call := func(token, method, path, body string) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	sees := func(token string, items ...int64) map[int64]bool {
		t.Helper()
		out := map[int64]bool{}
		for _, it := range items {
			out[it] = call(token, "GET", "/api/items/"+id(it), "") == http.StatusOK
		}
		return out
	}
	all := []int64{moana, alien, cars, home, bluey, season, ep, got, gotEp}

	// No limit: everything.
	for it, ok := range sees("kid", all...) {
		if !ok {
			t.Fatalf("kid can't see %d before any limit", it)
		}
	}
	// G / TV-Y: Cars and Bluey with its episodes; nothing unrated.
	if c := call("dana", "PUT", "/api/household/"+id(kid.ID), `{"maxRating":1}`); c != http.StatusNoContent {
		t.Fatalf("set limit: %d", c)
	}
	want := map[int64]bool{moana: false, alien: false, cars: true, home: false, bluey: true, season: true, ep: true, got: false, gotEp: false}
	for it, ok := range sees("kid", all...) {
		if ok != want[it] {
			t.Errorf("G limit: item %d visible=%v", it, ok)
		}
	}
	// The library list for the kid follows the limit too.
	req := httptest.NewRequest("GET", "/api/libraries/"+id(movies.ID)+"/items", nil)
	req.Header.Set("Authorization", "Bearer kid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Cars") || strings.Contains(rec.Body.String(), "Alien") {
		t.Errorf("library list ignores the limit: %d %s", rec.Code, rec.Body)
	}
	// A title the parent approves shows whatever its rating.
	if c := call("dana", "PUT", "/api/items/"+id(home)+"/kids", `{"userIds":[`+id(kid.ID)+`]}`); c != http.StatusNoContent {
		t.Fatalf("approve: %d", c)
	}
	if !sees("kid", home)[home] {
		t.Error("approved unrated title hidden by the limit")
	}
	// The account and other profiles are unaffected.
	if !sees("dana", alien)[alien] || !sees("teen", alien)[alien] {
		t.Error("limit leaked to other members")
	}

	// An admin limits the account to PG-13: its profiles can't go past it.
	if err := st.SetMaxRating(parent.ID, 4); err != nil {
		t.Fatal(err)
	}
	if sees("dana", alien)[alien] || sees("teen", alien)[alien] {
		t.Error("account limit not applied to the account or its profile")
	}
	call("dana", "PUT", "/api/household/"+id(teen.ID), `{"maxRating":0}`)
	var members []member
	req = httptest.NewRequest("GET", "/api/household", nil)
	req.Header.Set("Authorization", "Bearer dana")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	json.Unmarshal(rec.Body.Bytes(), &members)
	for _, m := range members {
		if m.ID == teen.ID && m.MaxRating != 4 {
			t.Errorf("profile set looser than its account: %d", m.MaxRating)
		}
	}
	if sees("teen", alien)[alien] {
		t.Error("profile sees past the account's limit")
	}
}

func TestDiscoverHiddenFromKids(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	parent, _ := st.CreateUser("dana", "", false)
	kid, _ := st.CreateProfile(parent, "Kids")
	st.SetKid(kid.ID, true)
	st.CreateSession("dana", parent.ID, "Web", "")
	st.CreateSession("kid", kid.ID, "Web", "")
	h := (&Server{Store: st}).Router(context.Background())
	for token, want := range map[string]int{"dana": http.StatusOK, "kid": http.StatusForbidden} {
		req := httptest.NewRequest("GET", "/api/discover", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: discover %d, want %d", token, rec.Code, want)
		}
	}
}
