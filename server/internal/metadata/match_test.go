package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"

	"bingebandit/internal/store"
)

// Matching goes TMDB id → folder TheTVDB/IMDb hints looked up on TMDB → TMDB
// search → TheTVDB for shows, and titles found more than once merge.
func TestMatchAcrossSources(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var logins atomic.Int32
	tvdbSrv := fakeTVDB(t, &logins)
	defer tvdbSrv.Close()
	target, _ := url.Parse(tvdbSrv.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		send := func(v any) { json.NewEncoder(w).Encode(v) }
		empty := map[string]any{"results": []any{}}
		switch r.URL.Path {
		case "/search/movie", "/search/tv": // TMDB knows none of these titles by name
			send(empty)
		case "/find/tt1375666":
			send(map[string]any{"movie_results": []map[string]any{{"id": 27205}}, "tv_results": []any{}})
		case "/find/78874":
			if r.URL.Query().Get("external_source") != "tvdb_id" {
				t.Errorf("find source %q", r.URL.Query().Get("external_source"))
			}
			send(map[string]any{"movie_results": []any{}, "tv_results": []map[string]any{{"id": 1437}}})
		case "/movie/27205":
			send(map[string]any{"id": 27205, "title": "Inception", "imdb_id": "tt1375666", "release_date": "2010-07-15"})
		default:
			proxy.ServeHTTP(w, r) // TheTVDB, and TMDB's /tv/1437
		}
	}))
	defer srv.Close()
	tvdbBase, tmdbBase = srv.URL, srv.URL
	st.SetSettings(map[string]string{store.SettingTMDBKey: "tmdbkey", store.SettingTVDBKey: "key"})
	svc := NewService(st)
	ctx := context.Background()

	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	shows := &store.Library{Name: "Shows", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(movies)
	st.CreateLibrary(shows)
	movie := func(title, key, imdb string) int64 {
		id, err := st.UpsertItem(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: title, IMDbID: imdb, MatchKey: key})
		if err != nil {
			t.Fatal(err)
		}
		st.UpsertFile(&store.MediaFile{ItemID: id, Path: "/m/" + key}, 1)
		return id
	}
	show := func(title, key string, tvdb int64) int64 {
		id, err := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "show", Title: title, TVDBID: tvdb, MatchKey: key})
		if err != nil {
			t.Fatal(err)
		}
		one := 1
		s, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "season", ParentID: id, ShowID: id, Title: "Season 1", SeasonNumber: &one, MatchKey: key + ":s1"})
		e, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "episode", ParentID: s, ShowID: id, Title: "Episode 1", SeasonNumber: &one, EpisodeNumber: &one, MatchKey: key + ":s1:e1"})
		st.UpsertFile(&store.MediaFile{ItemID: e, Path: "/tv/" + key}, 1)
		return id
	}
	inception := movie("Inception", "movie:inception:2010", "tt1375666")
	inception4k := movie("Inception 4K", "movie:inception4k:2010", "tt1375666")
	holiday := movie("Our Holiday", "movie:ourholiday:2019", "")
	firefly := show("firefly", "show:firefly:0", 0)            // TMDB search fails: TheTVDB finds it
	fireflyHint := show("Firefly", "show:firefly:2002", 78874) // {tvdb-78874}: found on TMDB through the hint

	for _, lib := range []int64{movies.ID, shows.ID} {
		if err := svc.RefreshLibrary(ctx, lib, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := st.Item(0, inception)
	if m.TMDBID != 27205 || m.Title != "Inception" || m.Unmatched {
		t.Errorf("IMDb hint: %+v", m)
	}
	if h, _ := st.Item(0, holiday); !h.Unmatched || h.Title != "Our Holiday" {
		t.Errorf("unknown movie should stay unmatched under its folder name: %+v", h)
	}
	f, _ := st.Item(0, firefly)
	if f.TVDBID != 78874 || f.TMDBID != 1437 || f.IMDbID != "tt0303461" || f.MetadataSource != "tvdb" || f.Title != "Firefly" || f.Unmatched {
		t.Errorf("TheTVDB fallback: tvdb %d tmdb %d imdb %q source %q title %q", f.TVDBID, f.TMDBID, f.IMDbID, f.MetadataSource, f.Title)
	}
	if h, _ := st.Item(0, fireflyHint); h.TMDBID != 1437 || h.MetadataSource != "" {
		t.Errorf("TheTVDB hint on TMDB: tmdb %d source %q", h.TMDBID, h.MetadataSource)
	}

	// Both copies of a movie come together; each show folder stays its own show.
	for lib, want := range map[int64]int{movies.ID: 1, shows.ID: 0} {
		merged, err := st.MergeDuplicates(lib)
		if err != nil || len(merged) != want {
			t.Fatalf("library %d merged %+v, %v; want %d", lib, merged, err, want)
		}
	}
	if _, err := st.Item(0, inception4k); err == nil {
		if _, err := st.Item(0, inception); err == nil {
			t.Error("both Inceptions still listed")
		}
	}
	if _, err := st.Item(0, firefly); err != nil {
		t.Error("show folder merged away")
	}
	if _, err := st.Item(0, fireflyHint); err != nil {
		t.Error("show folder merged away")
	}

	// Left unmatched on purpose: refreshes don't search, and it never merges.
	if err := st.SetUnmatched(holiday); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshItem(ctx, holiday); err != nil {
		t.Fatal(err)
	}
	if h, _ := st.Item(0, holiday); !h.Unmatched || h.MetadataSource != "none" || h.TMDBID != 0 {
		t.Errorf("left unmatched: %+v", h)
	}
}

// TMDB's year filter counts re-releases, so the 1994 Lion King can top a 2019
// search; the hit that first came out that year wins.
func TestPickHitPrefersReleaseYear(t *testing.T) {
	var res tmdbSearch
	json.Unmarshal([]byte(`{"results":[{"id":8587,"release_date":"1994-06-15"},{"id":420818,"release_date":"2019-07-12"}]}`), &res)
	if id := pickHit(res, 2019); id != 420818 {
		t.Errorf("2019 → %d", id)
	}
	if id := pickHit(res, 1994); id != 8587 {
		t.Errorf("1994 → %d", id)
	}
	if id := pickHit(res, 2020); id != 420818 {
		t.Errorf("2020 → %d, want the year-out 2019", id)
	}
	if id := pickHit(res, 2005); id != 0 {
		t.Errorf("2005 → %d, want none", id)
	}
	if id := pickHit(res, 0); id != 8587 {
		t.Errorf("no year → %d, want the first", id)
	}
}
