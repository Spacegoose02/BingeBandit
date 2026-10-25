package store

import (
	"fmt"
	"testing"
)

// Paging through a library must list every title exactly once, even when
// many share the sort value (a first scan adds everything in the same second,
// and unmatched titles all rate 0).
func TestLibraryItemsPagesWithoutRepeats(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "TV", Kind: "shows", Paths: []string{}}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	const n = 500
	for i := 0; i < n; i++ {
		// Every fifth title shares a name, as two folders of one show would.
		title := fmt.Sprintf("Show %03d", i-i%5)
		if _, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "show", Title: title, MatchKey: fmt.Sprintf("show:%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE items SET added_at = 1000, rating = 0, release_date = ''`); err != nil {
		t.Fatal(err)
	}
	for sort := range librarySorts {
		seen := map[int64]bool{}
		for off := 0; off < n; off += 60 {
			items, _, err := s.LibraryItems(0, lib.ID, sort, off, 60, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, it := range items {
				if seen[it.ID] {
					t.Errorf("sort %s: %q (id %d) listed twice", sort, it.Title, it.ID)
				}
				seen[it.ID] = true
			}
		}
		if len(seen) != n {
			t.Errorf("sort %s: %d of %d titles listed", sort, len(seen), n)
		}
	}
}

// Pages leave out cast and crew (only the title's own page shows them), and a
// collection pages in release order without repeats or gaps.
func TestListPagesAreSlimAndOrdered(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	const n = 25
	for i := 0; i < n; i++ {
		id := addMovie(t, s, lib.ID, fmt.Sprintf("Movie %02d", i))
		s.UpdateDetails(id, &Details{Title: fmt.Sprintf("Movie %02d", i), Labels: []string{"all"}})
		s.db.Exec(`UPDATE items SET credits = ? WHERE id = ?`, `[{"name":"Someone","kind":"cast"}]`, id)
		// Release order is the reverse of title order.
		s.db.Exec(`UPDATE items SET release_date = ? WHERE id = ?`, fmt.Sprintf("2000-01-%02d", n-i), id)
	}
	items, total, err := s.LibraryItems(0, lib.ID, "title", 5, 10, nil)
	if err != nil || total != n || len(items) != 10 {
		t.Fatalf("library page: %d items of %d, %v", len(items), total, err)
	}
	if items[0].Title != "Movie 05" || items[9].Title != "Movie 14" {
		t.Errorf("library page runs %q to %q", items[0].Title, items[9].Title)
	}
	if len(items[0].Credits) != 0 {
		t.Errorf("library page sent credits: %v", items[0].Credits)
	}
	if full, _ := s.Item(0, items[0].ID); len(full.Credits) != 1 {
		t.Errorf("the title itself lost its credits: %v", full.Credits)
	}

	c, err := s.CreateCollection(lib.ID, "Everything", SmartRules{LabelRule: "all"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for off := 0; off < n+5; off += 10 {
		page, err := s.CollectionPage(0, c.ID, off, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range page {
			got = append(got, it.Title)
		}
	}
	if len(got) != n || got[0] != "Movie 24" || got[n-1] != "Movie 00" {
		t.Errorf("collection pages = %v", got)
	}
	if all, _ := s.CollectionMembers(0, c.ID); len(all) != n {
		t.Errorf("all members = %d, want %d", len(all), n)
	}
}
