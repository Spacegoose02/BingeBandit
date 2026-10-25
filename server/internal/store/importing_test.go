package store

import "testing"

func TestImportDedupeAndState(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	id := addMovie(t, s, lib.ID, "Alpha")
	u, _ := s.CreateUser("ann", "x", false)

	play := func(stopped int64, key string) bool {
		t.Helper()
		ok, err := s.ImportPlay(&Play{UserID: u.ID, ItemID: id, Title: "Alpha", Kind: "movie", StartedAt: stopped - 100, StoppedAt: stopped, Completed: true}, key)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !play(1_000_000, "plex:1") {
		t.Fatal("first import should add a play")
	}
	if play(1_000_000, "plex:1") {
		t.Error("same key added twice")
	}
	if play(1_000_300, "tautulli:7") {
		t.Error("the same viewing from another source (5 min apart) was added again")
	}
	if !play(1_100_000, "tautulli:8") {
		t.Error("a later viewing should be added")
	}

	// Resume points never overwrite newer local progress.
	if _, err := s.SaveProgress(u.ID, id, 50, 100); err != nil {
		t.Fatal(err)
	}
	if set, _ := s.ImportResume(u.ID, id, 10, 100, 1); set {
		t.Error("an old imported resume point replaced newer local progress")
	}
	if err := s.ImportWatched(u.ID, id, now()+10); err != nil {
		t.Fatal(err)
	}
	it, err := s.Item(u.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !it.Watch.Watched {
		t.Error("ImportWatched didn't mark the item watched")
	}

	_, n, err := s.EnsureCollection(lib.ID, "Faves", "", []int64{id})
	if n != 1 || err != nil {
		t.Fatalf("EnsureCollection: %d %v", n, err)
	}
	if _, n, _ := s.EnsureCollection(lib.ID, "faves", "", []int64{id}); n != 0 {
		t.Error("collection names should match case-insensitively and items not be added twice")
	}
}
