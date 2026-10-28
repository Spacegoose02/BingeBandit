package store

import (
	"fmt"
	"testing"
)

// Shows are never merged, even two folders matched to the same TMDB show:
// each folder stays its own show, so a wrong match can be fixed.
func TestShowsDontMerge(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "TV", Kind: "shows", Paths: []string{}}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"show:mickeymouseclubhouse:0", "show:mickeymouseclubhouse:0|Mickey Mouse Clubhouse+"} {
		show, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "show", Title: key, MatchKey: key})
		if err != nil {
			t.Fatal(err)
		}
		one := 1
		season, _ := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "season", ParentID: show, ShowID: show, Title: "Season 1", SeasonNumber: &one, MatchKey: key + ":s1"})
		ep, _ := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "episode", ParentID: season, ShowID: show, Title: "E1", SeasonNumber: &one, EpisodeNumber: &one, MatchKey: key + ":s1:e1"})
		if err := s.UpsertFile(&MediaFile{ItemID: ep, Path: fmt.Sprintf("/tv/%s/1.mkv", key)}, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`UPDATE items SET tmdb_id = 2316 WHERE id = ?`, show); err != nil {
			t.Fatal(err)
		}
	}
	if merged, err := s.MergeDuplicates(lib.ID); err != nil || len(merged) != 0 {
		t.Fatalf("merged %+v, %v; want none", merged, err)
	}
}

// Titles merge when they share any id and no id disagrees; movies only
// within one edition; unmatched titles never.
func TestMergeDuplicatesAcrossSources(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	add := func(key string, tmdb int64, imdb, edition string) int64 {
		id, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "movie", Title: key, MatchKey: key})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`UPDATE items SET tmdb_id = ?, imdb_id = ?, edition = ?, metadata_at = 1 WHERE id = ?`, tmdb, imdb, edition, id); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertFile(&MediaFile{ItemID: id, Path: "/m/" + key}, 1); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := add("a", 27205, "", "")               // TMDB only
	b := add("b", 0, "tt1375666", "")          // IMDb only
	c := add("c", 27205, "tt1375666", "")      // both: joins a and b
	d := add("d", 27205, "", "Director's Cut") // another edition
	e := add("e", 603, "tt0133093", "")
	f := add("f", 604, "tt0133093", "") // shares IMDb with e but TMDB disagrees
	g := add("g", 0, "", "")            // unmatched
	h := add("h", 0, "", "")
	if _, err := s.db.Exec(`UPDATE items SET metadata_source = 'none' WHERE id IN (?, ?)`, g, h); err != nil {
		t.Fatal(err)
	}

	merged, err := s.MergeDuplicates(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 {
		t.Fatalf("merged %+v, want b and c into a", merged)
	}
	left := map[int64]bool{}
	for _, id := range []int64{a, b, c, d, e, f, g, h} {
		if _, err := s.Item(0, id); err == nil {
			left[id] = true
		}
	}
	if len(left) != 6 || !left[a] || !left[d] || !left[e] || !left[f] || !left[g] || !left[h] {
		t.Errorf("left %v", left)
	}
	if files, _ := s.FilesForItem(a); len(files) != 3 {
		t.Errorf("kept movie has %d files, want 3 versions", len(files))
	}
	if it, _ := s.Item(0, a); it.IMDbID != "tt1375666" {
		t.Errorf("kept movie didn't learn the IMDb id: %q", it.IMDbID)
	}
	if id, _, ok := s.ItemAlias(lib.ID, "b"); !ok || id != a {
		t.Errorf("alias b → %d %v", id, ok)
	}
}
