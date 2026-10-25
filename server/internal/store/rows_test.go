package store

import "testing"

func TestLibraryRows(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Kids Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	toy := addMovie(t, s, lib.ID, "Toy Story")
	addMovie(t, s, lib.ID, "Frozen")
	if err := s.SetItemCollections(toy, lib.ID, []string{"Pixar"}); err != nil {
		t.Fatal(err)
	}
	cols, _ := s.LibraryCollections(lib.ID)
	if len(cols) != 1 {
		t.Fatalf("want the Pixar collection, got %d", len(cols))
	}
	pixar := CollectionRowKey(cols[0].ID)

	rows, err := s.LibraryRows(lib)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Key != "continue" || rows[0].Home || rows[1].Key != "recent" || !rows[1].Home {
		t.Errorf("defaults: %+v", rows[:2])
	}
	last := rows[len(rows)-1]
	if last.Key != pixar || last.Title != "Pixar" || last.Library || last.Home {
		t.Errorf("a collection starts hidden at the end, got %+v", last)
	}

	// Turning Pixar on keeps the order; moving it first sticks.
	if err := s.SetRowVisibility(lib, pixar, Visibility{Library: true, Home: true}); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.LibraryRows(lib)
	if got := rows[len(rows)-1]; got.Key != pixar || !got.Home || got.Friends {
		t.Errorf("after Visible on: %+v", got)
	}
	reordered := append([]LibraryRow{rows[len(rows)-1]}, rows[:len(rows)-1]...)
	reordered[1].Home = true // continue is library-only, so this is ignored
	if err := s.SetLibraryRows(lib.ID, reordered); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.LibraryRows(lib)
	if rows[0].Key != pixar || rows[1].Key != "continue" || rows[1].Home {
		t.Errorf("after reorder: %+v %+v", rows[0], rows[1])
	}

	items, err := s.RowItems(0, []int64{lib.ID}, "movies", "recent", "", 10)
	if err != nil || len(items) != 2 {
		t.Errorf("recent: %d items, %v", len(items), err)
	}
	for _, key := range []string{"released", "top-rated", "unwatched", "genres", "watched", "continue"} {
		if _, err := s.RowItems(0, []int64{lib.ID}, "movies", key, "Animation", 10); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
}
