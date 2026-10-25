package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

func TestPreRoll(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	viewer, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("sam-token", viewer.ID, "Web", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	hidden := &store.Library{Name: "Private", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	st.CreateLibrary(hidden)
	st.SetUserLibraries(viewer.ID, false, []int64{lib.ID})
	movie := func(l *store.Library, title, rating, trailer string) int64 {
		id, err := st.UpsertItem(&store.Item{LibraryID: l.ID, Kind: "movie", Title: title, MatchKey: "movie:" + title})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.UpdateMetadata(id, &store.Item{Title: title, SortTitle: title, ContentRating: rating, Trailer: trailer}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	main := movie(lib, "Main", "PG-13", "main")
	movie(lib, "Family", "PG", "family")
	movie(lib, "Gory", "R", "gory")
	movie(lib, "Unrated", "", "unrated")
	movie(lib, "No trailer", "PG", "")
	watched := movie(lib, "Seen", "G", "seen")
	movie(hidden, "Secret", "PG", "secret")
	st.SetWatched(viewer.ID, []int64{watched}, true)
	ep, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "episode", Title: "Ep", MatchKey: "ep"})

	h := (&Server{Store: st, DataDir: dir}).Router(context.Background())
	call := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer sam-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	type resp struct {
		Trailers []PreRollTrailer `json:"trailers"`
		Intro    *struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		} `json:"intro"`
	}
	get := func(id int64) resp {
		rec := call("/api/items/" + itoa(id) + "/preroll")
		var out resp
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("preroll: %d %s", rec.Code, rec.Body)
		}
		return out
	}

	// Defaults: no trailers, and no intro until an admin sets one.
	if r := get(main); len(r.Trailers) != 0 || r.Intro != nil {
		t.Fatalf("defaults = %+v", r)
	}

	prefs := store.DefaultPrefs
	prefs.PreRollTrailers = 5
	st.SetUserPrefs(viewer.ID, prefs)
	r := get(main)
	if len(r.Trailers) != 1 || r.Trailers[0].YouTube != "family" {
		t.Errorf("trailers before a PG-13 movie = %+v, want only the unwatched, visible PG one", r.Trailers)
	}

	intros := filepath.Join(dir, "intros")
	os.MkdirAll(filepath.Join(intros, "sub"), 0o755)
	os.WriteFile(filepath.Join(intros, "sub", "Studio Logo.mp4"), []byte("fake video"), 0o644)
	os.WriteFile(filepath.Join(intros, "notes.txt"), []byte("x"), 0o644)
	st.SetSettings(map[string]string{store.SettingPreRollPaths: "/nowhere\n" + intros})
	r = get(main)
	if r.Intro == nil || r.Intro.Name != "Studio Logo" {
		t.Fatalf("intro = %+v", r.Intro)
	}
	rec := call(r.Intro.URL)
	if rec.Code != http.StatusOK || rec.Body.String() != "fake video" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "video/mp4") {
		t.Errorf("intro video: %d %q %s", rec.Code, rec.Body, rec.Header().Get("Content-Type"))
	}
	if rec := call("/api/preroll/intro/7"); rec.Code != http.StatusNotFound {
		t.Errorf("bad intro index: %d", rec.Code)
	}

	// Nothing before episodes, and the intro can be turned off.
	if r := get(ep); len(r.Trailers) != 0 || r.Intro != nil {
		t.Errorf("episode preroll = %+v", r)
	}
	prefs.PreRollIntro = false
	st.SetUserPrefs(viewer.ID, prefs)
	if r := get(main); r.Intro != nil {
		t.Error("intro shown with the preference off")
	}
	if rec := call("/api/items/" + itoa(movie(hidden, "Hidden main", "", "x")) + "/preroll"); rec.Code != http.StatusNotFound {
		t.Errorf("preroll for a movie the viewer can't see: %d", rec.Code)
	}
}

func TestPickTrailersUnratedMovie(t *testing.T) {
	cands := []*store.Item{{ID: 1, Trailer: "a", ContentRating: "R"}, {ID: 2, Trailer: "b"}, {ID: 3, Trailer: "c", ContentRating: "G"}}
	if got := pickTrailers(cands, "", 5); len(got) != 3 {
		t.Errorf("an unrated movie should allow every trailer, got %d", len(got))
	}
	if got := pickTrailers(cands, "G", 5); len(got) != 1 || got[0].ItemID != 3 {
		t.Errorf("G movie got %+v", got)
	}
}
