// Package store owns the SQLite database: schema migrations and every query.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"bingebandit/internal/avatars"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

// ErrExists is returned when a name that must be unique is taken.
var ErrExists = errors.New("already exists")

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	// _txlock=immediate: transactions take the write lock when they begin.
	// A deferred one that reads first and then writes fails at once with
	// SQLITE_BUSY (or BUSY_SNAPSHOT, 517) if another connection wrote in
	// between; the busy timeout can't help it.
	dsn := path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	s.loadOverlayTag()
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() int64 { return time.Now().Unix() }

// ImageURL turns a stored image reference into a URL the clients can load.
// TMDB images are stored as "tmdb:/<file>" and served through the local cache;
// uploaded or downloaded artwork is "local:/<file>" in the data folder.
func ImageURL(ref string) string {
	if strings.HasPrefix(ref, "tmdb:/") {
		return "/api/images/tmdb/" + ref[len("tmdb:/"):]
	}
	if strings.HasPrefix(ref, "local:/") {
		return "/api/images/local/" + ref[len("local:/"):]
	}
	if name, ok := strings.CutPrefix(ref, avatars.RefPrefix); ok {
		return avatars.URL(name)
	}
	return ref
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
