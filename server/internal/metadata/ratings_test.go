package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func TestRefreshRatings(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	movieID, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Parasite", Year: 2019, TMDBID: 496243, MatchKey: "movie:parasite:2019"})
	unknownID, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Home video", Year: 2020, TMDBID: 1, MatchKey: "movie:home:2020"})
	st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Unmatched", MatchKey: "movie:unmatched:0"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("apikey")
		switch {
		case key == "bad":
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/tmdb/movie/496243":
			w.Write([]byte(`{"ids":{"imdb":"tt6751668"},"ratings":[
				{"source":"imdb","value":8.5,"votes":1000000},{"source":"tomatoes","value":99},
				{"source":"popcorn","value":90},{"source":"metacritic","value":96},{"source":"letterboxd","value":4.5},
				{"source":"trakt","value":null}]}`))
		case r.URL.Path == "/" && r.URL.Query().Get("i") == "tt6751668":
			w.Write([]byte(`{"Response":"True","imdbRating":"8.5","imdbVotes":"1,000,000","Metascore":"96",
				"Ratings":[{"Source":"Internet Movie Database","Value":"8.5/10"},{"Source":"Rotten Tomatoes","Value":"99%"}]}`))
		case r.URL.Path == "/":
			w.Write([]byte(`{"Response":"False","Error":"Incorrect IMDb ID."}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	mdblistBase, omdbBase = srv.URL, srv.URL
	svc := NewService(st)
	ctx := context.Background()

	targets, err := st.ItemsNeedingRatings(time.Now().Unix(), 10)
	if err != nil || len(targets) != 2 {
		t.Fatalf("targets: %v %+v", err, targets)
	}
	if svc.RatingsEnabled() {
		t.Fatal("enabled without a provider")
	}

	st.SetSettings(map[string]string{store.SettingRatingsProvider: "mdblist", store.SettingRatingsKey: "k"})
	for _, tg := range targets {
		if err := svc.RefreshRatings(ctx, tg); err != nil {
			t.Fatalf("mdblist %d: %v", tg.ID, err)
		}
	}
	r, err := st.ExternalRatings(movieID)
	if err != nil || r.IMDbID != "tt6751668" || r.IMDb != 8.5 || r.IMDbVotes != 1000000 || r.RT != 99 || r.RTAudience != 90 || r.Metacritic != 96 {
		t.Fatalf("mdblist ratings: %v %+v", err, r)
	}
	if r, err := st.ExternalRatings(unknownID); err != nil || !r.Empty() {
		t.Fatalf("unknown title should be stored empty: %v %+v", err, r)
	}
	// Both were just looked up, so nothing is due.
	if again, _ := st.ItemsNeedingRatings(time.Now().Add(-time.Hour).Unix(), 10); len(again) != 0 {
		t.Fatalf("due again right away: %+v", again)
	}

	// OMDb reuses the IMDb id already known (no TMDB key here).
	st.SetSettings(map[string]string{store.SettingRatingsProvider: "omdb"})
	st.SaveExternalRatings(movieID, &store.ExternalRatings{IMDbID: "tt6751668", FetchedAt: 1})
	if err := svc.RefreshRatings(ctx, store.RatingTarget{ID: movieID, Kind: "movie", TMDBID: 496243}); err != nil {
		t.Fatal(err)
	}
	r, _ = st.ExternalRatings(movieID)
	if r.IMDb != 8.5 || r.IMDbVotes != 1000000 || r.RT != 99 || r.Metacritic != 96 || r.RTAudience != 0 {
		t.Fatalf("omdb ratings: %+v", r)
	}
	// Without an IMDb id or a TMDB key, OMDb can't look anything up.
	if err := svc.RefreshRatings(ctx, store.RatingTarget{ID: unknownID, Kind: "movie", TMDBID: 1}); err == nil {
		t.Fatal("omdb without a TMDB key should fail")
	}

	st.SetSettings(map[string]string{store.SettingRatingsProvider: "mdblist", store.SettingRatingsKey: "bad"})
	if err := svc.RefreshRatings(ctx, targets[0]); !errors.Is(err, ErrRatingsAuth) {
		t.Fatalf("bad key: %v", err)
	}
}
