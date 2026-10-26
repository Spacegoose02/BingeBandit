package store

import (
	"fmt"
	"testing"
)

func TestRewatchReturnsToContinueWatching(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	id := addMovie(t, s, lib.ID, "Heat")
	u, _ := s.CreateUser("dana", "x", false)
	s.SetUserLibraries(u.ID, true, nil)

	if _, err := s.SaveProgress(u.ID, id, 95, 100); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.InProgress(u.ID, 10); len(got) != 0 {
		t.Fatalf("a finished movie stayed in Continue Watching")
	}
	if _, err := s.SaveProgress(u.ID, id, 40, 100); err != nil {
		t.Fatal(err)
	}
	got, err := s.InProgress(u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Watch.Position != 40 {
		t.Fatalf("rewatch not in Continue Watching: %+v", got)
	}
}

func TestWatchedPercentSetting(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	id := addMovie(t, s, lib.ID, "Heat")
	u, _ := s.CreateUser("dana", "x", false)

	if done, _ := s.SaveProgress(u.ID, id, 85, 100); done {
		t.Fatal("85% counted as watched with the 90% default")
	}
	s.SetSettings(map[string]string{SettingWatchedPercent: "80"})
	if done, _ := s.SaveProgress(u.ID, id, 85, 100); !done {
		t.Fatal("85% not watched with the setting at 80%")
	}
}

func TestUpNextFollowsARewatch(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "TV", Kind: "shows", Paths: []string{}}
	s.CreateLibrary(lib)
	show, _ := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "show", Title: "Lost", MatchKey: "show:lost"})
	sn := 1
	season, _ := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "season", ParentID: show, ShowID: show, SeasonNumber: &sn, MatchKey: "show:lost:s1"})
	var eps []int64
	for e := 1; e <= 4; e++ {
		en := e
		id, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "episode", ParentID: season, ShowID: show, SeasonNumber: &sn, EpisodeNumber: &en,
			MatchKey: fmt.Sprintf("show:lost:s1:e%d", e)})
		if err != nil {
			t.Fatal(err)
		}
		s.UpsertFile(&MediaFile{ItemID: id, Path: fmt.Sprintf("/tv/lost/%d.mkv", e)}, 1)
		eps = append(eps, id)
	}
	u, _ := s.CreateUser("dana", "x", false)
	s.SetUserLibraries(u.ID, true, nil)

	// Episodes 1 and 3 seen, then 2 caught up: skip ahead to 4, the first unwatched one.
	s.SetWatched(u.ID, []int64{eps[0], eps[2]}, true)
	s.SetWatched(u.ID, []int64{eps[1]}, true)
	if next, err := s.UpNext(u.ID, show, 1, 2); err != nil || next.ID != eps[3] {
		t.Fatalf("catching up should skip to the first unwatched episode, got %+v %v", next, err)
	}

	// The whole show watched, then restarted: finishing 1 again offers 2.
	s.SetWatched(u.ID, []int64{eps[3]}, true)
	if next, err := s.UpNext(u.ID, show, 1, 1); err != nil || next.ID != eps[1] {
		t.Fatalf("rewatch should continue with the next episode, got %+v %v", next, err)
	}

	// After the finale there is nothing to offer.
	if _, err := s.UpNext(u.ID, show, 1, 4); err != ErrNotFound {
		t.Fatalf("finale: want ErrNotFound, got %v", err)
	}
}

func TestContinueWatchingDays(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	id := addMovie(t, s, lib.ID, "Heat")
	u, _ := s.CreateUser("dana", "x", false)
	s.SetUserLibraries(u.ID, true, nil)
	s.SaveProgress(u.ID, id, 40, 100)
	s.db.Exec(`UPDATE watch_state SET updated_at = ? WHERE item_id = ?`, now()-40*86400, id)
	if got, _ := s.InProgress(u.ID, 10); len(got) != 1 {
		t.Fatal("with no limit, an old start should stay")
	}
	s.SetSettings(map[string]string{SettingContinueDays: "30"})
	if got, _ := s.InProgress(u.ID, 10); len(got) != 0 {
		t.Fatal("a start 40 days ago stayed with a 30-day limit")
	}
}
