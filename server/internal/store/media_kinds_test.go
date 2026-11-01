package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The music migration widens the kind CHECKs in place. A server upgrading with a
// library full of items must keep every row (and everything that cascades
// from them) and then accept the new kinds.
func TestMediaKindsMigrationKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bb.db")
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	before := slices.IndexFunc(migrations, func(m string) bool { return strings.Contains(m, "writable_schema") })
	if before < 0 {
		t.Fatal("no music migration")
	}
	for i := 0; i < before; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	for _, q := range []string{
		fmt.Sprintf("PRAGMA user_version=%d", before),
		`INSERT INTO users (id, username, password_hash, created_at) VALUES (1, 'a', '', 0)`,
		`INSERT INTO libraries (id, name, kind, created_at) VALUES (1, 'Movies', 'movies', 0)`,
		`INSERT INTO items (id, library_id, kind, match_key, title, sort_title, added_at, updated_at) VALUES (1, 1, 'movie', 'm', 'Up', 'Up', 0, 0)`,
		`INSERT INTO watch_state (user_id, item_id, watched, updated_at) VALUES (1, 1, 1, 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO libraries (name, kind, created_at) VALUES ('Music', 'music', 0)`); err == nil {
		t.Fatal("the migrations before it already accept music libraries")
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lib := &Library{Name: "Music", Kind: KindMusic}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatalf("music library after upgrade: %v", err)
	}
	for _, kind := range []string{"artist", "album", "track", "audiobook", "chapter", "photoalbum", "photo"} {
		if _, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: kind, Title: kind, MatchKey: kind}); err != nil {
			t.Fatalf("%s item: %v", kind, err)
		}
	}
	if _, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "nonsense", Title: "x", MatchKey: "x"}); err == nil {
		t.Error("the CHECK no longer rejects unknown kinds")
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM watch_state WHERE item_id = 1 AND watched = 1`).Scan(&n)
	if n != 1 {
		t.Error("watch history lost in the upgrade")
	}
	var check string
	if err := s.db.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
		t.Fatalf("integrity_check = %q %v", check, err)
	}
}
