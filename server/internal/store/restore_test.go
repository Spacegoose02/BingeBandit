package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreReplacesRows(t *testing.T) {
	s := openTest(t)
	if _, err := s.CreateUser("admin", "hash", true); err != nil {
		t.Fatal(err)
	}
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	addMovie(t, s, lib.ID, "Alien")
	backup := filepath.Join(t.TempDir(), "backup.db")
	if err := s.Backup(backup); err != nil {
		t.Fatal(err)
	}

	// Changes after the backup: a new user and movie, and a deleted one.
	if _, err := s.CreateUser("later", "hash", false); err != nil {
		t.Fatal(err)
	}
	addMovie(t, s, lib.ID, "Aliens")
	if _, err := s.db.Exec(`DELETE FROM items WHERE title = 'Alien'`); err != nil {
		t.Fatal(err)
	}

	// The admin signs in after the backup; that session should survive.
	if err := s.CreateSession("admin-token", 1, "Web", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("later-token", 2, "Web", ""); err != nil {
		t.Fatal(err)
	}

	if err := s.Restore(context.Background(), backup, "admin-token"); err != nil {
		t.Fatal(err)
	}
	if u, err := s.UserBySession("admin-token"); err != nil || u.Username != "admin" {
		t.Fatalf("admin session after restore: %v, %v", u, err)
	}
	if _, err := s.UserBySession("later-token"); err == nil {
		t.Fatal("a session for a user missing from the backup survived")
	}
	var titles []string
	rows, err := s.db.Query(`SELECT title FROM items ORDER BY title`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var title string
		rows.Scan(&title)
		titles = append(titles, title)
	}
	rows.Close()
	if strings.Join(titles, ",") != "Alien" {
		t.Fatalf("items after restore = %v, want [Alien]", titles)
	}
	if n, _ := s.CountUsers(); n != 1 {
		t.Fatalf("users after restore = %d, want 1", n)
	}
	// New rows still get fresh ids and foreign keys are enforced again.
	addMovie(t, s, lib.ID, "Prometheus")
	if _, err := s.db.Exec(`INSERT INTO items (library_id, kind, title, match_key) VALUES (9999, 'movie', 'x', 'x')`); err == nil {
		t.Fatal("foreign keys are off after restore")
	}
}

func TestRestoreRefusesBadBackups(t *testing.T) {
	s := openTest(t)
	if _, err := s.CreateUser("admin", "hash", true); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	noAdmin := filepath.Join(dir, "noadmin.db")
	other := openTest(t)
	if err := other.Backup(noAdmin); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(context.Background(), noAdmin, ""); err == nil || !strings.Contains(err.Error(), "admin") {
		t.Fatalf("restore without an admin: err = %v", err)
	}

	newer := filepath.Join(dir, "newer.db")
	if err := s.Backup(newer); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 100000`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := s.Restore(context.Background(), newer, ""); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("restore from a newer version: err = %v", err)
	}

	if err := s.Restore(context.Background(), filepath.Join(dir, "missing.db"), ""); err == nil {
		t.Fatal("restore of a missing file succeeded")
	}
	if n, _ := s.CountUsers(); n != 1 {
		t.Fatalf("a refused restore changed the database: %d users", n)
	}
}
