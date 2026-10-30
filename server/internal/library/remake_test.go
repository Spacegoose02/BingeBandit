package library

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

// A remake an older version merged into the original by a wrong match
// (The Lion King 2019 into 1994) gets its own movie back on the next scan.
func TestScanSplitsMergedRemake(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "Movies")
	writeFile(t, filepath.Join(root, "The Lion King (1994)", "The Lion King (1994).mkv"))
	writeFile(t, filepath.Join(root, "The Lion King (2019)", "The Lion King (2019).mkv"))
	dbPath := filepath.Join(dir, "t.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{root}}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	sc := NewScanner(st, nil, "ffprobe-missing")
	scan := func() {
		t.Helper()
		if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
			t.Fatal(err)
		}
	}
	scan()

	// What the old merge left: the 2019 folder's key aliased to the 1994 movie.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var orig int64
	if err := db.QueryRow(`SELECT id FROM items WHERE match_key = 'movie:thelionking:1994'`).Scan(&orig); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE media_files SET item_id = ? WHERE item_id = (SELECT id FROM items WHERE match_key = 'movie:thelionking:2019')`,
		`DELETE FROM items WHERE match_key = 'movie:thelionking:2019'`,
		`INSERT INTO item_aliases (library_id, match_key, item_id) VALUES (1, 'movie:thelionking:2019', ?)`,
	} {
		if _, err := db.Exec(q, orig); err != nil {
			t.Fatal(q, err)
		}
	}
	if id, _, ok := st.ItemAlias(lib.ID, "movie:thelionking:2019"); !ok || id != orig {
		t.Fatalf("setup: alias → %d %v", id, ok)
	}

	scan()
	if _, _, ok := st.ItemAlias(lib.ID, "movie:thelionking:2019"); ok {
		t.Error("the 2019 folder is still aliased to 1994")
	}
	if files, _ := st.FilesForItem(orig); len(files) != 1 {
		t.Errorf("1994 has %d files, want its own one", len(files))
	}
	var remake int64
	if err := db.QueryRow(`SELECT id FROM items WHERE match_key = 'movie:thelionking:2019'`).Scan(&remake); err != nil {
		t.Fatal("no 2019 movie after the scan:", err)
	}
	if files, _ := st.FilesForItem(remake); len(files) != 1 {
		t.Errorf("2019 has %d files, want 1", len(files))
	}
}
