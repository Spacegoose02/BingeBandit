package store

import (
	"fmt"
	"testing"
)

func TestLibraryPins(t *testing.T) {
	s := openTest(t)
	movies := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	kids := &Library{Name: "Kids", Kind: "movies", Paths: []string{}}
	tv := &Library{Name: "TV", Kind: "shows", Paths: []string{}}
	for _, l := range []*Library{movies, kids, tv} {
		s.CreateLibrary(l)
	}
	addMovie(t, s, movies.ID, "Heat")
	bluey := addMovie(t, s, kids.ID, "Bluey")
	u, _ := s.CreateUser("dana", "x", false)
	s.SetUserLibraries(u.ID, true, nil)

	if chosen, _ := s.PinsChosen(u.ID); chosen {
		t.Fatal("a new user has not chosen pins yet")
	}
	// Kids first, Movies unpinned; TV was never placed, so it stays pinned.
	if err := s.SetLibraryPins(u.ID, []LibraryPin{{kids.ID, true}, {movies.ID, false}}); err != nil {
		t.Fatal(err)
	}
	if chosen, _ := s.PinsChosen(u.ID); !chosen {
		t.Error("saving pins should mark them chosen")
	}
	libs, _ := s.Libraries()
	if err := s.PinLibraries(u.ID, libs); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range libs {
		got = append(got, l.Name+map[bool]string{true: "+", false: "-"}[l.Pinned])
	}
	if want := "[Kids+ TV+ Movies-]"; fmt.Sprint(got) != want {
		t.Errorf("sidebar = %v, want %s", got, want)
	}
	recent, _ := s.RecentlyAdded(u.ID, "movie", 10)
	if len(recent) != 1 || recent[0].ID != bluey {
		t.Errorf("Home should only show pinned libraries, got %d items", len(recent))
	}
}
