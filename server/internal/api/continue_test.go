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
