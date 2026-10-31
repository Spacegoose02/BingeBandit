package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

// A drive that goes away often leaves its mount point behind, empty. Scanning
// it must keep the library (and its watch history) instead of pruning it all.
func TestScanKeepsLibraryOnEmptyMountPoint(t *testing.T) {
	dir := t.TempDir()
	movies := filepath.Join(dir, "Movies")
	writeFile(t, filepath.Join(movies, "Up (2009).mkv"))
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
	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 2 {
		t.Fatalf("first scan: %d items, want 2", n)
	}

	// The drive is unplugged: the folder is still there, but empty.
	entries, _ := os.ReadDir(movies)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(movies, e.Name()))
	}
	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 2 {
		t.Fatalf("full scan of an empty mount point: %d items, want 2 kept", n)
	}
	if err := sc.scanFolders(context.Background(), lib, &Status{}, []string{movies, filepath.Join(movies, "Coco (2017)")}); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 2 {
		t.Fatalf("folder rescan of an empty mount point: %d items, want 2 kept", n)
	}

	// One movie really deleted while the other stays: that's pruned as usual.
	writeFile(t, filepath.Join(movies, "Up (2009).mkv"))
	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("after deleting one movie: %d items, want 1", n)
	}
}

// A new library whose folder is still empty scans fine (nothing to keep).
func TestScanEmptyNewLibrary(t *testing.T) {
	dir := t.TempDir()
	movies := filepath.Join(dir, "Movies")
	os.MkdirAll(movies, 0o755)
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{movies}}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	sc := NewScanner(st, nil, "ffprobe-missing")
	if sc.unmounted(lib, movies) {
		t.Fatal("an empty new library was treated as an unmounted drive")
	}
}
