package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"bingebandit/internal/tasks"
)

// corruptErr looks like the SQLite driver's error for a damaged file.
type corruptErr struct{}

func (corruptErr) Error() string { return "database disk image is malformed (11)" }
func (corruptErr) Code() int     { return 11 }

// A damaged database used to show only as a failed task: now admins get one
// alert and a banner until a backup or Optimize database comes back clean.
func TestDamagedDatabaseIsFlagged(t *testing.T) {
	s, st := healthServer(t)
	backup := &tasks.Task{ID: "backup_database"}
	optimize := &tasks.Task{ID: "optimize_database"}

	if p := s.serverProblems(); len(p) != 0 {
		t.Fatalf("problems on a fresh server: %+v", p)
	}
	s.checkDatabase(context.Background())
	if p := s.serverProblems(); len(p) != 0 {
		t.Fatalf("a sound database flagged: %+v", p)
	}

	s.TaskDone(backup, "", corruptErr{})
	list := waitAlerts(t, s, 1)
	if list[0].Title != "The database is damaged" || !strings.Contains(list[0].Body, "Optimize database") {
		t.Fatalf("alert: %+v", list[0].alert)
	}
	if p := s.serverProblems(); len(p) != 1 || p[0].Kind != "database" {
		t.Fatalf("banner: %+v", p)
	}
	if notes := adminNotes(t, st); len(notes) != 1 || notes[0] != "The database is damaged" {
		t.Fatalf("admin notifications: %v", notes)
	}

	// The next night's failure doesn't alert again.
	s.TaskDone(backup, "", corruptErr{})
	s.TaskDone(optimize, "", errors.New("the database is damaged (x) and rebuilding its indexes didn't fix it; restore a backup"))
	if notes := adminNotes(t, st); len(notes) != 1 {
		t.Fatalf("alerted again: %v", notes)
	}

	// Optimize database repaired it.
	s.TaskDone(optimize, "repaired damaged indexes, updated statistics and compacted the database", nil)
	if p := s.serverProblems(); len(p) != 0 {
		t.Fatalf("still flagged after a clean check: %+v", p)
	}

	// Another kind of backup failure is its own alert and banner.
	s.TaskDone(backup, "", errors.New("no space left on device"))
	list = waitAlerts(t, s, 2)
	if list[0].Title != "The database backup failed" && list[1].Title != "The database backup failed" {
		t.Fatalf("alerts: %+v", list)
	}
}
