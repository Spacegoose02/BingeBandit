package store

import "testing"

func TestCollectionModesAndLabels(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	a, b, c := addMovie(t, s, lib.ID, "Alpha"), addMovie(t, s, lib.ID, "Beta"), addMovie(t, s, lib.ID, "Gamma")
	collapse, _, _ := s.EnsureCollection(lib.ID, "Folded", "", []int64{a})
	if err := s.UpdateCollection(collapse, "Folded", "", "collapse", SmartRules{}); err != nil {
		t.Fatal(err)
	}
	s.EnsureCollection(lib.ID, "Open", "", []int64{b})

	// Only the collection set to "Hide items in this collection" hides its titles.
	cols, hidden, err := s.LibraryCollectionsHiding(lib.ID, func(c *Collection) bool { return c.Mode == "collapse" })
	if err != nil || len(cols) != 2 || len(hidden) != 1 || hidden[0] != a {
		t.Fatalf("cols %d hidden %v err %v", len(cols), hidden, err)
	}
	if got, _ := s.Collection(collapse); got.Mode != "collapse" {
		t.Errorf("mode %q", got.Mode)
	}

	// Imported labels feed label rules, once each.
	if added, err := s.AddItemLabel(c, "Kometa Horror"); !added || err != nil {
		t.Fatalf("AddItemLabel: %v %v", added, err)
	}
	if added, _ := s.AddItemLabel(c, "kometa horror"); added {
		t.Error("the same label was added twice")
	}
	horror, _, _ := s.EnsureCollection(lib.ID, "Horror", "", nil)
	s.SetCollectionLabel(horror, "Kometa Horror")
	s.SetCollectionLabel(horror, "Other")
	got, _ := s.Collection(horror)
	if got.LabelRule != "Kometa Horror" || got.ItemCount != 1 {
		t.Errorf("label rule %q, %d items", got.LabelRule, got.ItemCount)
	}

	// Imported posters never replace one that's already set.
	if s.CollectionHasPoster(horror) {
		t.Error("no poster yet")
	}
	s.SetCollectionPoster(horror, "local:/a.jpg")
	s.SetCollectionPoster(horror, "local:/b.jpg")
	if got, _ := s.Collection(horror); got.Poster != ImageURL("local:/a.jpg") {
		t.Errorf("poster %q", got.Poster)
	}
}
