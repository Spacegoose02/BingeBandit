package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

// Asking for a title can name the library it should land in, from the
// ones of its kind the asker can see.
func TestRequestLibrary(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	dana, _ := st.CreateUser("dana", string(hash), true)
	sam, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("dana", dana.ID, "Web", "")
	st.CreateSession("sam", sam.ID, "Web", "")
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	kids := &store.Library{Name: "Kids Movies", Kind: "movies", Paths: []string{}}
	xmas := &store.Library{Name: "Christmas", Kind: "movies", Paths: []string{}}
	shows := &store.Library{Name: "TV Shows", Kind: "shows", Paths: []string{}}
	for _, l := range []*store.Library{movies, kids, xmas, shows} {
		st.CreateLibrary(l)
	}
	// Sam can't see Christmas.
	st.SetUserLibraries(sam.ID, false, []int64{movies.ID, kids.ID, shows.ID})
	srv := &Server{Store: st}
	h := srv.Router(context.Background())
	call := func(token, method, path, body string, out any) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Requested-With", "BingeBandit")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}
	ask := func(token, kind string, tmdb, lib int64) (int, store.TitleRequest) {
		var r store.TitleRequest
		c := call(token, "POST", "/api/requests", fmt.Sprintf(`{"kind":%q,"tmdbId":%d,"title":"T%d","libraryId":%d}`, kind, tmdb, tmdb, lib), &r)
		return c, r
	}

	samUser, _ := st.UserByID(sam.ID)
	libs := srv.requestLibraries(samUser, "movie")
	if len(libs) != 2 || libraryIn(libs, xmas.ID) || libraryIn(libs, shows.ID) {
		t.Errorf("sam's movie libraries: %+v", libs)
	}
	if got := srv.defaultRequestLibrary(libs, nil, "movie", 1, kids.ID); got != kids.ID {
		t.Errorf("default with a hint: %d", got)
	}
	if got := srv.defaultRequestLibrary(libs, nil, "movie", 1, xmas.ID); got == xmas.ID || got == 0 {
		t.Errorf("a hint sam can't see: %d", got)
	}

	// A show library for a movie, or one Sam can't see, is refused.
	if c, _ := ask("sam", "movie", 10, shows.ID); c != http.StatusBadRequest {
		t.Errorf("show library for a movie: %d", c)
	}
	if c, _ := ask("sam", "movie", 10, xmas.ID); c != http.StatusBadRequest {
		t.Errorf("hidden library: %d", c)
	}
	// Kids Movies is kept, with its name for the fetching plugin.
	c, r := ask("sam", "movie", 10, kids.ID)
	if c != http.StatusCreated || r.LibraryID != kids.ID || r.Library != "Kids Movies" {
		t.Fatalf("kids movies: %d %+v", c, r)
	}
	// Someone joining with another pick doesn't move it.
	if _, r := ask("dana", "movie", 10, movies.ID); r.LibraryID != kids.ID {
		t.Errorf("joined: %+v", r)
	}
	// No pick (older apps): left to the fetcher.
	_, r = ask("sam", "show", 20, 0)
	if r.LibraryID != 0 || r.Library != "" || r.Kind != "show" {
		t.Errorf("no pick: %+v", r)
	}
	// The admin can move it while approving.
	var approved store.TitleRequest
	if c := call("dana", "POST", fmt.Sprintf("/api/admin/requests/%d/approve", r.ID), fmt.Sprintf(`{"libraryId":%d}`, movies.ID), nil); c != http.StatusBadRequest {
		t.Errorf("movie library for a show: %d", c)
	}
	if c := call("dana", "POST", fmt.Sprintf("/api/admin/requests/%d/approve", r.ID), fmt.Sprintf(`{"libraryId":%d}`, shows.ID), &approved); c != http.StatusOK || approved.Library != "TV Shows" {
		t.Errorf("approve into TV Shows: %d %+v", c, approved)
	}
	// Approving without a body (older apps) keeps it.
	if c := call("dana", "POST", fmt.Sprintf("/api/admin/requests/%d/approve", r.ID), "", &approved); c != http.StatusOK || approved.Library != "TV Shows" {
		t.Errorf("approve again: %d %+v", c, approved)
	}
}
