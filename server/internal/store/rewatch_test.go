package store

import "testing"

func TestRewatchReturnsToContinueWatching(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	id := addMovie(t, s, lib.ID, "Heat")
	u, _ := s.CreateUser("dana", "x", false)
	s.SetUserLibraries(u.ID, true, nil)

	if _, err := s.SaveProgress(u.ID, id, 95, 100); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.InProgress(u.ID, 10); len(got) != 0 {
		t.Fatalf("a finished movie stayed in Continue Watching")
	}
	if _, err := s.SaveProgress(u.ID, id, 40, 100); err != nil {
		t.Fatal(err)
	}
	got, err := s.InProgress(u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Watch.Position != 40 {
		t.Fatalf("rewatch not in Continue Watching: %+v", got)
	}
}
