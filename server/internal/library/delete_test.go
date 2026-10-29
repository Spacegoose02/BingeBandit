package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// Deleting an episode, a season, a show or a movie removes just its files
// (or its own folder) from disk and the title from the library, and leaves
// the library folder and everything else alone.
func TestDeleteFromServer(t *testing.T) {
	dir := t.TempDir()
	tv, movies := filepath.Join(dir, "TV"), filepath.Join(dir, "Movies")
	ep1 := filepath.Join(tv, "Bluey (2018)", "Season 1", "Bluey - S01E01.mkv")
	ep2 := filepath.Join(tv, "Bluey (2018)", "Season 1", "Bluey - S01E02.mkv")
	ep2srt := filepath.Join(tv, "Bluey (2018)", "Season 1", "Bluey - S01E02.en.srt")
	s2 := filepath.Join(tv, "Bluey (2018)", "Season 2")
	for _, p := range []string{ep1, ep2, ep2srt, filepath.Join(s2, "Bluey - S02E01.mkv"), filepath.Join(s2, "poster.jpg"),
		filepath.Join(tv, "The Wire", "Season 1", "The Wire - S01E01.mkv")} {
		writeFile(t, p)
	}
	cars := filepath.Join(movies, "Cars (2006).mkv")
	carsSrt := filepath.Join(movies, "Cars (2006).en.srt")
	up := filepath.Join(movies, "Up (2009).mkv")
	coco := filepath.Join(movies, "Coco (2017)", "Coco (2017).mkv")
	for _, p := range []string{cars, carsSrt, up, coco} {
		writeFile(t, p)
	}

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tvLib := &store.Library{Name: "TV Shows", Kind: "shows", Paths: []string{tv}}
	movieLib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{movies}}
	for _, l := range []*store.Library{tvLib, movieLib} {
		if err := st.CreateLibrary(l); err != nil {
			t.Fatal(err)
		}
	}
	u, err := st.CreateUser("admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	sc := NewScanner(st, nil, "ffprobe-missing")
	scan := func() {
		t.Helper()
		for _, l := range []*store.Library{tvLib, movieLib} {
			if err := sc.scanFiles(context.Background(), l, &Status{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	titles := func(l *store.Library) map[string]*store.Item {
		t.Helper()
		items, _, err := st.LibraryItems(u.ID, l.ID, "title", 0, 10, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*store.Item{}
		for _, it := range items {
			out[it.Title] = it
		}
		return out
	}
	scan()
	del := func(it *store.Item, want ...string) {
		t.Helper()
		plan, err := sc.PlanDelete(it)
		if err != nil || plan.Problem != "" {
			t.Fatalf("PlanDelete(%s) = %+v, %v", it.Title, plan, err)
		}
		if len(plan.Paths) != len(want) {
			t.Fatalf("PlanDelete(%s) paths = %v, want %v", it.Title, plan.Paths, want)
		}
		for i := range want {
			if plan.Paths[i] != want[i] {
				t.Fatalf("PlanDelete(%s) paths = %v, want %v", it.Title, plan.Paths, want)
			}
		}
		if err := sc.Delete(it); err != nil {
			t.Fatal(err)
		}
		for _, p := range want {
			if exists(p) {
				t.Fatalf("%s is still there", p)
			}
		}
		if _, err := st.Item(0, it.ID); err == nil {
			t.Fatalf("%s is still in the library", it.Title)
		}
	}

	bluey := titles(tvLib)["Bluey"]
	seasons, _ := st.Children(u.ID, bluey.ID)
	if len(seasons) != 2 {
		t.Fatalf("seasons = %d", len(seasons))
	}
	eps, _ := st.Children(u.ID, seasons[0].ID)
	del(eps[1], ep2srt, ep2)
	if !exists(ep1) {
		t.Fatal("the other episode went too")
	}
	// A season in its own folder goes with the folder, poster and all.
	del(seasons[1], s2)
	if left, _ := st.Children(u.ID, bluey.ID); len(left) != 1 {
		t.Fatalf("seasons left = %d", len(left))
	}
	del(titles(tvLib)["The Wire"], filepath.Join(tv, "The Wire"))
	// The last episode's season and show go from the library, and its
	// emptied folders from disk, but never the library folder.
	eps, _ = st.Children(u.ID, seasons[0].ID)
	del(eps[0], ep1)
	if exists(filepath.Join(tv, "Bluey (2018)")) || !isDir(tv) {
		t.Fatal("empty folders weren't tidied, or the library folder went")
	}
	if _, err := st.Item(0, bluey.ID); err == nil {
		t.Fatal("Bluey is still there with no episodes")
	}

	ms := titles(movieLib)
	del(ms["Cars"], carsSrt, cars)
	if !exists(up) {
		t.Fatal("Up went with Cars")
	}
	del(ms["Coco"], filepath.Join(movies, "Coco (2017)"))

	scan()
	if got := titles(movieLib); len(got) != 1 || got["Up"] == nil {
		t.Fatalf("movies after a scan = %v", got)
	}
	if got := titles(tvLib); len(got) != 0 {
		t.Fatalf("shows after a scan = %v", got)
	}
}
