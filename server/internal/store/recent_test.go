package store

import "testing"

func TestRecentlyAddedCutoff(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	addMovie(t, s, lib.ID, "Heat")
	old := addMovie(t, s, lib.ID, "Star Wars")
	if _, err := s.db.Exec(`UPDATE items SET added_at = ? WHERE id = ?`, now()-100*86400, old); err != nil {
		t.Fatal(err)
	}
	count := func() (int, int) {
		recent, err := s.RecentlyAdded(0, "movie", 10)
		if err != nil {
			t.Fatal(err)
		}
		row, err := s.RowItems(0, []int64{lib.ID}, "movies", "recent", "", 10)
		if err != nil {
			t.Fatal(err)
		}
		return len(recent), len(row)
	}
	if a, b := count(); a != 1 || b != 1 {
		t.Errorf("default 90 days: got %d and %d titles, want 1 (Star Wars was added 100 days ago)", a, b)
	}
	s.SetSettings(map[string]string{SettingRecentDays: "0"})
	if a, b := count(); a != 2 || b != 2 {
		t.Errorf("0 days = no limit: got %d and %d titles, want 2", a, b)
	}
}
