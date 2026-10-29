package api

import (
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"syscall"

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

// deletePlan shows an admin what deleting a title from the server removes.
func (s *Server) deletePlan(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	plan, err := s.Scanner.PlanDelete(it)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// deleteFromServer deletes a movie, show, season or episode's files from
// disk and the title from its library. The admin types its title to confirm.
func (s *Server) deleteFromServer(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := readJSON(r, &req); err != nil || !strings.EqualFold(strings.TrimSpace(req.Confirm), strings.TrimSpace(it.Title)) {
		writeErr(w, http.StatusBadRequest, "type the title to confirm")
		return
	}
	err := s.Scanner.Delete(it)
	switch {
	case err == nil:
		log.Printf("admin %d deleted %s %q (item %d) from the server", currentUser(r).ID, it.Kind, it.Title, it.ID)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case errors.Is(err, library.ErrCannotDelete):
		writeErr(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), library.ErrCannotDelete.Error()+": "))
	case errors.Is(err, library.ErrRunning):
		writeErr(w, http.StatusConflict, "The library is being scanned. Try again when it's done.")
	case errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS):
		writeErr(w, http.StatusInternalServerError, "The server isn't allowed to delete files there: "+err.Error()+". Check that the media folder isn't mounted read-only in Docker.")
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}
