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

func TestFavorites(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	sam, _ := st.CreateUser("sam", string(hash), false)
	other, _ := st.CreateUser("other", string(hash), false)
	st.CreateSession("sam", sam.ID, "Web", "")
	st.CreateSession("other", other.ID, "Web", "")
	st.SetUserLibraries(sam.ID, true, nil)
	st.SetUserLibraries(other.ID, true, nil)
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(movies)
	shows := &store.Library{Name: "Shows", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(shows)
	movie, _ := st.UpsertItem(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Up", Year: 2009, MatchKey: "movie:up:2009"})
	show, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "show", Title: "Bluey", MatchKey: "show:bluey"})
	season, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "season", Title: "Season 1", ParentID: show, ShowID: show, MatchKey: "season:bluey:1"})
	one := 1
	episode, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "episode", Title: "Magic Xylophone", ParentID: season, ShowID: show,
		SeasonNumber: &one, EpisodeNumber: &one, MatchKey: "episode:bluey:1:1"})
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

	// Movies, shows and episodes can be favorites; seasons can't.
	for _, id := range []int64{movie, show, episode} {
		if c := call("sam", "POST", "/api/favorites/"+itoa(id), "", nil); c != http.StatusNoContent {
			t.Fatalf("favorite %d: %d", id, c)
		}
	}
	if c := call("sam", "POST", "/api/favorites/"+itoa(season), "", nil); c != http.StatusBadRequest {
		t.Errorf("favorite a season: %d, want 400", c)
	}
	var favs []*store.Item
	call("sam", "GET", "/api/favorites", "", &favs)
	if len(favs) != 3 {
		t.Fatalf("favorites: %d, want 3", len(favs))
	}
	var it struct {
		Item store.Item `json:"item"`
	}
	call("sam", "GET", "/api/items/"+itoa(episode), "", &it)
	if !it.Item.Favorite {
		t.Error("the episode should say it's a favorite")
	}
	call("other", "GET", "/api/favorites", "", &favs)
	if len(favs) != 0 {
		t.Error("favorites are per person")
	}

	// Home shows them only once the Favorites tab is pinned.
	var home struct {
		Rows []struct {
			ID    string        `json:"id"`
			Items []*store.Item `json:"items"`
		} `json:"rows"`
	}
	favRow := func() int {
		home.Rows = nil
		call("sam", "GET", "/api/home", "", &home)
		for _, r := range home.Rows {
			if r.ID == "favorites" {
				return len(r.Items)
			}
		}
		return -1
	}
	if n := favRow(); n != -1 {
		t.Errorf("Favorites row before pinning has %d", n)
	}
	var prefs map[string]any
	call("sam", "GET", "/api/me/prefs", "", &prefs)
	prefs["pinnedTabs"] = []string{"favorites"}
	b, _ := json.Marshal(prefs)
	if c := call("sam", "PUT", "/api/me/prefs", string(b), nil); c != http.StatusOK && c != http.StatusNoContent {
		t.Fatalf("pin favorites: %d", c)
	}
	if n := favRow(); n != 3 {
		t.Errorf("Favorites row after pinning has %d, want 3", n)
	}

	call("sam", "DELETE", "/api/favorites/"+itoa(movie), "", nil)
	if n := favRow(); n != 2 {
		t.Errorf("after removing one, the row has %d, want 2", n)
	}
}
