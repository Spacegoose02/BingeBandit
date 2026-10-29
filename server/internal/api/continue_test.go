package api

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"bingebandit/internal/store"
)

// We Bare Bears showed twice: two started episodes of a show, or the same
// show in two libraries, each got a card.
func TestContinueWatchingOneCardPerShow(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, _ := st.CreateUser("sam", "x", false)
	st.SetUserLibraries(u.ID, true, nil)
	show := func(lib, title string) []int64 {
		l := &store.Library{Name: lib, Kind: "shows", Paths: []string{}}
		st.CreateLibrary(l)
		key := lib + ":" + title
		sh, _ := st.UpsertItem(&store.Item{LibraryID: l.ID, Kind: "show", Title: title, MatchKey: key})
		sn := 1
		season, _ := st.UpsertItem(&store.Item{LibraryID: l.ID, Kind: "season", ParentID: sh, ShowID: sh, SeasonNumber: &sn, MatchKey: key + ":s1"})
		var eps []int64
		for e := 1; e <= 3; e++ {
			en := e
			id, err := st.UpsertItem(&store.Item{LibraryID: l.ID, Kind: "episode", ParentID: season, ShowID: sh, SeasonNumber: &sn,
				EpisodeNumber: &en, MatchKey: fmt.Sprintf("%s:e%d", key, e)})
			if err != nil {
				t.Fatal(err)
			}
			eps = append(eps, id)
		}
		return eps
	}
	kids := show("Kids TV", "We Bare Bears")
	tv := show("TV", "We Bare Bears")
	other := show("TV2", "Lost")
	progress := func(id int64) {
		if _, err := st.SaveProgress(u.ID, id, 300, 1200); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1100 * time.Millisecond) // updated_at is in seconds
	}
	progress(other[0])
	progress(kids[0])
	progress(kids[1])
	progress(tv[2])

	got, err := (&Server{Store: st}).continueWatching(u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != tv[2] || got[1].ID != other[0] {
		for _, it := range got {
			t.Logf("%d %s %s", it.ID, it.ShowTitle, it.Title)
		}
		t.Fatalf("want the latest We Bare Bears episode, then Lost; got %d cards", len(got))
	}
}

// Mickey Mouse Clubhouse dropped off Continue Watching: after the last
// episode there was no next one, so the show vanished from the row.
func TestContinueWatchingKeepsAShowAfterItsLastEpisode(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, _ := st.CreateUser("sam", "x", false)
	st.SetUserLibraries(u.ID, true, nil)
	l := &store.Library{Name: "Kids TV", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(l)
	sh, _ := st.UpsertItem(&store.Item{LibraryID: l.ID, Kind: "show", Title: "Mickey Mouse Clubhouse", MatchKey: "mmc"})
	sn := 1
	season, _ := st.UpsertItem(&store.Item{LibraryID: l.ID, Kind: "season", ParentID: sh, ShowID: sh, SeasonNumber: &sn, MatchKey: "mmc:s1"})
	var eps []int64
	for e := 1; e <= 3; e++ {
		en := e
		id, _ := st.UpsertItem(&store.Item{LibraryID: l.ID, Kind: "episode", ParentID: season, ShowID: sh, SeasonNumber: &sn,
			EpisodeNumber: &en, MatchKey: fmt.Sprintf("mmc:e%d", e)})
		eps = append(eps, id)
	}
	srv := &Server{Store: st}
	first := func() int64 {
		got, err := srv.continueWatching(u.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("want the show on the row, got %d cards", len(got))
		}
		return got[0].ID
	}

	// The finale watched first, episode 1 skipped: offer the skipped one.
	st.SaveProgress(u.ID, eps[1], 100, 100)
	time.Sleep(1100 * time.Millisecond)
	st.SaveProgress(u.ID, eps[2], 100, 100)
	if got := first(); got != eps[0] {
		t.Fatalf("want the skipped episode 1, got %d", got)
	}

	// Everything watched: keep the last episode they finished.
	time.Sleep(1100 * time.Millisecond)
	st.SaveProgress(u.ID, eps[0], 100, 100)
	time.Sleep(1100 * time.Millisecond)
	st.SaveProgress(u.ID, eps[2], 100, 100)
	if got := first(); got != eps[2] {
		t.Fatalf("want the finale they just watched, got %d", got)
	}
}
