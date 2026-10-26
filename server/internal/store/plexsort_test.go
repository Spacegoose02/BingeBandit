package store

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// Kometa's tiers: "+_" first, then "++_" and so on; within a tier, a digit
// after the underscore comes before letters, and case doesn't matter.
func TestPlexCompareTiers(t *testing.T) {
	want := []string{
		"+_1APirated Weekly", "+_1Family Friendly Movies", "+_Daily", "+_Top Rated", "+_Trending",
		"++_Amazon", "++_APPLE", "++_Disney+ Originals",
		"+++_1Shark Week", "+++_2Marvel Collection", "+++_Based on a True Story", "+++_WhoDunit",
		"++++_A24",
		"++++++_160s Movies", "++++++_170s Movies",
		"++++++++_ON Netflix", "++++++++_OnAmazon",
		"Action", "zombies",
	}
	got := append([]string(nil), want...)
	sort.Slice(got, func(i, j int) bool { return got[i] > got[j] }) // scramble
	sort.Slice(got, func(i, j int) bool { return PlexCompare(got[i], got[j]) < 0 })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order:\n%q\nwant\n%q", got, want)
	}
}

func TestCollectionsInPlexOrder(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	for name, sortTitle := range map[string]string{"Apple": "++_APPLE", "Trending": "+_Trending", "Family": "+_1Family", "Horror": ""} {
		c, _ := st.CreateCollection(lib.ID, name, SmartRules{})
		st.SetCollectionSortTitle(c.ID, sortTitle)
	}
	cols, err := st.LibraryCollections(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cols {
		names = append(names, c.Name)
	}
	if want := []string{"Family", "Trending", "Apple", "Horror"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("collections %v, want %v", names, want)
	}
}
