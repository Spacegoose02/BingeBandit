package store

import (
	"fmt"
	"testing"
)

func TestMergeDuplicateShows(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "TV", Kind: "shows", Paths: []string{}}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser("sam", "x", false)
	if err != nil {
		t.Fatal(err)
	}
	// One show in two folders: "The Office (US)" has seasons 1-2, "The Office (2005)" season 2 again and 3.
	addEp := func(showKey, title string, season, ep int) (show, episode int64) {
		show, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "show", Title: title, MatchKey: showKey})
		if err != nil {
			t.Fatal(err)
		}
		sn, en := season, ep
		seasonID, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "season", ParentID: show, ShowID: show, Title: fmt.Sprintf("Season %d", season),
			SeasonNumber: &sn, MatchKey: fmt.Sprintf("%s:s%d", showKey, season)})
		if err != nil {
			t.Fatal(err)
		}
		episode, err = s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "episode", ParentID: seasonID, ShowID: show, Title: fmt.Sprintf("E%d", ep),
			SeasonNumber: &sn, EpisodeNumber: &en, MatchKey: fmt.Sprintf("%s:s%d:e%d", showKey, season, ep)})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertFile(&MediaFile{ItemID: episode, Path: fmt.Sprintf("/tv/%s/%d-%d.mkv", showKey, season, ep)}, 1); err != nil {
			t.Fatal(err)
		}
		return show, episode
	}
	a, _ := addEp("show:theofficeus:0", "The Office (US)", 1, 1)
	addEp("show:theofficeus:0", "The Office (US)", 1, 2)
	_, a21 := addEp("show:theofficeus:0", "The Office (US)", 2, 1)
	b, _ := addEp("show:theoffice:2005", "The Office", 2, 1)
	_, b31 := addEp("show:theoffice:2005", "The Office", 3, 1)
	other, _ := addEp("show:other:0", "Other", 1, 1)
	for _, id := range []int64{a, b} {
		if _, err := s.db.Exec(`UPDATE items SET tmdb_id = 2316 WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE items SET tmdb_id = 99 WHERE id = ?`, other); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWatched(u.ID, []int64{b31}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.AddToWatchlist(u.ID, b); err != nil {
		t.Fatal(err)
	}

	merged, err := s.MergeDuplicates(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || merged[0].KeptID != a {
		t.Fatalf("merged = %+v, want the second folder into show %d", merged, a)
	}
	if _, err := s.Item(0, b); err == nil {
		t.Error("duplicate show still there")
	}
	items, total, err := s.LibraryItems(u.ID, lib.ID, "title", 0, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("library lists %d shows, want 2", total)
	}
	show, err := s.Item(u.ID, a)
	if err != nil {
		t.Fatal(err)
	}
	if show.ChildCount != 4 { // 1x01, 1x02, 2x01 (two versions), 3x01
		t.Errorf("kept show has %d episodes, want 4", show.ChildCount)
	}
	if !show.OnWatchlist {
		t.Error("watchlist entry didn't follow the merge")
	}
	if files, _ := s.FilesForItem(a21); len(files) != 2 {
		t.Errorf("2x01 has %d files, want both copies", len(files))
	}
	var watched int
	s.db.QueryRow(`SELECT COUNT(*) FROM watch_state w JOIN items e ON e.id = w.item_id
		WHERE w.user_id = ? AND w.watched = 1 AND e.show_id = ? AND e.season_number = 3`, u.ID, a).Scan(&watched)
	if watched != 1 {
		t.Error("watched episode lost its watch state")
	}
	// A later scan of the merged folder lands in the kept show.
	id, key, ok := s.ItemAlias(lib.ID, "show:theoffice:2005")
	if !ok || id != a || key != "show:theofficeus:0" {
		t.Errorf("alias = %d %q %v", id, key, ok)
	}
	if again, err := s.MergeDuplicates(lib.ID); err != nil || len(again) != 0 {
		t.Errorf("second merge: %v %v", again, err)
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
