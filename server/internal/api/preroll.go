package api

import (
	"io/fs"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/store"
)

// PreRollTrailer is another movie's trailer shown before a movie.
type PreRollTrailer struct {
	ItemID  int64  `json:"itemId"`
	Title   string `json:"title"`
	Year    int    `json:"year,omitempty"`
	YouTube string `json:"youtube"` // video key
}

// usRatings orders US movie ratings, so trailers before a movie are never
// rated above it.
var usRatings = map[string]int{"G": 1, "TV-Y": 1, "TV-G": 1, "PG": 2, "TV-PG": 2, "PG-13": 3, "TV-14": 3, "R": 4, "TV-MA": 4, "NC-17": 5}

// preRoll answers what to play before a movie, from the viewer's
// preferences: trailers of other movies (newest unwatched, never rated above
// this one) and the server's intro video.
func (s *Server) preRoll(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u := currentUser(r)
	it, err := s.Store.Item(u.ID, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	prefs, _ := s.Store.UserPrefs(u.ID)
	resp := map[string]any{"trailers": []PreRollTrailer{}}
	if it.Kind != "movie" {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if prefs.PreRollTrailers > 0 {
		if cands, err := s.Store.TrailerCandidates(u.ID, it.ID, 30); err == nil {
			resp["trailers"] = pickTrailers(cands, it.ContentRating, prefs.PreRollTrailers)
		}
	}
	if prefs.PreRollIntro {
		if files := s.introVideos(); len(files) > 0 {
			n := rand.IntN(len(files))
			resp["intro"] = map[string]any{"url": "/api/preroll/intro/" + strconv.Itoa(n), "name": strings.TrimSuffix(filepath.Base(files[n]), filepath.Ext(files[n]))}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// pickTrailers picks up to n at random from the candidates (newest first),
// leaving out any rated above rating when rating is a known US rating.
func pickTrailers(cands []*store.Item, rating string, n int) []PreRollTrailer {
	limit, known := usRatings[strings.ToUpper(strings.TrimSpace(rating))]
	var ok []*store.Item
	for _, c := range cands {
		if known {
			if rk, ck := usRatings[strings.ToUpper(strings.TrimSpace(c.ContentRating))]; !ck || rk > limit {
				continue
			}
		}
		ok = append(ok, c)
	}
	// Favour the newest: shuffle the first dozen only.
	if len(ok) > 12 {
		ok = ok[:12]
	}
	rand.Shuffle(len(ok), func(i, j int) { ok[i], ok[j] = ok[j], ok[i] })
	out := []PreRollTrailer{}
	for _, c := range ok[:min(n, len(ok))] {
		out = append(out, PreRollTrailer{ItemID: c.ID, Title: c.Title, Year: c.Year, YouTube: c.Trailer})
	}
	return out
}

var introExts = map[string]bool{".mp4": true, ".m4v": true, ".mov": true, ".webm": true, ".mkv": true}

// introVideos lists the intro videos from the admin's paths (files, or
// folders searched two levels deep), sorted so indexes stay stable.
func (s *Server) introVideos() []string {
	var out []string
	for _, line := range strings.Split(s.Store.Setting(store.SettingPreRollPaths), "\n") {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			if introExts[strings.ToLower(filepath.Ext(p))] {
				out = append(out, p)
			}
			continue
		}
		root := filepath.Clean(p)
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != root && (strings.HasPrefix(d.Name(), ".") || strings.Count(strings.TrimPrefix(path, root), string(filepath.Separator)) > 2) {
					return filepath.SkipDir
				}
				return nil
			}
			if introExts[strings.ToLower(filepath.Ext(path))] && !strings.HasPrefix(d.Name(), ".") {
				out = append(out, path)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// introVideo streams one intro video (with range requests, like direct play).
func (s *Server) introVideo(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	files := s.introVideos()
	if err != nil || n < 0 || n >= len(files) {
		writeErr(w, http.StatusNotFound, "no such intro video")
		return
	}
	f, err := os.Open(files[n])
	if err != nil {
		writeErr(w, http.StatusNotFound, "the intro video is not available")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot read file")
		return
	}
	if ct, ok := videoMime[strings.ToLower(filepath.Ext(files[n]))]; ok {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(files[n]), st.ModTime(), f)
}
