package api

import (
	"errors"
	"net/http"
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
