package store

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// damagedItems builds a database with a few thousand movies, a backup of
// it, a user added after the backup, and one leaf page of the items table
// overwritten with junk (so the table itself, not an index, is damaged).
func damagedItems(t *testing.T) (s *Store, path, backup string, lib int64) {
	return damagedFile(t, func(id int64) string { return fmt.Sprintf("marker-%d!", id) })
}

// damagedFile damages the page holding the text find returns for the id of
// movie 1500.
func damagedFile(t *testing.T, find func(id int64) string) (s *Store, path, backup string, lib int64) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "bingebandit.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser("admin", "hash", true); err != nil {
		t.Fatal(err)
	}
	l := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	if err := s.CreateLibrary(l); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3000; n++ {
		addMovie(t, s, l.ID, fmt.Sprintf("Movie-%04d %s", n, strings.Repeat("x", 60)))
	}
	// A marker only the table holds (titles are in an index too).
	if _, err := s.db.Exec(`UPDATE items SET overview = 'marker-' || id || '!'`); err != nil {
		t.Fatal(err)
	}
	backup = filepath.Join(dir, "backups", "bingebandit-20261108-020254.db")
	os.MkdirAll(filepath.Dir(backup), 0o755)
	if err := s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	// After the backup: a new user, which only the damaged file has.
	if _, err := s.CreateUser("later", "hash", false); err != nil {
		t.Fatal(err)
	}
	var pageSize int64
	s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
	s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	s.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var mid int64
	s2, _ := Open(path)
	s2.db.QueryRow(`SELECT id FROM items WHERE title LIKE 'Movie-1500 %'`).Scan(&mid)
	s2.Close()
	at := bytes.Index(data, []byte(find(mid)))
	if at < 0 {
		t.Fatal("movie row not found in the file")
	}
	page := int64(at) / pageSize
	f, _ := os.OpenFile(path, os.O_RDWR, 0)
	f.WriteAt(bytes.Repeat([]byte{0xA5}, int(pageSize)), page*pageSize)
	f.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path, backup, l.ID
}

func countItems(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&n); err != nil {
		t.Fatalf("count items: %v", err)
	}
	return n
}

// A damaged items table used to end Optimize database with "restore a
// backup", losing everything since it. Now it's salvaged: rows that read
// are kept, the rest come back from the backup.
func TestOptimizeSalvagesDamagedTable(t *testing.T) {
	s, path, backup, _ := damagedItems(t)
	var n int
	if s.db.QueryRow(`SELECT COUNT(*) FROM items NOT INDEXED`).Scan(&n) == nil {
		t.Fatal("the items table isn't damaged")
	}

	result, err := s.Optimize(context.Background(), backup)
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if !strings.Contains(result, "rebuilt the damaged database") || !strings.Contains(result, "came back from bingebandit-20261108-020254.db") {
		t.Fatalf("result: %q", result)
	}
	if n := countItems(t, s); n != 3000 {
		t.Fatalf("items after salvage = %d, want 3000", n)
	}
	if n, _ := s.CountUsers(); n != 2 {
		t.Fatalf("users after salvage = %d, want 2 (the one added after the backup kept)", n)
	}
	if problem, err := s.CheckIntegrity(context.Background()); err != nil || problem != "" {
		t.Fatalf("integrity after salvage: %q %v", problem, err)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "damaged-*.db"))
	if len(matches) != 1 {
		t.Fatalf("damaged copy: %v", matches)
	}
	// Still a working database: new rows, foreign keys on.
	if _, err := s.db.Exec(`INSERT INTO items (library_id, kind, title, match_key) VALUES (9999, 'movie', 'x', 'x')`); err == nil {
		t.Fatal("foreign keys are off after salvage")
	}
}

// Without a backup the rows that read are still kept.
func TestOptimizeSalvagesWithoutBackup(t *testing.T) {
	s, _, _, _ := damagedItems(t)
	result, err := s.Optimize(context.Background())
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if !strings.Contains(result, "no sound backup") {
		t.Fatalf("result: %q", result)
	}
	if n := countItems(t, s); n < 2900 || n >= 3000 {
		t.Fatalf("items after salvage = %d, want most of 3000", n)
	}
}

// Restoring over a damaged database used to fail twice: the safety backup
// first (it reads every row) and then clearing the damaged table.
func TestRestoreOverDamagedDatabase(t *testing.T) {
	s, path, backup, _ := damagedItems(t)
	if err := s.CreateSession("admin-token", 1, "Web", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(filepath.Join(t.TempDir(), "x.db")); err == nil || !IsCorrupt(err) {
		t.Fatalf("a normal backup of the damaged file: %v", err)
	}
	copyPath := filepath.Join(filepath.Dir(path), "damaged-copy.db")
	if err := s.SaveDamagedCopy(context.Background(), copyPath); err != nil {
		t.Fatalf("damaged copy: %v", err)
	}
	if err := s.Restore(context.Background(), backup, "admin-token"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if n := countItems(t, s); n != 3000 {
		t.Fatalf("items after restore = %d", n)
	}
	if n, _ := s.CountUsers(); n != 1 {
		t.Fatalf("users after restore = %d, want the backup's 1", n)
	}
	if u, err := s.UserBySession("admin-token"); err != nil || u.Username != "admin" {
		t.Fatalf("admin signed out: %v %v", u, err)
	}
	if problem, err := s.CheckIntegrity(context.Background()); err != nil || problem != "" {
		t.Fatalf("integrity after restore: %q %v", problem, err)
	}
}

// Damage to an index page that a VACUUM then trips over (it failed the
// rebuild with "restore a backup") is salvaged too.
func TestOptimizeSalvagesWhenIndexRebuildFails(t *testing.T) {
	s, _, backup, _ := damagedFile(t, func(int64) string { return "movie:Movie-1500 " })
	if _, err := s.Optimize(context.Background(), backup); err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if n := countItems(t, s); n != 3000 {
		t.Fatalf("items = %d", n)
	}
	if n, _ := s.CountUsers(); n != 2 {
		t.Fatalf("users = %d", n)
	}
}
