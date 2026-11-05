package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

// Home's and a library's built-in rows say how to see all of them, and
// /api/rows pages through every title of the row.
func TestRowSeeAll(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	sam, _ := st.CreateUser("sam", string(hash), false)
	kid, _ := st.CreateUser("kid", string(hash), false)
	st.CreateSession("sam", sam.ID, "Web", "")
	st.CreateSession("kid", kid.ID, "Web", "")
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(movies)
	more := &store.Library{Name: "4K Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(more)
	st.SetUserLibraries(sam.ID, true, nil)
	st.SetUserLibraries(kid.ID, false, []int64{movies.ID})
	for i := 0; i < 30; i++ {
		lib := movies.ID
		if i%3 == 0 {
			lib = more.ID
		}
		st.UpsertItem(&store.Item{LibraryID: lib, Kind: "movie", Title: fmt.Sprintf("Movie %02d", i), Year: 2000 + i, MatchKey: fmt.Sprintf("movie:m%d:%d", i, 2000+i)})
	}
	h := (&Server{Store: st}).Router(context.Background())
	call := func(token, path string, out any) int {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}
	type row struct {
		ID     string       `json:"id"`
		Title  string       `json:"title"`
		Items  []store.Item `json:"items"`
		SeeAll string       `json:"seeAll"`
	}
	type page struct {
		Title string       `json:"title"`
		Items []store.Item `json:"items"`
	}

	var home struct {
		Rows []row `json:"rows"`
	}
	call("sam", "/api/home", &home)
	var recent *row
	for i := range home.Rows {
		if home.Rows[i].ID == "movies-recent" {
			recent = &home.Rows[i]
		}
	}
	if recent == nil {
		t.Fatalf("no Recently Added Movies row on Home (%d rows)", len(home.Rows))
	}
	if len(recent.Items) != rowSize {
		t.Errorf("Home's row: %d titles, want %d", len(recent.Items), rowSize)
	}
	q, _ := url.ParseQuery(recent.SeeAll)
	if libs := q.Get("libraries"); q.Get("key") != "recent" || (libs != fmt.Sprintf("%d,%d", movies.ID, more.ID) && libs != fmt.Sprintf("%d,%d", more.ID, movies.ID)) {
		t.Errorf("seeAll %q: want key=recent and both libraries", recent.SeeAll)
	}

	// Every title, a page at a time, newest first like the row.
	var p1, p2 page
	call("sam", "/api/rows?"+recent.SeeAll+"&limit=20", &p1)
	call("sam", "/api/rows?"+recent.SeeAll+"&offset=20&limit=20", &p2)
	if p1.Title != "Recently Added Movies" || len(p1.Items) != 20 || len(p2.Items) != 10 {
		t.Fatalf("pages: %q %d + %d titles, want 20 + 10", p1.Title, len(p1.Items), len(p2.Items))
	}
	if p1.Items[0].ID != recent.Items[0].ID {
		t.Error("See all should start where the row does")
	}

	// A library's Recommended tab: just that library.
	var hubs []row
	call("sam", fmt.Sprintf("/api/libraries/%d/hubs", movies.ID), &hubs)
	if len(hubs) == 0 || hubs[0].SeeAll != fmt.Sprintf("key=recent&libraries=%d", movies.ID) {
		t.Fatalf("Recommended tab's first row should see all of Movies' Recently Added; got %d rows", len(hubs))
	}
	var lp page
	call("sam", "/api/rows?"+hubs[0].SeeAll, &lp)
	if len(lp.Items) != 20 {
		t.Errorf("Movies' Recently Added: %d titles, want 20", len(lp.Items))
	}

	// Libraries the person can't see are left out; none left is a 404.
	var k page
	call("kid", "/api/rows?"+recent.SeeAll+"&limit=100", &k)
	if len(k.Items) != 20 {
		t.Errorf("kid sees %d titles, want the 20 in Movies", len(k.Items))
	}
	if c := call("kid", fmt.Sprintf("/api/rows?key=recent&libraries=%d", more.ID), nil); c != http.StatusNotFound {
		t.Errorf("a library they can't see: %d, want 404", c)
	}
	if c := call("sam", fmt.Sprintf("/api/rows?key=nope&libraries=%d", movies.ID), nil); c != http.StatusNotFound {
		t.Errorf("unknown row: %d, want 404", c)
	}
	if c := call("sam", fmt.Sprintf("/api/rows?key=genres&libraries=%d", movies.ID), nil); c != http.StatusNotFound {
		t.Errorf("genre row without a genre: %d, want 404", c)
	}
}

// A title's details carry its best picture and sound for the apps' badges;
// a show's come from its best episode.
func TestItemQuality(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	sam, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("sam", sam.ID, "Web", "")
	st.SetUserLibraries(sam.ID, true, nil)
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(movies)
	shows := &store.Library{Name: "Shows", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(shows)
	movie, _ := st.UpsertItem(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Dune", Year: 2021, MatchKey: "movie:dune:2021"})
	plain, _ := st.UpsertItem(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Heat", Year: 1995, MatchKey: "movie:heat:1995"})
	show, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "show", Title: "Bluey", MatchKey: "show:bluey"})
	season, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "season", Title: "Season 1", ParentID: show, ShowID: show, MatchKey: "season:bluey:1"})
	one := 1
	ep, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "episode", Title: "Magic Xylophone", ParentID: season, ShowID: show,
		SeasonNumber: &one, EpisodeNumber: &one, MatchKey: "episode:bluey:1:1"})
	dir := t.TempDir()
	st.UpsertFile(&store.MediaFile{ItemID: movie, Path: filepath.Join(dir, "dune.mkv"), VideoCodec: "hevc", Width: 3840, Height: 1600, HDR: true,
		Streams: []store.Stream{{Type: "video", Codec: "hevc", Range: "dv"}, {Type: "audio", Codec: "truehd", Profile: "Dolby TrueHD + Dolby Atmos", Channels: 8}}}, 1)
	st.UpsertFile(&store.MediaFile{ItemID: plain, Path: filepath.Join(dir, "heat.mkv"), VideoCodec: "h264", Width: 720, Height: 480,
		Streams: []store.Stream{{Type: "video", Codec: "h264", Range: "sdr"}, {Type: "audio", Codec: "aac", Channels: 2}}}, 1)
	st.UpsertFile(&store.MediaFile{ItemID: ep, Path: filepath.Join(dir, "bluey.mkv"), VideoCodec: "h264", Width: 1920, Height: 1080,
		Streams: []store.Stream{{Type: "video", Codec: "h264", Range: "sdr"}, {Type: "audio", Codec: "eac3", Channels: 6}}}, 1)
	h := (&Server{Store: st}).Router(context.Background())
	get := func(id int64) *quality {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/items/"+itoa(id), nil)
		req.Header.Set("Authorization", "Bearer sam")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var d struct {
			Quality *quality `json:"quality"`
		}
		json.Unmarshal(rec.Body.Bytes(), &d)
		return d.Quality
	}
	if q := get(movie); q == nil || *q != (quality{"4k", "dv", "atmos"}) {
		t.Errorf("Dune: %+v, want 4k dv atmos", q)
	}
	if q := get(plain); q == nil || *q != (quality{Resolution: "sd"}) {
		t.Errorf("Heat: %+v, want just sd (SDR and stereo say nothing)", q)
	}
	for _, id := range []int64{show, ep} {
		if q := get(id); q == nil || *q != (quality{Resolution: "1080", Audio: "5.1"}) {
			t.Errorf("Bluey %d: %+v, want 1080 5.1", id, q)
		}
	}
}
