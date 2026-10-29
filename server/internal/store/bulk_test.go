package store

import (
	"slices"
	"testing"
)

func TestBulkEdit(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	a, b := addMovie(t, s, lib.ID, "Alpha"), addMovie(t, s, lib.ID, "Beta")
	s.AddItemLabel(a, "Old")
	old, _, _ := s.EnsureCollection(lib.ID, "Old", "", []int64{a, b})

	n, err := s.BulkEditItems([]int64{a, b, 999}, BulkEdit{
		AddCollections: []string{"Picks"}, RemoveCollections: []string{"old"},
		AddLabels: []string{"New", "new"}, RemoveLabels: []string{"OLD"},
	})
	if err != nil || n != 2 {
		t.Fatalf("changed %d, err %v", n, err)
	}
	for _, id := range []int64{a, b} {
		names, _ := s.ItemCollectionNames(id)
		if !slices.Equal(names, []string{"Picks"}) {
			t.Errorf("item %d collections %v", id, names)
		}
		it, _ := s.Item(0, id)
		if !slices.Equal(it.Labels, []string{"New"}) {
			t.Errorf("item %d labels %v", id, it.Labels)
		}
	}
	// Running it again changes nothing.
	if n, _ := s.BulkEditItems([]int64{a, b}, BulkEdit{AddCollections: []string{"Picks"}, AddLabels: []string{"New"}}); n != 0 {
		t.Errorf("second run changed %d", n)
	}

	if n, err := s.BulkEditCollections([]int64{old}, BulkEdit{AddLabels: []string{"Franchise"}}); err != nil || n != 1 {
		t.Fatalf("collections changed %d, err %v", n, err)
	}
	c, _ := s.Collection(old)
	if !slices.Equal(c.Labels, []string{"Franchise"}) {
		t.Errorf("collection labels %v", c.Labels)
	}
	if labels, _ := s.CollectionLabels(lib.ID); !slices.Equal(labels, []string{"Franchise"}) {
		t.Errorf("collection labels list %v", labels)
	}
}
