package importer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

// chartServer stands in for TMDB, Trakt, IMDb and Yamtrack.
func chartServer(t *testing.T) *httptest.Server {
	signedIn := false
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tmdb/trending/movie/week":
			if r.URL.Query().Get("api_key") != "tk" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("page") == "1" {
				w.Write([]byte(`{"total_pages":2,"results":[{"id":1091,"title":"The Thing","release_date":"1982-06-25","media_type":"movie"},{"id":9,"name":"A Person","media_type":"person"}]}`))
			} else {
				w.Write([]byte(`{"total_pages":2,"results":[{"id":348,"title":"Alien","release_date":"1979-05-25","media_type":"movie"}]}`))
			}
		case "/tmdb/tv/top_rated":
			w.Write([]byte(`{"total_pages":1,"results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20"}]}`))
		case "/trakt/movies/trending":
			if r.Header.Get("trakt-api-key") != "cid" || r.URL.Query().Get("limit") != "2" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Write([]byte(`[{"watchers":9,"movie":{"title":"Alien","year":1979,"ids":{"tmdb":348}}},{"watchers":3,"movie":{"title":"Heat","year":1995,"ids":{"imdb":"tt0113277"}}}]`))
		case "/trakt/movies/popular":
			w.Write([]byte(`[{"title":"The Thing","year":1982,"ids":{"tmdb":1091}}]`))
		case "/trakt/users/ann/lists/horror/items":
			w.Write([]byte(`[{"type":"movie","movie":{"title":"Alien","year":1979,"ids":{"tmdb":348}}},{"type":"show","show":{"title":"Bluey","year":2018,"ids":{"tvdb":353948}}}]`))
		case "/graphql/":
			body, _ := io.ReadAll(r.Body)
			var q struct{ Query string }
			json.Unmarshal(body, &q)
			if strings.Contains(q.Query, "TOP_RATED_MOVIES") {
				w.Write([]byte(`{"data":{"chartTitles":{"edges":[{"node":{"id":"tt0113277","titleText":{"text":"Heat"},"releaseYear":{"year":1995},"titleType":{"id":"movie"}}},{"node":{"id":"tt0903747","titleText":{"text":"Breaking Bad"},"releaseYear":{"year":2008},"titleType":{"id":"tvSeries"}}}]}}}`))
				return
			}
			w.Write([]byte(`{"errors":[{"message":"nope"}]}`))
		case "/imdb/list/ls000096828/":
			w.Write([]byte(`<a href="/title/tt0078748/?ref_=1">Alien</a><a href="/title/tt0078748/">again</a><a href="/title/tt0113277/">Heat</a>`))
		case "/list/halloween":
			if !signedIn {
				http.Redirect(w, r, "/accounts/login/?next=/list/halloween", http.StatusFound)
				return
			}
			if r.URL.Query().Get("sort") != "custom" {
				t.Errorf("Yamtrack list read without its custom order: %s", r.URL)
			}
			if r.URL.Query().Get("page") == "" {
				w.Write([]byte(`<a href="/details/tmdb/movie/1091/the-thing">x</a><a href="/details/tmdb/movie/348">y</a><a href="/details/tmdb/movie/1091/the-thing">dup</a>`))
				return
			}
			http.NotFound(w, r)
		case "/accounts/login/":
			if r.Method == http.MethodPost {
				r.ParseForm()
				if r.Form.Get("csrfmiddlewaretoken") == "tok" && r.Form.Get("password") == "pw" {
					signedIn = true
					http.Redirect(w, r, "/", http.StatusFound)
					return
				}
			}
			w.Write([]byte(`<form><input type="hidden" name="csrfmiddlewaretoken" value="tok"></form>`))
		case "/":
			w.Write([]byte(`home`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestChartLists(t *testing.T) {
	srv := chartServer(t)
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	l := NewLists(st)
	l.tmdbBase, l.traktBase, l.imdbGraphQL, l.imdbSite = srv.URL+"/tmdb", srv.URL+"/trakt", srv.URL+"/graphql/", srv.URL+"/imdb"
	l.client = srv.Client()
	ctx := context.Background()
	fetch := func(link, kind string) ([]Record, error) {
		t.Helper()
		return l.Fetch(ctx, link, kind)
	}
	labels := func(recs []Record) string {
		var s []string
		for _, r := range recs {
			s = append(s, r.Kind+":"+r.Title+":"+r.IMDB)
		}
		return strings.Join(s, ", ")
	}

	if _, err := fetch("https://www.themoviedb.org/trending/movie/week", "movies"); err == nil || !strings.Contains(err.Error(), "TMDB API key") {
		t.Fatalf("TMDB without a key: %v", err)
	}
	if _, err := fetch("https://trakt.tv/movies/trending", "movies"); err == nil || !strings.Contains(err.Error(), "Trakt client ID") {
		t.Fatalf("Trakt without a client ID: %v", err)
	}
	st.SetSettings(map[string]string{store.SettingTMDBKey: "tk"})
	st.SetSettings(map[string]string{store.SettingTraktClientID: "cid"})

	recs, err := fetch("https://www.themoviedb.org/trending/movie/week", "movies")
	if err != nil || labels(recs) != "movie:The Thing:, movie:Alien:" || recs[0].TMDB != 1091 || recs[0].Year != 1982 {
		t.Fatalf("TMDB trending, over two pages, without people: %v %+v", err, recs)
	}
	if recs, _ = fetch("https://www.themoviedb.org/trending/movie/week?limit=1", "movies"); len(recs) != 1 {
		t.Fatalf("limit: %+v", recs)
	}
	if recs, _ = fetch("https://www.themoviedb.org/tv/top-rated", "shows"); labels(recs) != "show:Breaking Bad:" || recs[0].Year != 2008 {
		t.Fatalf("TMDB top rated TV: %+v", recs)
	}

	if recs, err = fetch("https://trakt.tv/movies/trending?limit=2", "movies"); err != nil || labels(recs) != "movie:Alien:, movie:Heat:tt0113277" {
		t.Fatalf("Trakt trending: %v %+v", err, recs)
	}
	if recs, _ = fetch("https://trakt.tv/movies/popular", "movies"); labels(recs) != "movie:The Thing:" {
		t.Fatalf("Trakt popular (bare titles): %+v", recs)
	}
	if recs, _ = fetch("https://trakt.tv/users/ann/lists/horror", "movies"); labels(recs) != "movie:Alien:, show:Bluey:" || recs[1].TVDB != 353948 {
		t.Fatalf("Trakt list: %+v", recs)
	}

	if recs, err = fetch("https://www.imdb.com/chart/top/", "movies"); err != nil || labels(recs) != "movie:Heat:tt0113277, show:Breaking Bad:tt0903747" {
		t.Fatalf("IMDb Top 250: %v %+v", err, recs)
	}
	// GraphQL refuses the list, so the page's title links are read instead.
	if recs, err = fetch("https://www.imdb.com/list/ls000096828/", "movies"); err != nil || labels(recs) != "movie::tt0078748, movie::tt0113277" {
		t.Fatalf("IMDb list from the page: %v %+v", err, recs)
	}

	// Yamtrack: a private list signs in first.
	yt := srv.URL + "/list/halloween"
	st.SetSettings(map[string]string{store.SettingYamtrackURL: srv.URL})
	if _, err := fetch(yt, "movies"); err == nil || !strings.Contains(err.Error(), "username and password") {
		t.Fatalf("Yamtrack without a sign-in: %v", err)
	}
	st.SetSettings(map[string]string{store.SettingYamtrackUser: "michael"})
	st.SetSettings(map[string]string{store.SettingYamtrackPassword: "pw"})
	if recs, err = fetch(yt, "movies"); err != nil || len(recs) != 2 || recs[0].TMDB != 1091 || recs[1].TMDB != 348 {
		t.Fatalf("Yamtrack list: %v %+v", err, recs)
	}

	// Several lists are taken in turn, without repeats.
	recs, err = fetch("https://trakt.tv/movies/trending?limit=2\nhttps://www.themoviedb.org/trending/movie/week", "movies")
	if err != nil || labels(recs) != "movie:Alien:, movie:The Thing:, movie:Heat:tt0113277" {
		t.Fatalf("combined: %v %s", err, labels(recs))
	}
}

func TestChartCollectionOrder(t *testing.T) {
	srv := chartServer(t)
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSettings(map[string]string{store.SettingTraktClientID: "cid"})
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	alien, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Alien", Year: 1979, TMDBID: 348, ReleaseDate: "1979-05-25", MatchKey: "m:alien"})
	heat, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", Year: 1995, IMDbID: "tt0113277", ReleaseDate: "1995-12-15", MatchKey: "m:heat"})
	thing, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "The Thing", Year: 1982, TMDBID: 1091, ReleaseDate: "1982-06-25", MatchKey: "m:thing"})

	l := NewLists(st)
	l.traktBase = srv.URL + "/trakt"
	l.client = srv.Client()
	c, _ := st.CreateCollection(lib.ID, "Trending", store.SmartRules{})
	st.SetCollectionSort(c.ID, "list")
	st.SetCollectionList(c.ID, "https://trakt.tv/movies/popular\nhttps://trakt.tv/movies/trending?limit=2")
	c, _ = st.Collection(c.ID)
	if err := l.Sync(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	items, _ := st.CollectionMembers(0, c.ID)
	var got []int64
	for _, it := range items {
		got = append(got, it.ID)
	}
	// The Thing (popular #1), Alien (trending #1), Heat (trending #2, by IMDb id).
	if len(got) != 3 || got[0] != thing || got[1] != alien || got[2] != heat {
		t.Fatalf("list order: %v, want %v", got, []int64{thing, alien, heat})
	}
}
