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
