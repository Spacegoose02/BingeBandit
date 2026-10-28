package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

// Folders whose names differ only in punctuation are separate shows, and a
// scan splits ones an older scan put together.
func TestShowFoldersStayApart(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "TV")
	var files []found
	add := func(show, name string) {
		season := filepath.Join(root, show, "Season 1")
		os.MkdirAll(season, 0o755)
		p := filepath.Join(season, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, found{root, p})
	}
	add("Mickey Mouse Clubhouse", "Mickey Mouse Clubhouse - S01E01.mkv")
	add("Mickey Mouse Clubhouse", "Mickey Mouse Clubhouse - S01E02.mkv")
	add("Mickey Mouse Clubhouse+", "Mickey Mouse Clubhouse+ - S01E01.mkv")
	add("Bluey", "Bluey - S01E01.mkv")

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "TV", Kind: "shows", Paths: []string{root}}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser("admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	sc := NewScanner(st, nil, "ffprobe-missing")
	shows := func() map[string]*store.Item {
		t.Helper()
		items, _, err := st.LibraryItems(u.ID, lib.ID, "title", 0, 10, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*store.Item{}
		for _, it := range items {
			out[it.Title] = it
		}
		return out
	}

	// How scans used to file them: both Clubhouse folders in one show.
	for _, f := range files {
		if err := sc.processFile(context.Background(), lib, f, 1, nil); err != nil {
			t.Fatal(err)
		}
	}
	before := shows()
	if len(before) != 2 {
		t.Fatalf("old scan made %d shows, want the Clubhouses merged", len(before))
	}

	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	after := shows()
	if len(after) != 3 {
		t.Fatalf("shows = %v, want 3", after)
	}
	plain, plus := after["Mickey Mouse Clubhouse"], after["Mickey Mouse Clubhouse+"]
	if plain == nil || plus == nil {
		t.Fatalf("shows = %v", after)
	}
	if plain.ID != before["Mickey Mouse Clubhouse"].ID {
		t.Error("the plain folder should keep its show")
	}
	for show, want := range map[*store.Item]int{plain: 2, plus: 1} {
		eps, err := st.ShowEpisodes(u.ID, show.ID)
		if err != nil || len(eps) != want {
			t.Errorf("%s has %d episodes, want %d (%v)", show.Title, len(eps), want, err)
		}
		for _, e := range eps {
			fs, _ := st.FilesForItem(e.ID)
			if len(fs) != 1 || filepath.Base(filepath.Dir(filepath.Dir(fs[0].Path))) != show.Title {
				t.Errorf("%s %s plays %v", show.Title, e.Title, fs)
			}
		}
	}
}

func TestFolderKeys(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"Mickey Mouse Clubhouse+", "Mickey Mouse Clubhouse", "mickey mouse clubhouse", "Bluey", "Bluey (2018)"} {
		os.MkdirAll(filepath.Join(root, n), 0o755)
	}
	got := folderKeys([]string{root})
	want := map[string]string{
		"Mickey Mouse Clubhouse+": "show:mickeymouseclubhouse:0|Mickey Mouse Clubhouse+",
		"mickey mouse clubhouse":  "show:mickeymouseclubhouse:0|mickey mouse clubhouse",
	}
	if len(got) != len(want) {
		t.Fatalf("folderKeys = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%q → %q, want %q", k, got[k], v)
		}
	}
}
