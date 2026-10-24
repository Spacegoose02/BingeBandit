// Package api is the HTTP/JSON API used by the web UI and future TV/mobile apps.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"bingebandit/internal/library"
	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
	"bingebandit/internal/stream"
	"bingebandit/internal/web"
)

type Server struct {
	Store      *store.Store
	Scanner    *library.Scanner
	Metadata   *metadata.Service
	Images     *metadata.ImageCache
	Artwork    *metadata.Artwork
	Transcoder *stream.Manager
	Activity   *stream.Activity
	Version    string
	DataDir    string
	FFmpeg     string
	FFprobe    string

	limiter *loginLimiter
}

func (s *Server) Router() http.Handler {
	s.limiter = newLoginLimiter()
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(requestLog)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
		})
		r.Get("/setup", s.setupStatus)
		r.Post("/setup", s.setup)
		r.Post("/auth/login", s.login)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.logout)
			r.Get("/me", s.me)
			r.Put("/me/password", s.changePassword)

			r.Get("/home", s.home)
			r.Get("/libraries", s.libraries)
			r.Get("/libraries/{id}/items", s.libraryItems)
			r.Get("/items/{id}", s.item)
			r.Get("/items/{id}/next", s.nextEpisode)
			r.Post("/items/{id}/watched", s.setWatched(true))
			r.Post("/items/{id}/unwatched", s.setWatched(false))
			r.Get("/search", s.search)
			r.Get("/images/tmdb/{file}", s.tmdbImage)
			r.Get("/images/local/{file}", s.localImage)
			r.Get("/libraries/{id}/collections", s.libraryCollections)
			r.Get("/collections/{id}", s.collection)

			r.Post("/progress", s.progress)
			r.Get("/files/{id}/direct", s.directPlay)
			r.Get("/files/{id}/subtitles/{stream}", s.subtitle)
			r.Post("/files/{id}/transcode", s.startTranscode)
			r.Get("/transcode/{sid}/{name}", s.transcodeFile)
			r.Delete("/transcode/{sid}", s.stopTranscode)

			r.Route("/admin", func(r chi.Router) {
				r.Use(requireAdmin)
				r.Get("/system", s.system)
				r.Get("/stats", s.stats)
				r.Get("/activity", s.activity)
				r.Delete("/activity/{id}", s.killActivity)
				r.Get("/settings", s.getSettings)
				r.Put("/settings", s.putSettings)
				r.Get("/browse", s.browse)

				r.Get("/libraries", s.libraries)
				r.Post("/libraries", s.createLibrary)
				r.Put("/libraries/{id}", s.updateLibrary)
				r.Delete("/libraries/{id}", s.deleteLibrary)
				r.Post("/libraries/{id}/scan", s.scanLibrary)
				r.Post("/libraries/{id}/refresh", s.refreshLibrary)
				r.Post("/scan", s.scanAll)
				r.Get("/scan", s.scanStatus)
				r.Post("/items/{id}/refresh", s.refreshItem)
				r.Put("/items/{id}", s.editItem)
				r.Get("/items/{id}/artwork", s.artworkOptions)
				r.Put("/items/{id}/artwork", s.setArtwork)
				r.Post("/items/{id}/artwork/upload", s.uploadArtwork)
				r.Post("/items/{id}/match", s.matchItem)
				r.Get("/match", s.matchSearch)

				r.Get("/libraries/{id}/labels", s.libraryLabels)
				r.Post("/libraries/{id}/collections", s.createCollection)
				r.Put("/collections/{id}", s.updateCollection)
				r.Delete("/collections/{id}", s.deleteCollection)

				r.Put("/users/{id}/libraries", s.setUserLibraries)
				r.Get("/users", s.users)
				r.Post("/users", s.createUser)
				r.Delete("/users/{id}", s.deleteUser)
				r.Put("/users/{id}/password", s.resetPassword)
			})
		})
	})
	r.Handle("/*", web.Handler())
	return r
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		// Segment and image requests would drown everything else out.
		if ww.Status() >= 400 || (r.Method != http.MethodGet && r.URL.Path != "/api/progress") {
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, ww.Status(), time.Since(start).Round(time.Millisecond))
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeStoreErr maps a store error to 404 or 500.
func writeStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	log.Printf("store: %v", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func readJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	return json.NewDecoder(r.Body).Decode(v)
}

func idParam(r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n, err == nil && n > 0
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
