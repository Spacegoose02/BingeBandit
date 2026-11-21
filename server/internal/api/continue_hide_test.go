package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

// Remove from Continue Watching takes a movie or a whole show off the row
// until it's played again, for that person only.
func TestRemoveFromContinueWatching(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	sam, _ := st.CreateUser("sam", string(hash), false)
	other, _ := st.CreateUser("other", string(hash), false)
	st.CreateSession("sam", sam.ID, "Web", "")
	for _, u := range []int64{sam.ID, other.ID} {
		st.SetUserLibraries(u, true, nil)
	}
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(movies)
	shows := &store.Library{Name: "Shows", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(shows)
	movie, _ := st.UpsertItem(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Up", MatchKey: "movie:up"})
	show, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "show", Title: "Bluey", MatchKey: "show:bluey"})
	one := 1
	season, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "season", ParentID: show, ShowID: show, SeasonNumber: &one, MatchKey: "bluey:s1"})
	var eps []int64
	for e := 1; e <= 3; e++ {
		en := e
		id, _ := st.UpsertItem(&store.Item{LibraryID: shows.ID, Kind: "episode", ParentID: season, ShowID: show, SeasonNumber: &one,
			EpisodeNumber: &en, MatchKey: "bluey:e" + itoa(int64(e))})
		eps = append(eps, id)
	}
	srv := &Server{Store: st}
	h := srv.Router(context.Background())
	remove := func(id int64) int {
		req := httptest.NewRequest("DELETE", "/api/continue/"+itoa(id), nil)
		req.Header.Set("Authorization", "Bearer sam")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	row := func(uid int64) []int64 {
		t.Helper()
		got, err := srv.continueWatching(uid, 10)
		if err != nil {
			t.Fatal(err)
		}
		ids := []int64{}
		for _, it := range got {
			ids = append(ids, it.ID)
		}
		return ids
	}

	for _, u := range []int64{sam.ID, other.ID} {
		st.SaveProgress(u, movie, 300, 6000)
		st.SaveProgress(u, eps[0], 1200, 1200) // finished: episode 2 is up next
	}
	if got := row(sam.ID); len(got) != 2 {
		t.Fatalf("want the movie and the show, got %v", got)
	}

	// The show goes by its up-next episode, the movie by itself.
	if c := remove(eps[1]); c != http.StatusNoContent {
		t.Fatalf("remove episode: %d", c)
	}
	if c := remove(movie); c != http.StatusNoContent {
		t.Fatalf("remove movie: %d", c)
	}
	if got := row(sam.ID); len(got) != 0 {
		t.Fatalf("want an empty row, got %v", got)
	}
	if got := row(other.ID); len(got) != 2 {
		t.Fatalf("someone else's row changed: %v", got)
	}
	if c := remove(season); c != http.StatusBadRequest {
		t.Errorf("remove a season: %d, want 400", c)
	}

	// Watching again brings it back.
	time.Sleep(1100 * time.Millisecond) // updated_at is in seconds
	st.SaveProgress(sam.ID, eps[1], 200, 1200)
	if got := row(sam.ID); len(got) != 1 || got[0] != eps[1] {
		t.Fatalf("want Bluey back after playing it, got %v", got)
	}
}
