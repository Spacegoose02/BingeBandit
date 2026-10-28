package api

import (
	"errors"
	"net/http"
	"strings"

	"bingebandit/internal/library"
)

// itemLocation shows an admin where a movie or show is on disk, to check
// its match, and the library folders it could move to.
func (s *Server) itemLocation(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	loc, err := s.Scanner.Locate(it)
	if errors.Is(err, library.ErrCannotMove) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	targets, err := s.Scanner.MoveTargets(loc)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"location": loc, "targets": targets})
}

// moveItem moves a movie or show's folder into another library's folder
// (Kids TV and back, Movies to 4K, …), and the title with it.
func (s *Server) moveItem(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	var req struct {
		LibraryID int64  `json:"libraryId"`
		Path      string `json:"path"`
	}
	if err := readJSON(r, &req); err != nil || req.LibraryID == 0 || strings.TrimSpace(req.Path) == "" {
		writeErr(w, http.StatusBadRequest, "pick a library folder")
		return
	}
	copying, err := s.Scanner.Move(it, req.LibraryID, req.Path)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]bool{"copying": copying})
	case errors.Is(err, library.ErrCannotMove):
		writeErr(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), library.ErrCannotMove.Error()+": "))
	case errors.Is(err, library.ErrRunning):
		writeErr(w, http.StatusConflict, "One of the libraries is being scanned. Try again when it's done.")
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}
