package api

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// A damaged database file fails some queries ("database disk image is
// malformed") while the rest work, so the server keeps running and shows
// wrong things (every season of every show "not here yet") instead of an
// error. Nightly backups fail too, but that used to show only on the Tasks
// and Health pages. Now the server checks the file at start, the backup and
// Optimize database tasks report what they find, and admins get an alert
// and a banner on every page until it's fixed.

// serverProblem is something an admin needs to fix, shown on every page.
type serverProblem struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Link  string `json:"link"`
}

// checkDatabase runs SQLite's quick check once and warns admins about
// damage.
func (s *Server) checkDatabase(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	problem, err := s.Store.CheckIntegrity(ctx)
	if err != nil {
		return // cancelled or timed out: no verdict
	}
	if problem == "" {
		s.setDatabaseProblem("")
		return
	}
	s.databaseDamaged(problem)
}

// setDatabaseProblem records what's wrong with the database ("" for
// nothing) and reports whether that's news.
func (s *Server) setDatabaseProblem(problem string) bool {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()
	changed := (s.health.dbProblem == "") != (problem == "")
	s.health.dbProblem = problem
	s.health.dbCheckedAt = time.Now()
	return changed
}

// databaseDamaged records damage and alerts admins the first time it's
// seen (and after a restart, once the start-up check finds it again).
func (s *Server) databaseDamaged(detail string) {
	if !s.setDatabaseProblem(detail) {
		return
	}
	p := damagedProblem(s.newestBackupNote())
	s.sendAlert(alert{Kind: "backup", Title: p.Title, Body: p.Body + "\n\n" + firstLines(detail, 4), Link: p.Link, Problem: true})
}

func damagedProblem(backupNote string) serverProblem {
	return serverProblem{
		Kind:  "database",
		Title: "The database is damaged",
		Body: "Some pages may fail or show the wrong thing, and nightly backups can't be made. " +
			"Run Optimize database under Tasks to repair it; if it can't, restore a backup from before the damage." + backupNote,
		Link: "/admin/tasks",
	}
}

// newestBackupNote names the newest backup, for alert and banner text.
func (s *Server) newestBackupNote() string {
	_, list, err := s.backupFiles()
	if err != nil || len(list) == 0 {
		return " There are no backups in the backup folder."
	}
	return " The newest backup is from " + time.Unix(list[0].Modified, 0).Local().Format("Mon Jan 2, 3:04 PM") + "."
}

// serverProblems is what admins should see a banner for right now.
func (s *Server) serverProblems() []serverProblem {
	out := []serverProblem{}
	s.health.mu.Lock()
	damaged, checked := s.health.dbProblem != "", s.health.dbCheckedAt
	s.health.mu.Unlock()
	// The backup task's last error is kept across restarts, so a failing
	// backup shows even before the start-up check has run. A check since
	// then (Optimize database repaired it, say) has the last word.
	var backupErr string
	if s.Tasks != nil {
		for _, t := range s.Tasks.List() {
			if t.ID == "backup_database" && t.Enabled && t.LastError != "" && checked.Unix() <= t.LastRun {
				backupErr = t.LastError
			}
		}
	}
	if !damaged && backupErr != "" && strings.Contains(backupErr, "malformed") {
		damaged = true
	}
	switch {
	case damaged:
		out = append(out, damagedProblem(s.newestBackupNote()))
	case backupErr != "":
		out = append(out, serverProblem{Kind: "backup", Title: "Database backups are failing",
			Body: backupErr + "." + s.newestBackupNote(), Link: "/admin/tasks"})
	}
	return out
}

// adminStatus is the small summary every admin page polls for its banner.
func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"problems": s.serverProblems()})
}
