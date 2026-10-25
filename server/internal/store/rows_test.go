package store

import (
	"fmt"
	"testing"
)

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

// LibraryCollections counts members in Go; it must agree with the SQL rules.
func TestLibraryCollectionsMatchSQL(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	for i := 0; i < 60; i++ {
		id := addMovie(t, s, lib.ID, fmt.Sprint("m", i))
		s.db.Exec(`UPDATE items SET genres = ?, labels = ?, studio = ?, year = ?, poster = ?, release_date = ? WHERE id = ?`,
			fmt.Sprintf(`["%s"]`, []string{"Drama", "comedy", "Action"}[i%3]), fmt.Sprintf(`["L%d"]`, i%4),
			[]string{"Pixar", "pixar", "A24"}[i%3], 1990+i%30, fmt.Sprintf("tmdb:/p%d.jpg", i), fmt.Sprintf("%d-01-01", 1990+i%30), id)
		if i%7 == 0 {
			s.SetItemCollections(id, lib.ID, []string{"Picks"})
		}
	}
	for _, r := range []SmartRules{{LabelRule: "l1"}, {GenreRule: "Comedy"}, {StudioRule: "PIXAR", YearFrom: 2000}, {YearTo: 1995}, {GenreRule: "Drama", LabelRule: "L0"}} {
		if _, err := s.CreateCollection(lib.ID, fmt.Sprintf("%+v", r), r); err != nil {
			t.Fatal(err)
		}
	}
	cols, err := s.LibraryCollections(lib.ID)
	if err != nil || len(cols) != 6 {
		t.Fatalf("%d collections, %v", len(cols), err)
	}
	for _, c := range cols {
		want, _ := s.Collection(c.ID)
		// Posters can differ between members released the same day, so only check one is picked.
		if c.ItemCount != want.ItemCount || c.ItemCount == 0 || c.Poster == "" {
			t.Errorf("%s: count %d poster %q, SQL says %d", c.Name, c.ItemCount, c.Poster, want.ItemCount)
		}
	}
	// "Hide items which are in collections" drops every collected title.
	in, err := s.CollectedItems(lib.ID)
	if err != nil || len(in) == 0 {
		t.Fatalf("collected %d, %v", len(in), err)
	}
	_, total, err := s.LibraryItems(0, lib.ID, "title", 0, 100, in)
	if err != nil || total != 60-len(in) {
		t.Errorf("with collected titles hidden: %d of 60, want %d (%v)", total, 60-len(in), err)
	}
}
