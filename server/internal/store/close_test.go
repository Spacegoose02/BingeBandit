package store

import (
	"os"
	"path/filepath"
	"testing"
)

// A closed store leaves one self-contained file: the write-ahead log is
// copied back in and removed, so nothing depends on a -wal file surviving.
func TestCloseCheckpointsWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bingebandit.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskConfig("close_test", true, 6); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path + "-wal"); err == nil && info.Size() > 0 {
		t.Fatalf("-wal is %d bytes after Close", info.Size())
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok, err := s.TaskState("close_test"); err != nil || !ok {
		t.Fatalf("row lost after Close: ok=%v err=%v", ok, err)
	}
}
