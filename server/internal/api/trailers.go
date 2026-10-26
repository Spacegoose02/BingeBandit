package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/library"
	"bingebandit/internal/store"
)

// LocalTrailer is a trailer file found next to a movie (Plex's
// "-trailer" files and Trailers folder).
type LocalTrailer struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// localTrailers lists a movie's trailer files, from the folder of its first file.
func localTrailers(itemID int64, files []*store.MediaFile) []LocalTrailer {
	out := []LocalTrailer{}
	if len(files) == 0 {
		return out
	}
	for i, p := range library.LocalTrailers(files[0].Path) {
		name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		out = append(out, LocalTrailer{URL: "/api/items/" + strconv.FormatInt(itemID, 10) + "/trailers/" + strconv.Itoa(i), Name: name})
	}
	return out
}

// trailerFile streams a movie's nth trailer file to anyone who can see the movie.
func (s *Server) trailerFile(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if !ok || err != nil || n < 0 {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	it, err := s.Store.Item(currentUser(r).ID, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	files, err := s.Store.FilesForItem(it.ID)
	if err != nil || len(files) == 0 || it.Kind != "movie" {
		writeErr(w, http.StatusNotFound, "no trailers")
		return
	}
	paths := library.LocalTrailers(files[0].Path)
	if n >= len(paths) {
		writeErr(w, http.StatusNotFound, "no such trailer")
		return
	}
	serveVideoFile(w, r, paths[n])
}

// serveVideoFile streams a video with range requests, like direct play.
func serveVideoFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusNotFound, "the video is not available")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot read file")
		return
	}
	if ct, ok := videoMime[strings.ToLower(filepath.Ext(path))]; ok {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
}
