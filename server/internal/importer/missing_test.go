package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

func TestCollectionMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tmdb/collection/10":
			w.Write([]byte(`{"parts":[{"id":11,"title":"Star Wars","release_date":"1977-05-25"},{"id":1891,"title":"The Empire Strikes Back","release_date":"1980-05-20"},{"id":1892,"title":"Return of the Jedi","release_date":"1983-05-25"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSettings(map[string]string{store.SettingTMDBKey: "tk"})
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	kids := &store.Library{Name: "Kids Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(kids)
	sw, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Star Wars", Year: 1977, TMDBID: 11, MatchKey: "m:sw"})
	// In another library, so not missing.
	st.UpsertItem(&store.Item{LibraryID: kids.ID, Kind: "movie", Title: "Return of the Jedi", Year: 1983, TMDBID: 1892, MatchKey: "k:rotj"})
	if err := st.LinkTMDBCollection(sw, lib.ID, 10, "Star Wars Collection", "", ""); err != nil {
		t.Fatal(err)
	}
	cols, _ := st.LibraryCollections(lib.ID)
	var c *store.Collection
	for _, x := range cols {
		if x.TMDBID == 10 {
			c = x
		}
	}
	if c == nil {
		t.Fatal("no TMDB collection")
	}
	l := NewLists(st)
	l.tmdbBase, l.client = srv.URL+"/tmdb", srv.Client()
	missing, unknown, err := l.Missing(context.Background(), c)
	if err != nil || unknown != 0 || len(missing) != 1 || missing[0].TMDBID != 1891 || missing[0].Kind != "movie" || missing[0].Year != 1980 {
		t.Fatalf("missing: %v %d %+v", err, unknown, missing)
	}
	// A hand-picked collection has nothing to go and get.
	hand, _ := st.CreateCollection(lib.ID, "Favourites", store.SmartRules{})
	if missing, _, err := l.Missing(context.Background(), hand); err != nil || len(missing) != 0 {
		t.Fatalf("hand-picked: %v %+v", err, missing)
	}
}
