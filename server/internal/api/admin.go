package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/library"
	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":        s.Version,
		"os":             runtime.GOOS + "/" + runtime.GOARCH,
		"dataDir":        s.DataDir,
		"ffmpeg":         media.Version(s.FFmpeg),
		"ffprobe":        media.Version(s.FFprobe),
		"metadataActive": s.Metadata.Enabled(),
	})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Stats()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"nowPlaying": s.Activity.List(),
		"transcodes": s.Transcoder.List(),
	})
}

// killActivity stops a player's transcode and drops it from Now Playing.
func (s *Server) killActivity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	for _, np := range s.Activity.List() {
		if np.ID == id && np.TranscodeID != "" {
			s.Transcoder.Stop(np.TranscodeID)
		}
	}
	s.Activity.Stop(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Store.Settings()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

var validEncoders = map[string]bool{"none": true, "nvenc": true, "qsv": true, "vaapi": true, "amf": true, "videotoolbox": true}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	for k, v := range in {
		if _, known := store.SettingDefaults[k]; !known {
			writeErr(w, http.StatusBadRequest, "unknown setting "+k)
			return
		}
		in[k] = strings.TrimSpace(v)
	}
	if enc, ok := in[store.SettingHWEncoder]; ok && !validEncoders[enc] {
		writeErr(w, http.StatusBadRequest, "unknown hardware encoder")
		return
	}
	if dir := in[store.SettingTranscodeDir]; dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeErr(w, http.StatusBadRequest, "transcode folder is not writable: "+err.Error())
			return
		}
	}
	if err := s.Store.SetSettings(in); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.getSettings(w, r)
}

type dirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// browse lists folders on the server so admins can pick library paths.
func (s *Server) browse(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		writeJSON(w, http.StatusOK, map[string]any{"path": "", "parent": "", "dirs": roots()})
		return
	}
	p = filepath.Clean(p)
	entries, err := os.ReadDir(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	dirs := []dirEntry{}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasPrefix(e.Name(), "$") {
			dirs = append(dirs, dirEntry{e.Name(), filepath.Join(p, e.Name())})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	parent := filepath.Dir(p)
	if parent == p {
		parent = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": p, "parent": parent, "dirs": dirs})
}

func roots() []dirEntry {
	if runtime.GOOS != "windows" {
		return []dirEntry{{"/", "/"}}
	}
	var out []dirEntry
	for c := 'A'; c <= 'Z'; c++ {
		d := string(c) + `:\`
		if _, err := os.Stat(d); err == nil {
			out = append(out, dirEntry{d, d})
		}
	}
	return out
}

type libraryReq struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Paths []string `json:"paths"`
}

func (l *libraryReq) validate() error {
	l.Name = strings.TrimSpace(l.Name)
	if l.Name == "" {
		return errors.New("name is required")
	}
	if len(l.Paths) == 0 {
		return errors.New("add at least one folder")
	}
	for i, p := range l.Paths {
		p = filepath.Clean(strings.TrimSpace(p))
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			return errors.New("folder not found: " + p)
		}
		l.Paths[i] = p
	}
	return nil
}

func (s *Server) createLibrary(w http.ResponseWriter, r *http.Request) {
	var req libraryReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Kind != "movies" && req.Kind != "shows" {
		writeErr(w, http.StatusBadRequest, "kind must be movies or shows")
		return
	}
	if err := req.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	lib := &store.Library{Name: req.Name, Kind: req.Kind, Paths: req.Paths}
	if err := s.Store.CreateLibrary(lib); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.Scanner.Scan(lib.ID)
	writeJSON(w, http.StatusCreated, lib)
}

func (s *Server) updateLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	lib, err := s.Store.Library(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var req libraryReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := req.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	lib.Name, lib.Paths = req.Name, req.Paths
	if err := s.Store.UpdateLibrary(lib); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.Scanner.Scan(lib.ID)
	writeJSON(w, http.StatusOK, lib)
}

func (s *Server) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeleteLibrary(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) scanResult(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, library.ErrRunning):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		writeStoreErr(w, err)
	}
}

func (s *Server) scanLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	s.scanResult(w, s.Scanner.Scan(id))
}

func (s *Server) refreshLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	s.scanResult(w, s.Scanner.RefreshMetadata(id))
}

func (s *Server) scanAll(w http.ResponseWriter, r *http.Request) {
	s.Scanner.ScanAll()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) scanStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Scanner.Statuses())
}

func (s *Server) refreshItem(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if !s.Metadata.Enabled() {
		writeErr(w, http.StatusBadRequest, "add a TMDB API key in Settings first")
		return
	}
	if err := s.Metadata.RefreshItem(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	users, err := s.Store.ListUsers()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		credentials
		IsAdmin bool `json:"isAdmin"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := validCredentials(req.credentials); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.Store.UserByUsername(strings.TrimSpace(req.Username)); err == nil {
		writeErr(w, http.StatusConflict, "that username is taken")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	u, err := s.Store.CreateUser(strings.TrimSpace(req.Username), string(hash), req.IsAdmin)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if id == currentUser(r).ID {
		writeErr(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}
	if err := s.Store.DeleteUser(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil || len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err := s.Store.SetPassword(id, string(hash)); err != nil {
		writeStoreErr(w, err)
		return
	}
	// Sign the user out everywhere after an admin reset.
	s.Store.DeleteUserSessions(id)
	w.WriteHeader(http.StatusNoContent)
}
