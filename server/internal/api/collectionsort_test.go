package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

// The TV apps' Sort button orders a collection for the viewer without
// changing its saved sort.
func TestCollectionViewerSort(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("admin", "x", true)
	st.CreateSession("admin-token", admin.ID, "TV", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	for _, it := range []*store.Item{
		{Title: "Alien", ReleaseDate: "1979-05-25", Rating: 8.5},
		{Title: "Cube", ReleaseDate: "1997-09-09", Rating: 9.1},
		{Title: "Brazil", ReleaseDate: "2000-02-20", Rating: 7.9},
	} {
		it.LibraryID, it.Kind, it.MatchKey = lib.ID, "movie", "movie:"+strings.ToLower(it.Title)
		id, err := st.UpsertItem(it)
		if err != nil {
			t.Fatal(err)
		}
		st.UpdateMetadata(id, it)
		st.SetItemCollections(id, lib.ID, []string{"Sci-Fi"})
	}
	cols, _ := st.LibraryCollections(lib.ID)
	if len(cols) != 1 {
		t.Fatalf("collections: %d", len(cols))
	}
	h := (&Server{Store: st, DataDir: dir}).Router(context.Background())
	get := func(query string) (string, []string) {
		req := httptest.NewRequest("GET", "/api/collections/"+itoa(cols[0].ID)+"?limit=50"+query, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", query, rec.Code, rec.Body)
		}
		var out struct {
			Sort  string `json:"sort"`
			Items []struct{ Title string }
		}
		json.Unmarshal(rec.Body.Bytes(), &out)
		var titles []string
		for _, it := range out.Items {
			titles = append(titles, it.Title)
		}
		return out.Sort, titles
	}
	for _, c := range []struct{ query, sort, want string }{
		{"", "", "Alien,Cube,Brazil"},
		{"&sort=title", "title", "Alien,Brazil,Cube"},
		{"&sort=release_desc", "release_desc", "Brazil,Cube,Alien"},
		{"&sort=rating", "rating", "Cube,Alien,Brazil"},
		{"&sort=bogus", "", "Alien,Cube,Brazil"},
	} {
		sort, titles := get(c.query)
		if sort != c.sort || strings.Join(titles, ",") != c.want {
			t.Errorf("%q: sort %q, %v", c.query, sort, titles)
		}
	}
	// The saved sort still applies, and "release" overrides it back.
	st.SetCollectionSort(cols[0].ID, "release_desc")
	if sort, titles := get(""); sort != "release_desc" || strings.Join(titles, ",") != "Brazil,Cube,Alien" {
		t.Errorf("saved: %q %v", sort, titles)
	}
	if sort, titles := get("&sort=release"); sort != "" || strings.Join(titles, ",") != "Alien,Cube,Brazil" {
		t.Errorf("release: %q %v", sort, titles)
	}
}
