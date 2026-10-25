package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

const letterboxdFeed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:letterboxd="https://letterboxd.com" xmlns:tmdb="https://themoviedb.org">
<channel><title>Horror</title>
<item><title>Alien, 1979 - ★★★★★</title><letterboxd:filmTitle>Alien</letterboxd:filmTitle><letterboxd:filmYear>1979</letterboxd:filmYear><tmdb:movieId>348</tmdb:movieId></item>
<item><title>The Thing (1982)</title></item>
<item><title>Not In The Library, 2001</title></item>
</channel></rss>`

func TestListCollections(t *testing.T) {
	var feed string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lists/ann/horror/json": // public, no key
			w.Write([]byte(`[{"id":348,"title":"Alien","release_year":1979,"mediatype":"movie"},{"id":1,"title":"Bluey","mediatype":"show"}]`))
		case "/lists/ann/horror/items":
			if r.URL.Query().Get("apikey") != "k" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"movies":[{"id":0,"title":"The Thing","release_year":1982,"mediatype":"movie","ids":{"tmdb":1091}}],"shows":[]}`))
		case "/feed":
			w.Write([]byte(feed))
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
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	kids := &store.Library{Name: "Kids Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(kids)
	add := func(lib int64, title string, year int, tmdb int64) int64 {
		id, err := st.UpsertItem(&store.Item{LibraryID: lib, Kind: "movie", Title: title, Year: year, TMDBID: tmdb, MatchKey: "movie:" + title + filepath.Base(t.TempDir())})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	st.UpsertItem(&store.Item{LibraryID: kids.ID, Kind: "movie", Title: "Alien", Year: 1979, TMDBID: 348, MatchKey: "kids:alien"})
	alien, thing, hand := add(lib.ID, "Alien", 1979, 348), add(lib.ID, "The Thing", 1982, 1091), add(lib.ID, "Hand Picked", 2000, 5)

	l := NewLists(st)
	l.mdblistSite, l.mdblistBase = srv.URL, srv.URL
	l.client = srv.Client()
	members := func(id int64) map[int64]bool {
		items, _ := st.CollectionMembers(0, id)
		out := map[int64]bool{}
		for _, it := range items {
			out[it.ID] = true
		}
		return out
	}
	sync := func(id int64, url string) *store.Collection {
		t.Helper()
		st.SetCollectionList(id, url)
		c, _ := st.Collection(id)
		l.Sync(context.Background(), c)
		c, _ = st.Collection(id)
		return c
	}

	// MDBList without a key reads the public JSON, and only this library's Alien joins.
	col, _, _ := st.EnsureCollection(lib.ID, "Horror", "", []int64{hand})
	c := sync(col, "https://mdblist.com/lists/ann/horror")
	if m := members(col); len(m) != 2 || !m[alien] || !m[hand] || c.ListError != "" || c.ListSyncedAt == 0 {
		t.Fatalf("public list: members %v, error %q", m, c.ListError)
	}
	// With a key it uses the API, and titles the list dropped leave; hand-picked ones stay.
	st.SetSettings(map[string]string{store.SettingMDBListKey: "k"})
	sync(col, "https://mdblist.com/lists/ann/horror/")
	if m := members(col); len(m) != 2 || !m[thing] || !m[hand] {
		t.Fatalf("API list: members %v", m)
	}
	// A bad key keeps the members and says why.
	st.SetSettings(map[string]string{store.SettingMDBListKey: "bad"})
	if c := sync(col, "https://mdblist.com/lists/ann/horror"); c.ListError == "" || len(members(col)) != 2 {
		t.Errorf("bad key: error %q, members %v", c.ListError, members(col))
	}

	// RSS: Letterboxd's TMDB ids, and "Title (Year)" titles.
	feed = letterboxdFeed
	if c := sync(col, srv.URL+"/feed"); c.ListError != "" {
		t.Fatal(c.ListError)
	}
	if m := members(col); len(m) != 3 || !m[alien] || !m[thing] || !m[hand] {
		t.Fatalf("rss: members %v", m)
	}
	if msg, err := l.SyncAll(context.Background()); err != nil || msg != "1 list collections synced" {
		t.Errorf("SyncAll: %q %v", msg, err)
	}
	// Not following the list any more keeps what it added.
	st.SetCollectionList(col, "")
	feed = "<rss><channel></channel></rss>"
	l.SyncAll(context.Background())
	if len(members(col)) != 3 {
		t.Errorf("unfollowed list changed members: %v", members(col))
	}
}
