package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"bingebandit/internal/store"
)

// A show folder removed or renamed right inside the library folder must
// rescan only that folder, not every file in the library.
func TestWatchRootLevelChangeRescansJustThatPath(t *testing.T) {
	dir := t.TempDir()
	tv := filepath.Join(dir, "TV Shows")
	writeFile(t, filepath.Join(tv, "Bluey", "Season 1", "Bluey S01E01.mkv"))
	writeFile(t, filepath.Join(tv, "Bluey", "Season 1", "Bluey S01E02.mkv"))
	writeFile(t, filepath.Join(tv, "Ghosts", "Season 1", "Ghosts S01E01.mkv"))

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "TV", Kind: "shows", Paths: []string{tv}}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser("admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	sc := NewScanner(st, nil, "ffprobe-missing")
	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	shows := func() int {
		t.Helper()
		items, _, err := st.LibraryItems(u.ID, lib.ID, "title", 0, 10, nil)
		if err != nil {
			t.Fatal(err)
		}
		return len(items)
	}
	if n := shows(); n != 2 {
		t.Fatalf("first scan: %d shows, want 2", n)
	}

	// Ghosts is moved out of the library.
	ghosts := filepath.Join(tv, "Ghosts")
	if err := os.RemoveAll(ghosts); err != nil {
		t.Fatal(err)
	}
	w := &watcher{s: sc, pending: map[int64]map[string]bool{}, timers: map[int64]*time.Timer{}}
	defer w.close()
	w.handle(nil, fsnotify.Event{Name: ghosts, Op: fsnotify.Remove})
	got := w.pending[lib.ID]
	if len(got) != 1 || !got[ghosts] {
		t.Fatalf("queued %v, want just %s (not the whole library)", got, ghosts)
	}

	st0 := &Status{}
	if err := sc.scanFolders(context.Background(), lib, st0, []string{ghosts}); err != nil {
		t.Fatal(err)
	}
	if st0.Found != 0 {
		t.Fatalf("rescan of a removed folder looked at %d files, want 0", st0.Found)
	}
	if n := shows(); n != 1 {
		t.Fatalf("after removing Ghosts: %d shows, want 1", n)
	}
}

// A loose video right in a movies library folder is rescanned on its own,
// and dropped by its own path once it's deleted.
func TestScanFoldersSingleFile(t *testing.T) {
	dir := t.TempDir()
	movies := filepath.Join(dir, "Movies")
	up := filepath.Join(movies, "Up (2009).mkv")
	writeFile(t, up)
	writeFile(t, filepath.Join(movies, "Coco (2017)", "Coco (2017).mkv"))

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{movies}}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser("admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	sc := NewScanner(st, nil, "ffprobe-missing")
	count := func() int {
		t.Helper()
		items, _, err := st.LibraryItems(u.ID, lib.ID, "title", 0, 10, nil)
		if err != nil {
			t.Fatal(err)
		}
		return len(items)
	}
	s1 := &Status{}
	if err := sc.scanFolders(context.Background(), lib, s1, []string{up}); err != nil {
		t.Fatal(err)
	}
	if s1.Found != 1 || count() != 1 {
		t.Fatalf("single-file rescan: found %d, %d items; want 1 and 1", s1.Found, count())
	}
	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	os.Remove(up)
	if err := sc.scanFolders(context.Background(), lib, &Status{}, []string{up}); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("after deleting Up: %d items, want 1 (Coco kept)", n)
	}
}
