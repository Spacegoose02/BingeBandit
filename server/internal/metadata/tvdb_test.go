package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"bingebandit/internal/store"
)

// fakeTVDB serves the TheTVDB v4 endpoints the client uses for one show,
// "Firefly" (78874), whose DVD order differs from the aired order.
func fakeTVDB(t *testing.T, logins *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		send := func(v any) { json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": v}) }
		if r.URL.Path == "/login" {
			var body struct{ APIKey, PIN string }
			json.NewDecoder(r.Body).Decode(&body)
			if body.APIKey != "key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			n := logins.Add(1)
			send(map[string]string{"token": "tok" + string(rune('0'+n))})
			return
		}
		// The first token "expires" after one use, to exercise logging in again.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer tok2" && !(auth == "Bearer tok1" && r.URL.Path == "/search") && !strings.HasPrefix(r.URL.Path, "/tv/") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/search":
			if r.URL.Query().Get("type") != "series" {
				t.Errorf("search type %q", r.URL.Query().Get("type"))
			}
			send([]map[string]any{
				{"tvdb_id": "78874", "name": "Firefly", "year": "2002", "image_url": "https://artworks.thetvdb.com/firefly.jpg",
					"overview": "Default overview", "overviews": map[string]string{"eng": "Captain Malcolm Reynolds…"}},
				{"tvdb_id": "", "name": "broken"},
			})
		case "/series/78874/extended":
			send(map[string]any{
				"id": 78874, "name": "Firefly", "image": "https://artworks.thetvdb.com/fallback.jpg", "firstAired": "2002-09-20",
				"averageRuntime": 44, "genres": []map[string]string{{"name": "Science Fiction"}, {"name": "Western"}},
				"originalNetwork": map[string]string{"name": "FOX"},
				"artworks": []map[string]any{
					{"image": "https://a/poster-fr.jpg", "type": 2, "language": "fra", "score": 900},
					{"image": "https://a/poster-en.jpg", "type": 2, "language": "eng", "score": 100},
					{"image": "https://a/bg.jpg", "type": 3, "language": nil, "score": 10},
				},
				"remoteIds":      []map[string]string{{"id": "tt0303461", "sourceName": "IMDB"}, {"id": "1437", "sourceName": "TheMovieDB.com"}},
				"characters":     []map[string]any{{"name": "Zoe", "peopleType": "Actor", "personName": "Gina Torres", "sort": 2}, {"name": "Mal", "peopleType": "Actor", "personName": "Nathan Fillion", "sort": 1}, {"name": "", "peopleType": "Director", "personName": "Joss Whedon"}},
				"contentRatings": []map[string]string{{"name": "TV-14", "country": "usa"}},
				"seasons": []map[string]any{
					{"number": 1, "image": "https://a/s1-aired.jpg", "type": map[string]string{"type": "official"}},
					{"number": 1, "image": "https://a/s1-dvd.jpg", "type": map[string]string{"type": "dvd"}},
				},
			})
		case "/series/78874/translations/eng":
			send(map[string]string{"name": "Firefly", "overview": "Five hundred years in the future…"})
		case "/series/78874/episodes/dvd/eng":
			if r.URL.Query().Get("page") == "1" {
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"episodes": []map[string]any{
					{"seasonNumber": 1, "number": 2, "name": "The Train Job", "aired": "2002-09-20"},
				}}, "links": map[string]any{"next": nil}})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"episodes": []map[string]any{
				{"seasonNumber": 1, "number": 1, "name": "Serenity", "overview": "The pilot.", "aired": "2002-12-20", "runtime": 86, "image": "https://a/e1.jpg"},
			}}, "links": map[string]any{"next": "https://api4.thetvdb.com/v4/series/78874/episodes/dvd/eng?page=1"}})
		case "/series/78874/episodes/official/eng":
			send(map[string]any{"episodes": []map[string]any{
				{"seasonNumber": 1, "number": 1, "name": "The Train Job", "aired": "2002-09-20"},
				{"seasonNumber": 1, "number": 2, "name": "Bushwhacked", "aired": "2002-09-27"},
			}})
		case "/tv/1437":
			w.Write([]byte(`{"id":1437,"name":"Firefly","tagline":"You can't take the sky from me","vote_average":8.3,
				"videos":{"results":[{"key":"abc","site":"YouTube","type":"Trailer","official":true}]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestTVDBShow(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "Shows", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(lib)
	showID, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "show", Title: "firefly", MatchKey: "show:firefly"})
	one, two := 1, 2
	seasonID, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "season", ParentID: showID, ShowID: showID, Title: "Season 1", SeasonNumber: &one, MatchKey: "show:firefly:s1"})
	ep1, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: seasonID, ShowID: showID, Title: "Episode 1", SeasonNumber: &one, EpisodeNumber: &one, MatchKey: "show:firefly:s1:e1"})
	ep2, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: seasonID, ShowID: showID, Title: "Episode 2", SeasonNumber: &one, EpisodeNumber: &two, MatchKey: "show:firefly:s1:e2"})

	var logins atomic.Int32
	srv := fakeTVDB(t, &logins)
	defer srv.Close()
	tvdbBase, tmdbBase = srv.URL, srv.URL
	svc := NewService(st)
	ctx := context.Background()

	if _, err := svc.SearchTVDB(ctx, "firefly", 0); !errors.Is(err, ErrNoTVDBKey) {
		t.Fatalf("no key: %v", err)
	}
	st.SetSettings(map[string]string{store.SettingTVDBKey: "bad"})
	if _, err := svc.SearchTVDB(ctx, "firefly", 0); !errors.Is(err, errTVDBAuth) {
		t.Fatalf("bad key: %v", err)
	}
	st.SetSettings(map[string]string{store.SettingTVDBKey: "key"})
	res, err := svc.SearchTVDB(ctx, "firefly", 2002)
	if err != nil || len(res) != 1 || res[0].TVDBID != 78874 || res[0].Year != 2002 || res[0].Overview != "Captain Malcolm Reynolds…" {
		t.Fatalf("search: %v %+v", err, res)
	}

	// DVD order, no TMDB key: everything from TheTVDB. The first token is
	// refused partway, so the client logs in again.
	if err := svc.MatchTVDB(ctx, showID, 78874, "dvd"); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 2 {
		t.Errorf("logins = %d, want 2", logins.Load())
	}
	show, _ := st.Item(0, showID)
	if show.Title != "Firefly" || show.Overview != "Five hundred years in the future…" || show.Year != 2002 || show.Studio != "FOX" ||
		show.Poster != "https://a/poster-en.jpg" || show.Backdrop != "https://a/bg.jpg" || show.ContentRating != "TV-14" ||
		show.TMDBID != 1437 || strings.Join(show.Genres, ",") != "Science Fiction,Western" || show.Trailer != "" {
		t.Errorf("show: %+v", show)
	}
	if len(show.Credits) != 2 || show.Credits[0].Name != "Nathan Fillion" || show.Credits[0].Role != "Mal" {
		t.Errorf("cast: %+v", show.Credits)
	}
	season, _ := st.Item(0, seasonID)
	if season.Poster != "https://a/s1-dvd.jpg" || season.ReleaseDate != "2002-09-20" {
		t.Errorf("season: %+v", season)
	}
	e1, _ := st.Item(0, ep1)
	e2, _ := st.Item(0, ep2)
	if e1.Title != "Serenity" || e1.Overview != "The pilot." || e1.Runtime != 86 || e1.Backdrop != "https://a/e1.jpg" || e2.Title != "The Train Job" {
		t.Errorf("dvd episodes: %q %q", e1.Title, e2.Title)
	}
	if id, order, _ := st.ShowTVDB(showID); id != 78874 || order != "dvd" {
		t.Errorf("stored match: %d %q", id, order)
	}

	// Aired order, with TMDB adding the trailer, tagline and rating.
	st.SetSettings(map[string]string{store.SettingTMDBKey: "tmdbkey"})
	if err := svc.MatchTVDB(ctx, showID, 78874, ""); err != nil {
		t.Fatal(err)
	}
	show, _ = st.Item(0, showID)
	e1, _ = st.Item(0, ep1)
	e2, _ = st.Item(0, ep2)
	if show.Trailer != "abc" || show.Tagline != "You can't take the sky from me" || show.Rating != 8.3 || show.Credits[0].Name != "Nathan Fillion" {
		t.Errorf("tmdb extras: %+v", show)
	}
	if e1.Title != "The Train Job" || e2.Title != "Bushwhacked" {
		t.Errorf("aired episodes: %q %q", e1.Title, e2.Title)
	}
	season, _ = st.Item(0, seasonID)
	if season.Poster != "https://a/s1-aired.jpg" {
		t.Errorf("aired season poster: %q", season.Poster)
	}
}
