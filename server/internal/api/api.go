// Package api is the HTTP/JSON API used by the web UI and future TV/mobile apps.
package api

import (
	"context"
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
	"bingebandit/internal/livetv"
	"bingebandit/internal/logs"
	"bingebandit/internal/markers"
	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
	"bingebandit/internal/stream"
	"bingebandit/internal/tasks"
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
	Tasks      *tasks.Scheduler
	Markers    *markers.Detector
	Live       *livetv.Manager
	Logos      *livetv.LogoCache
	DVR        *livetv.Recorder
	Logs       *logs.Buffer
	Version    string
	DataDir    string
	FFmpeg     string
	FFprobe    string

	limiter     *loginLimiter
	linkLimiter *loginLimiter
	links       *linkManager
	oidc        *oidcFlows
	netp        netHolder
	ctx         context.Context // server lifetime, for work that outlives a request
}

func (s *Server) Router(ctx context.Context) http.Handler {
	s.limiter = newLoginLimiter()
	s.linkLimiter = newLoginLimiter()
	s.links = newLinkManager()
	s.oidc = newOIDCFlows()
	s.ctx = ctx
	s.reloadNetwork()
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(s.network)
	r.Use(requestLog)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
		})
		r.Get("/setup", s.setupStatus)
		r.Post("/setup", s.setup)
		r.Post("/auth/login", s.login)
		r.Post("/link/start", s.linkStart)
		r.Get("/link/poll", s.linkPoll)
		r.Get("/auth/oidc", s.oidcOptions)
		r.Get("/auth/oidc/callback", s.oidcCallback)
		r.Get("/auth/oidc/{id}/login", s.oidcLogin)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.logout)
			r.Get("/me", s.me)
			r.Put("/me/password", s.changePassword)
			r.Get("/me/prefs", s.getPrefs)
			r.Put("/me/prefs", s.putPrefs)
			r.Get("/me/history", s.myHistory)
			r.Post("/link/approve", s.linkApprove)
			r.Post("/me/2fa/setup", s.setup2FA)
			r.Post("/me/2fa/enable", s.enable2FA)
			r.Post("/me/2fa/disable", s.disable2FA)
			r.Get("/me/sessions", s.sessions)
			r.Delete("/me/sessions/{id}", s.revokeSession)
			r.Get("/me/keys", s.apiKeys)
			r.Post("/me/keys", s.createAPIKey)
			r.Delete("/me/keys/{id}", s.deleteAPIKey)
			r.Put("/me/share-activity", s.setShareActivity)
			r.Get("/me/identities", s.identities)
			r.Post("/me/identities/{id}/link", s.linkIdentity)
			r.Delete("/me/identities/{id}", s.unlinkIdentity)
			r.Get("/activity/friends", s.friendsActivity)
			r.Put("/me/profile", s.setProfileVisibility)
			r.Get("/profiles", s.publicProfiles)
			r.Get("/profiles/{username}", s.profile)

			r.Get("/watchlist", s.watchlist)
			r.Post("/watchlist/{id}", s.setWatchlist(true))
			r.Delete("/watchlist/{id}", s.setWatchlist(false))

			r.Get("/live/channels", s.liveChannels)
			r.Get("/live/guide", s.liveGuide)
			r.Post("/live/channels/{id}/favorite", s.setChannelFavorite(true))
			r.Delete("/live/channels/{id}/favorite", s.setChannelFavorite(false))
			r.Post("/live/channels/{id}/watch", s.watchChannel)
			r.Get("/live/channels/{id}/hls/{name}", s.liveHLS)
			r.Get("/live/channels/{id}/stream.ts", s.liveTS)
			r.Get("/live/channels/{id}/logo", s.channelLogo)
			r.Delete("/live/viewers/{viewer}", s.leaveChannel)
			r.Get("/dvr", s.recordings)
			r.Post("/dvr/recordings", s.scheduleRecording)
			r.Delete("/dvr/recordings/{id}", s.deleteRecording)
			r.Delete("/dvr/rules/{id}", s.deleteRule)
			r.Get("/live/playlist.m3u", s.livePlaylist)
			r.Get("/live/epg.xml", s.liveEPG)

			r.Get("/playlists", s.playlists)
			r.Post("/playlists", s.createPlaylist)
			r.Get("/playlists/{id}", s.playlist)
			r.Put("/playlists/{id}", s.renamePlaylist)
			r.Delete("/playlists/{id}", s.deletePlaylist)
			r.Post("/playlists/{id}/items", s.addToPlaylist)
			r.Delete("/playlists/{id}/items/{item}", s.removeFromPlaylist)
			r.Put("/playlists/{id}/order", s.reorderPlaylist)
			r.Get("/playlists/{id}/next", s.playlistNext)

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
				r.Put("/items/{id}/markers", s.setMarkers)
				r.Post("/items/{id}/detect-markers", s.detectMarkers)
				r.Get("/match", s.matchSearch)

				r.Get("/libraries/{id}/labels", s.libraryLabels)
				r.Post("/libraries/{id}/collections", s.createCollection)
				r.Put("/collections/{id}", s.updateCollection)
				r.Delete("/collections/{id}", s.deleteCollection)

				r.Get("/live/sources", s.liveSources)
				r.Post("/live/sources", s.saveLiveSource)
				r.Put("/live/sources/{id}", s.saveLiveSource)
				r.Delete("/live/sources/{id}", s.deleteLiveSource)
				r.Post("/live/sources/{id}/refresh", s.refreshLiveSource)
				r.Get("/live/streams", s.liveStreams)
				r.Get("/live/groups", s.liveGroups)
				r.Get("/live/channels", s.adminChannels)
				r.Post("/live/channels", s.createChannels)
				r.Put("/live/channels/{id}", s.updateChannel)
				r.Delete("/live/channels/{id}", s.deleteChannel)
				r.Get("/live/epg-channels", s.epgChannels)
				r.Get("/live/content-sources", s.contentSources)
				r.Post("/live/custom", s.saveCustomChannel)
				r.Get("/live/custom/{id}", s.customChannel)
				r.Put("/live/custom/{id}", s.saveCustomChannel)
				r.Get("/live/custom/{id}/schedule", s.customSchedule)
				r.Get("/live/activity", s.liveActivity)
				r.Delete("/live/activity/{id}", s.stopRelay)
				r.Delete("/live/viewers/{viewer}", s.kickViewer)

				r.Post("/dvr/recordings/{id}/approve", s.approveRecording)
				r.Post("/dvr/rules/{id}/approve", s.approveRule)

				r.Get("/history", s.adminHistory)
				r.Get("/statistics", s.adminStatistics)
				r.Get("/tasks", s.listTasks)
				r.Put("/tasks/{id}", s.configureTask)
				r.Post("/tasks/{id}/run", s.runTask)
				r.Get("/logs", s.logLines)
				r.Get("/logs/download", s.downloadLog)

				r.Put("/users/{id}/libraries", s.setUserLibraries)
				r.Delete("/users/{id}/2fa", s.adminReset2FA)
				r.Get("/webhooks", s.webhooks)
				r.Post("/webhooks", s.saveWebhook)
				r.Put("/webhooks/{id}", s.saveWebhook)
				r.Delete("/webhooks/{id}", s.deleteWebhook)
				r.Post("/webhooks/{id}/test", s.testWebhook)
				r.Get("/oidc", s.adminOIDC)
				r.Post("/oidc", s.saveOIDC)
				r.Put("/oidc/{id}", s.saveOIDC)
				r.Delete("/oidc/{id}", s.deleteOIDC)
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
