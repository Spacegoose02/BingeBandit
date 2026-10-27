package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

// A collection's page lists the same-named collections in the other
// libraries the viewer may see, movies and shows alike.
func TestSameNamedCollections(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("admin", "x", true)
	st.CreateSession("admin-token", admin.ID, "TV", "")
	kid, _ := st.CreateUser("kid", "x", false)
	st.CreateSession("kid-token", kid.ID, "TV", "")
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	kids := &store.Library{Name: "Kids Movies", Kind: "movies", Paths: []string{dir}}
	shows := &store.Library{Name: "TV Shows", Kind: "shows", Paths: []string{dir}}
	for _, l := range []*store.Library{movies, kids, shows} {
		st.CreateLibrary(l)
	}
	st.SetUserLibraries(kid.ID, false, []int64{kids.ID})
	add := func(lib *store.Library, kind, title string) {
		id, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: kind, Title: title, MatchKey: kind + ":" + title})
		st.SetItemCollections(id, lib.ID, []string{"Marvel Universe"})
	}
	add(movies, "movie", "Iron Man")
	add(kids, "movie", "Spider-Man")
	add(shows, "show", "Loki")
	mc, _ := st.LibraryCollections(movies.ID)
	main := mc[0]
	h := (&Server{Store: st, DataDir: dir}).Router(context.Background())
	others := func(token string, id int64) []string {
		req := httptest.NewRequest("GET", "/api/collections/"+itoa(id), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			return []string{rec.Result().Status}
		}
		var body struct {
			Others []struct {
				Library string `json:"library"`
				Items   []struct {
					Title string `json:"title"`
				} `json:"items"`
			} `json:"others"`
		}
		json.Unmarshal(rec.Body.Bytes(), &body)
		out := []string{}
		for _, o := range body.Others {
			for _, it := range o.Items {
				out = append(out, o.Library+": "+it.Title)
			}
		}
		return out
	}
	if got := others("admin-token", main.ID); len(got) != 2 || got[0] != "Kids Movies: Spider-Man" || got[1] != "TV Shows: Loki" {
		t.Fatalf("admin: %v", got)
	}
	// The kid only has Kids Movies: no Movies page, and no rows from it.
	if got := others("kid-token", main.ID); len(got) != 1 || got[0] != "404 Not Found" {
		t.Fatalf("kid on Movies: %v", got)
	}
	cols, _ := st.SameNamedCollections(main.ID)
	for _, c := range cols {
		if c.LibraryID == kids.ID {
			if got := others("kid-token", c.ID); len(got) != 0 {
				t.Fatalf("kid on Kids Movies: %v", got)
			}
		}
	}
}
