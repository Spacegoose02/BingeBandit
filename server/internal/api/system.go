package api

import (
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/tasks"
)

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Tasks.List())
}

func (s *Server) configureTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled       bool `json:"enabled"`
		IntervalHours int  `json:"intervalHours"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Tasks.Configure(chi.URLParam(r, "id"), req.Enabled, req.IntervalHours); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runTask(w http.ResponseWriter, r *http.Request) {
	// Detach from the request so the task outlives it.
	err := s.Tasks.RunNow(s.ctx, chi.URLParam(r, "id"))
	switch {
	case errors.Is(err, tasks.ErrRunning):
		writeErr(w, http.StatusConflict, err.Error())
	case err != nil:
		writeErr(w, http.StatusNotFound, err.Error())
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}

// logLines returns buffered log lines newer than ?after=<seq>.
func (s *Server) logLines(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	writeJSON(w, http.StatusOK, s.Logs.Since(after))
}

func (s *Server) downloadLog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="bingebandit.log"`)
	http.ServeFile(w, r, s.Logs.Path())
}

type backupFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

var backupName = regexp.MustCompile(`^bingebandit-[0-9]{8}-[0-9]{6}\.db$`)

// listBackups lists the database backups in the backup folder, newest first.
func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	dir := tasks.BackupDir(s.Store, s.DataDir)
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []backupFile{}
	for _, e := range entries {
		if e.IsDir() || !backupName.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, backupFile{e.Name(), info.Size(), info.ModTime().Unix()})
		}
	}
	// Names carry the time, so they sort newest first in reverse.
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"dir": dir, "backups": out})
}

// restoreBackup backs up the current database, then replaces its contents
// with the named backup.
func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !backupName.MatchString(name) {
		writeErr(w, http.StatusBadRequest, "bad backup name")
		return
	}
	dir := tasks.BackupDir(s.Store, s.DataDir)
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		writeErr(w, http.StatusNotFound, "backup not found")
		return
	}
	safety, err := tasks.SaveBackup(s.Store, dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not back up the current database first: "+err.Error())
		return
	}
	if err := s.Store.Restore(r.Context(), path, requestToken(r)); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.reloadNetwork()
	log.Printf("database restored from %s (previous data saved as %s)", name, filepath.Base(safety))
	writeJSON(w, http.StatusOK, map[string]string{"restored": name, "previous": filepath.Base(safety)})
}
