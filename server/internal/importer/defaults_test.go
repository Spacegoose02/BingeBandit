package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

func TestDefaultCollections(t *testing.T) {
	failWikidata := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if failWikidata {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.Header.Get("User-Agent") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.Contains(q, `"Academy Award for Best Picture"@en`) {
			w.Write([]byte(`{"results":{"bindings":[
				{"imdb":{"value":"tt0068646"},"tmdbMovie":{"value":"238"},"year":{"value":"1973"}},
				{"tmdbMovie":{"value":"13"},"year":{"value":"1995"}},
				{"imdb":{"value":"tt9999999"}}]}}`))
			return
		}
		w.Write([]byte(`{"results":{"bindings":[]}}`))
	}))
	defer srv.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	add := func(title string, year int, studio, imdb string, tmdb int64, genres ...string) int64 {
		t.Helper()
		id, err := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, MatchKey: "movie:" + title})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.UpdateMetadata(id, &store.Item{Title: title, Year: year, Studio: studio, Genres: genres, IMDbID: imdb, TMDBID: tmdb}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	godfather := add("The Godfather", 1972, "Paramount", "tt0068646", 238, "Drama", "Crime")
	gump := add("Forrest Gump", 1994, "Paramount", "", 13, "Drama", "Comedy")
	add("Chinatown", 1974, "Paramount", "", 829, "Drama")
	add("Jaws", 1975, "Universal", "", 578, "Thriller")
	// The admin's own "1970s" collection is left alone, not replaced by a decade.
	st.CreateCollection(lib.ID, "1970s", store.SmartRules{LabelRule: "mine"})

	d := NewDefaults(st)
	d.wikidata, d.client = srv.URL, srv.Client()
	build := func() string {
		t.Helper()
		msg, err := d.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return msg
	}
	names := func() map[string]*store.Collection {
		cols, _ := st.LibraryCollections(lib.ID)
		out := map[string]*store.Collection{}
		for _, c := range cols {
			out[c.Name] = c
		}
		return out
	}

	// Everything off: nothing made.
	build()
	if n := len(names()); n != 1 {
		t.Fatalf("%d collections with defaults off", n)
	}

	st.SetSettings(map[string]string{
		store.SettingDefaultStudios: "true", store.SettingDefaultDecades: "true", store.SettingDefaultGenres: "true",
		store.SettingDefaultAwards: "true", store.SettingDefaultMinimum: "2",
	})
	build()
	got := names()
	for _, want := range []string{"Paramount", "Drama", "Oscar Best Picture Winners"} {
		if got[want] == nil {
			t.Errorf("no %q collection; have %v", want, keys(got))
		}
	}
	for _, not := range []string{"Universal", "Thriller", "Crime", "1990s"} { // fewer than 2 titles
		if got[not] != nil {
			t.Errorf("%q made with too few titles", not)
		}
	}
	if c := got["1970s"]; c == nil || c.LabelRule != "mine" {
		t.Errorf("the admin's 1970s collection was replaced: %+v", c)
	}
	if c := got["Paramount"]; c == nil || c.ItemCount != 3 || c.Mode != "hide" {
		t.Errorf("Paramount: %+v", c)
	}
	oscars, _ := st.CollectionMembers(0, got["Oscar Best Picture Winners"].ID)
	if len(oscars) != 2 || !(oscars[0].ID == godfather && oscars[1].ID == gump || oscars[0].ID == gump && oscars[1].ID == godfather) {
		t.Errorf("Oscar winners %v", oscars)
	}

	// Wikidata down: award collections stay as they were.
	failWikidata = true
	if msg := build(); !strings.Contains(msg, "Wikidata") {
		t.Errorf("message %q doesn't mention Wikidata", msg)
	}
	if names()["Oscar Best Picture Winners"] == nil {
		t.Error("award collection removed when Wikidata failed")
	}

	// Turning groups off removes only BingeBandit's collections.
	st.SetSettings(map[string]string{store.SettingDefaultStudios: "false", store.SettingDefaultGenres: "false", store.SettingDefaultAwards: "false", store.SettingDefaultDecades: "false"})
	build()
	if got := names(); len(got) != 1 || got["1970s"] == nil {
		t.Errorf("after turning off: %v", keys(got))
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
