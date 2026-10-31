package store

import (
	"fmt"
	"testing"
)

func TestLibraryFilters(t *testing.T) {
	s := openTest(t)
	u, _ := s.CreateUser("ann", "x", true)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	add := func(title string, year int, rating float64, genres, credits string) int64 {
		id, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "movie", Title: title, MatchKey: "movie:" + title})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`UPDATE items SET year = ?, rating = ?, genres = ?, credits = ? WHERE id = ?`, year, rating, genres, credits, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	alien := add("Alien", 1979, 8.2, `["Horror","Science Fiction"]`, `[{"name":"Sigourney Weaver","role":"Ripley","kind":"cast","photo":"/w.jpg"},{"name":"Ridley Scott","role":"Director","kind":"crew"}]`)
	aliens := add("Aliens", 1986, 7.9, `["Action","Science Fiction"]`, `[{"name":"Sigourney Weaver","role":"Ripley","kind":"cast"}]`)
	add("Gladiator", 2000, 8.5, `["Action","Drama"]`, `[{"name":"Russell Crowe","role":"Maximus","kind":"cast"},{"name":"Ridley Scott","role":"Director","kind":"crew"},{"name":"Ridley Scott","role":"Producer","kind":"crew"}]`)
	add("Cats", 2019, 2.8, `["Comedy"]`, `[{"name":"Sigourney Weaver","role":"Producer","kind":"crew"},{"name":"Sigourney Weaver","role":"Narrator","kind":"cast"}]`)
	if err := s.SetWatched(u.ID, []int64{alien}, true); err != nil {
		t.Fatal(err)
	}

	titles := func(f LibraryFilter) string {
		items, total, err := s.FilteredLibraryItems(u.ID, lib.ID, "title", 0, 100, nil, f)
		if err != nil {
			t.Fatal(err)
		}
		out := ""
		for _, it := range items {
			out += it.Title + ";"
		}
		if total != len(items) {
			t.Errorf("%+v: total %d, listed %d", f, total, len(items))
		}
		return out
	}
	for _, c := range []struct {
		f    LibraryFilter
		want string
	}{
		{LibraryFilter{}, "Alien;Aliens;Cats;Gladiator;"},
		{LibraryFilter{Genre: "science fiction"}, "Alien;Aliens;"},
		{LibraryFilter{YearFrom: 1980, YearTo: 1989}, "Aliens;"},
		{LibraryFilter{YearFrom: 2000}, "Cats;Gladiator;"},
		{LibraryFilter{MinRating: 8}, "Alien;Gladiator;"},
		{LibraryFilter{Unwatched: true}, "Aliens;Cats;Gladiator;"},
		{LibraryFilter{Genre: "Action", Unwatched: true, MinRating: 8}, "Gladiator;"},
	} {
		if got := titles(c.f); got != c.want {
			t.Errorf("%+v: got %s, want %s", c.f, got, c.want)
		}
	}
	letters, err := s.FilteredLibraryLetters(u.ID, lib.ID, nil, LibraryFilter{Genre: "Action"})
	if err != nil || fmt.Sprint(letters) != "[{A 0} {G 1}]" {
		t.Errorf("letters: %v %v", letters, err)
	}

	opts, err := s.LibraryFilterOptions(lib.ID)
	if err != nil || fmt.Sprint(opts.Genres) != "[Action Comedy Drama Horror Science Fiction]" || fmt.Sprint(opts.Decades) != "[2010 2000 1980 1970]" {
		t.Errorf("options: %+v %v", opts, err)
	}

	p, err := s.PersonByName(u.ID, "Ridley Scott")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Titles) != 2 || p.Titles[0].Item.Title != "Gladiator" || p.Titles[0].Role != "Director, Producer" || p.Titles[0].Kind != "crew" || p.Titles[0].Item.Credits != nil {
		t.Errorf("Ridley Scott: %+v", p.Titles)
	}
	p, err = s.PersonByName(u.ID, "Sigourney Weaver")
	if err != nil || p.Photo == "" || len(p.Titles) != 3 || p.Titles[0].Role != "Narrator" || p.Titles[1].Item.ID != aliens || p.Titles[2].Kind != "cast" || p.Titles[2].Role != "Ripley" {
		t.Errorf("Sigourney Weaver: %+v %v", p, err)
	}
	if _, err := s.PersonByName(u.ID, "Nobody"); err != ErrNotFound {
		t.Errorf("unknown person: %v", err)
	}
	// Someone who can't see the library finds nobody.
	bob, _ := s.CreateUser("bob", "x", false)
	s.SetUserLibraries(bob.ID, false, nil)
	if _, err := s.PersonByName(bob.ID, "Ridley Scott"); err != ErrNotFound {
		t.Errorf("hidden library: %v", err)
	}
}
