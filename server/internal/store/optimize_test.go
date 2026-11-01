package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lockDB opens a second connection to the store's file and holds its write
// lock until the returned func is called.
func lockDB(t *testing.T, path string) func() {
	t.Helper()
	other, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatal(err)
	}
	other.SetMaxOpenConns(1)
	if _, err := other.Exec(`BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Exec(`INSERT INTO settings (key, value) VALUES ('lock-test', 'x') ON CONFLICT(key) DO UPDATE SET value = 'y'`); err != nil {
		t.Fatal(err)
	}
	return func() {
		other.Exec(`COMMIT`)
		other.Close()
	}
}

func shortOptimizeWaits(t *testing.T) {
	oldTimeout, oldRetries := optimizeBusyTimeout, optimizeRetries
	optimizeBusyTimeout, optimizeRetries = 50*time.Millisecond, []time.Duration{100 * time.Millisecond, 100 * time.Millisecond}
	t.Cleanup(func() { optimizeBusyTimeout, optimizeRetries = oldTimeout, oldRetries })
}

func TestOptimizeWaitsOutABusyDatabase(t *testing.T) {
	shortOptimizeWaits(t)
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	unlock := lockDB(t, path)
	time.AfterFunc(120*time.Millisecond, unlock)
	result, err := s.Optimize(context.Background())
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if !strings.Contains(result, "compacted") {
		t.Errorf("result = %q, want compacted", result)
	}
}

func TestIsBusy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	unlock := lockDB(t, path)
	defer unlock()
	conn, _ := s.db.Conn(context.Background())
	defer conn.Close()
	conn.ExecContext(context.Background(), `PRAGMA busy_timeout = 0`)
	_, err = conn.ExecContext(context.Background(), `VACUUM`)
	if !IsBusy(err) {
		t.Errorf("IsBusy(%v) = false", err)
	}
	if IsBusy(nil) {
		t.Error("IsBusy(nil) = true")
	}
}
