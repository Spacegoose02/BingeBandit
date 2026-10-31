package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// trickplayInfo describes the file's scrub previews. Missing ones are made in
// the background, so the answer is 404 until they're ready.
func (s *Server) trickplayInfo(w http.ResponseWriter, r *http.Request) {
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	if s.Trickplay == nil || !s.Trickplay.Enabled() {
		writeErr(w, http.StatusNotFound, "scrub previews are off")
		return
	}
	in, ok := s.Trickplay.Info(mf)
	if !ok {
		s.Trickplay.Queue(mf)
		writeErr(w, http.StatusNotFound, "scrub previews are being made")
		return
	}
	writeJSON(w, http.StatusOK, in)
}

// trickplaySheet serves sheet n (0-based) of the file's thumbnails.
func (s *Server) trickplaySheet(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(strings.TrimSuffix(chi.URLParam(r, "sheet"), ".jpg"))
	if err != nil || n < 0 {
		writeErr(w, http.StatusBadRequest, "bad sheet")
		return
	}
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	if s.Trickplay == nil {
		writeErr(w, http.StatusNotFound, "scrub previews are off")
		return
	}
	in, ok := s.Trickplay.Info(mf)
	if !ok || n >= in.Sheets {
		writeErr(w, http.StatusNotFound, "no such sheet")
		return
	}
	// Sheets only change when the file is replaced, so a day is safe to keep.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, s.Trickplay.Sheet(mf, n))
}

// trickplayBIF serves the thumbnails as a Roku BIF file. ?from= is where a
// conversion started, in seconds into the file.
func (s *Server) trickplayBIF(w http.ResponseWriter, r *http.Request) {
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	if s.Trickplay == nil || !s.Trickplay.Enabled() {
		writeErr(w, http.StatusNotFound, "scrub previews are off")
		return
	}
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	path, err := s.Trickplay.BIF(mf, from)
	if os.IsNotExist(err) {
		s.Trickplay.Queue(mf)
		writeErr(w, http.StatusNotFound, "scrub previews are being made")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not make the BIF file")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}
