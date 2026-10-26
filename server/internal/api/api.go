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
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"bingebandit/internal/avatars"
	"bingebandit/internal/importer"
	"bingebandit/internal/library"
	"bingebandit/internal/livetv"
	"bingebandit/internal/logs"
	"bingebandit/internal/markers"
	"bingebandit/internal/metadata"
	"bingebandit/internal/plugins"
	"bingebandit/internal/sports"
	"bingebandit/internal/store"
	"bingebandit/internal/stream"
	"bingebandit/internal/tasks"
	"bingebandit/internal/trakt"
	"bingebandit/internal/web"
)

type Server struct {
	Store      *store.Store
	Scanner    *library.Scanner
	Metadata   *metadata.Service
	Images     *metadata.ImageCache
	Artwork    *metadata.Artwork
	TPDb       *metadata.TPDb
	Transcoder *stream.Manager
	Activity   *stream.Activity
	Tasks      *tasks.Scheduler
	Markers    *markers.Detector
	Live       *livetv.Manager
	Logos      *livetv.LogoCache
	LogoLib    *livetv.LogoLibrary
	DVR        *livetv.Recorder
	Importer   *importer.Runner
	Trakt      *trakt.Syncer
	Plugins    *plugins.Host
	Logs       *logs.Buffer
	Version    string
	DataDir    string
	FFmpeg     string
	FFprobe    string
	UpdateRepo string // GitHub "owner/repo" whose releases carry the TV app

	updates     appUpdates
	limiter     *loginLimiter
	linkLimiter *loginLimiter
	links       *linkManager
	oidc        *oidcFlows
	passkeys    passkeyFlows
	together    togetherRooms
	netp        netHolder
	unlocks     unlocks
	restoreMu   sync.Mutex // one restore at a time
	dispatcharr livetv.DispatcharrJob
	// ratingFetches holds items whose external ratings are being looked up.
	ratingFetches sync.Map
	overlays      overlayState
	ctx           context.Context // server lifetime, for work that outlives a request
	// Sports reads ESPN for the Sports page and the game alerts.
	Sports     *sports.Client
	sportsOnce sync.Once
	sportsWake chan struct{}
}

func (s *Server) Router(ctx context.Context) http.Handler {
	s.limiter = newLoginLimiter()
	s.linkLimiter = newLoginLimiter()
	s.links = newLinkManager()
	s.oidc = newOIDCFlows()
	s.ctx = ctx
	if s.Activity != nil {
		s.Activity.OnExpire = s.playerExpired
		go s.sweepPlayers(ctx)
	}
	s.reloadNetwork()
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(s.network)
	r.Use(requestLog)

	r.Route("/api", func(r chi.Router) {
		// Apps scan the local network for this to find servers, so it names itself.
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			writeJSON(w, http.StatusOK, map[string]string{
				"status": "ok", "version": s.Version, "product": "BingeBandit", "name": s.Store.Setting(store.SettingServerName),
			})
		})
		r.Get("/setup", s.setupStatus)
		r.Post("/setup", s.setup)
		r.Post("/setup/restore", s.setupRestore)
		r.Post("/auth/login", s.login)
		r.Post("/auth/passkey/begin", s.passkeyLoginBegin)
		r.Post("/auth/passkey/finish", s.passkeyLoginFinish)
		r.Post("/link/start", s.linkStart)
		r.Get("/link/poll", s.linkPoll)
		r.Get("/auth/oidc", s.oidcOptions)
		r.Get("/auth/oidc/callback", s.oidcCallback)
		r.Get("/auth/oidc/{id}/login", s.oidcLogin)
		r.Route("/plugin/v1", s.pluginAPI)
		r.Handle("/avatars/*", avatars.Handler())
		r.Route("/live/tuner/{key}", s.tunerRoutes)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.settingsLock)
			r.Post("/auth/logout", s.logout)
			r.Get("/me/passcode", s.passcodeStatus)
			r.Put("/me/passcode", s.setPasscode)
			r.Delete("/me/passcode", s.removePasscode)
			r.Post("/me/passcode/unlock", s.unlockSettings)
			r.Post("/me/passcode/lock", s.lockSettings)
			r.Get("/household", s.household)
			r.Post("/household", s.createProfile)
			r.Put("/household/{id}", s.updateProfile)
			r.Put("/household/{id}/pin", s.setProfilePIN)
			r.Delete("/household/{id}", s.deleteProfile)
			r.Post("/household/{id}/switch", s.switchProfile)
			r.Put("/household/autoselect", s.setAutoProfile)
			r.Put("/household/{id}/avatar", s.setAvatar)
			r.Delete("/household/{id}/avatar", s.removeAvatar)
			r.Post("/household/{id}/avatar/roll", s.rollAvatar)
			r.Get("/me", s.me)
			r.Put("/me/password", s.changePassword)
			r.Get("/me/recommendations", s.getRecommendOptIn)
			r.Put("/me/recommendations", s.setRecommendOptIn)
			r.Get("/items/{id}/recommend", s.recommendRecipients)
			r.Post("/together", s.startTogether)
			r.Get("/together/invites", s.myTogetherInvites)
			r.Get("/together/{id}", s.getTogether)
			r.Post("/together/{id}/state", s.updateTogether)
			r.Get("/together/{id}/events", s.togetherEvents)
			r.Get("/together/{id}/people", s.togetherPeople)
			r.Post("/together/{id}/invite", s.inviteTogether)
			r.Delete("/together/{id}/invite", s.declineTogether)
			r.Post("/items/{id}/recommend", s.recommend)
			r.Delete("/recommendations/{id}", s.dismissRecommendation)
			r.Get("/sports", s.sportsHome)
			r.Put("/sports/teams", s.setSportsTeams)
			r.Get("/sports/{league}/teams", s.sportsTeams)
			r.Get("/sports/{league}/scores", s.sportsScores)
			r.Get("/notifications", s.notifications)
			r.Post("/notifications/read", s.readNotifications)
			r.Delete("/notifications", s.clearNotifications)
			r.Get("/me/prefs", s.getPrefs)
			r.Get("/me/pins", s.pins)
			r.Put("/me/pins", s.setPins)
			r.Put("/me/prefs", s.putPrefs)
			r.Get("/me/history", s.myHistory)
			r.Post("/link/approve", s.linkApprove)
			r.Post("/me/2fa/setup", s.setup2FA)
			r.Post("/me/2fa/enable", s.enable2FA)
			r.Post("/me/2fa/disable", s.disable2FA)
			r.Get("/me/passkeys", s.myPasskeys)
			r.Post("/me/passkeys/begin", s.passkeyRegisterBegin)
			r.Post("/me/passkeys", s.passkeyRegisterFinish)
			r.Put("/me/passkeys/{id}", s.renamePasskey)
			r.Delete("/me/passkeys/{id}", s.deletePasskey)
			r.Get("/me/sessions", s.sessions)
			r.Delete("/me/sessions/{id}", s.revokeSession)
			r.Get("/me/keys", s.apiKeys)
			r.Post("/me/keys", s.createAPIKey)
			r.Delete("/me/keys/{id}", s.deleteAPIKey)
			r.Put("/me/share-activity", s.setShareActivity)
			r.Get("/apps/android", s.androidApp)
			r.Get("/me/trakt", s.traktStatus)
			r.Post("/me/trakt/connect", s.traktConnect)
			r.Post("/me/trakt/poll", s.traktPoll)
			r.Put("/me/trakt", s.traktSettings)
			r.Delete("/me/trakt", s.traktDisconnect)
			r.Post("/me/trakt/push", s.traktPush)
			r.Post("/me/trakt/import", s.traktImport)
			r.Get("/me/export/history.csv", s.exportHistory("history"))
			r.Get("/me/export/letterboxd.csv", s.exportHistory("letterboxd"))
			r.Get("/me/identities", s.identities)
			r.Post("/me/identities/{id}/link", s.linkIdentity)
			r.Delete("/me/identities/{id}", s.unlinkIdentity)
			r.Get("/activity/friends", s.friendsActivity)
			r.Put("/me/profile", s.setProfileVisibility)
			r.Get("/profiles", s.publicProfiles)
			r.Get("/profiles/{username}", s.profile)

			r.Get("/plugins", s.pluginPages)
			r.HandleFunc("/plugins/{id}/*", s.pluginProxy)

			r.Get("/discover", s.discoverRows)
			r.Get("/watchlist", s.watchlist)
			r.Post("/watchlist/{id}", s.setWatchlist(true))
			r.Delete("/watchlist/{id}", s.setWatchlist(false))

			// Live TV and the DVR, unless an admin turned them off for the user.
			r.Group(func(r chi.Router) {
				r.Use(requireLiveTV)
				r.Get("/live/channels", s.liveChannels)
				r.Get("/live/guide", s.liveGuide)
				r.Post("/live/channels/{id}/favorite", s.setChannelFavorite(true))
				r.Delete("/live/channels/{id}/favorite", s.setChannelFavorite(false))
				r.Post("/live/channels/{id}/watch", s.watchChannel)
				r.Get("/live/channels/{id}/hls/{name}", s.liveHLS)
				r.Get("/live/channels/{id}/stream.ts", s.liveTS)
				r.Get("/live/channels/{id}/logo", s.channelLogo)
				r.Get("/live/logos/{file}", s.libraryLogo)
				r.Get("/live/categories", s.liveCategories)
				r.Post("/live/categories", s.saveCategory)
				r.Put("/live/categories/order", s.orderCategories)
				r.Put("/live/categories/{id}", s.saveCategory)
				r.Delete("/live/categories/{id}", s.deleteCategory)
				r.Post("/live/categories/{id}/channels/{channel}", s.setChannelCategory(true))
				r.Delete("/live/categories/{id}/channels/{channel}", s.setChannelCategory(false))
				r.Delete("/live/viewers/{viewer}", s.leaveChannel)
				r.Post("/live/viewers/{viewer}/ping", s.pingViewer)
				r.Post("/live/viewers/{viewer}/buffering", s.viewerBuffering)
				r.Get("/dvr", s.recordings)
				r.Post("/dvr/recordings", s.scheduleRecording)
				r.Delete("/dvr/recordings/{id}", s.deleteRecording)
				r.Patch("/dvr/rules/{id}", s.updateRule)
				r.Delete("/dvr/rules/{id}", s.deleteRule)
				r.Get("/live/playlist.m3u", s.livePlaylist)
				r.Get("/live/epg.xml", s.liveEPG)
			})

			r.Get("/playlists", s.playlists)
			r.Post("/playlists", s.createPlaylist)
			r.Get("/playlists/shared", s.sharedPlaylists)
			r.Get("/playlists/{id}", s.playlist)
			r.Put("/playlists/{id}", s.renamePlaylist)
			r.Delete("/playlists/{id}", s.deletePlaylist)
			r.Post("/playlists/{id}/items", s.addToPlaylist)
			r.Delete("/playlists/{id}/items/{item}", s.removeFromPlaylist)
			r.Put("/playlists/{id}/order", s.reorderPlaylist)
			r.Get("/playlists/{id}/next", s.playlistNext)
			r.Get("/playlists/{id}/share", s.playlistSharing)
			r.Put("/playlists/{id}/share", s.sharePlaylist)

			r.Get("/home", s.home)
			r.Get("/libraries", s.libraries)
			r.Get("/libraries/{id}/items", s.libraryItems)
			r.Get("/items/{id}", s.item)
			r.Get("/items/{id}/trailers/{n}", s.trailerFile)
			r.Get("/items/{id}/next", s.nextEpisode)
			r.Get("/items/{id}/preroll", s.preRoll)
			r.Get("/preroll/intro/{n}", s.introVideo)
			r.Post("/items/{id}/watched", s.setWatched(true))
			r.Post("/items/{id}/unwatched", s.setWatched(false))
			r.Get("/search", s.search)
			r.Get("/images/tmdb/{file}", s.tmdbImage)
			r.Get("/images/local/{file}", s.localImage)
			r.Get("/images/poster/{file}", s.overlayPoster)
			r.Get("/libraries/{id}/collections", s.libraryCollections)
			r.Get("/libraries/{id}/hubs", s.libraryHubs)
			r.Get("/collections/{id}", s.collection)

			r.Post("/progress", s.progress)
			r.Get("/files/{id}/direct", s.directPlay)
			r.Get("/files/{id}/subtitles/{stream}", s.subtitle)
			r.Post("/files/{id}/transcode", s.startTranscode)
			r.Get("/transcode/{sid}/{name}", s.transcodeFile)
			r.Delete("/transcode/{sid}", s.stopTranscode)

			r.Route("/admin", func(r chi.Router) {
				r.Use(requireAdmin)
				r.Get("/discover", s.discoverSources)
				r.Post("/discover", s.addDiscoverSource)
				r.Post("/discover/refresh", s.refreshDiscover)
				r.Put("/discover/order", s.orderDiscoverSources)
				r.Put("/discover/{id}", s.renameDiscoverSource)
				r.Delete("/discover/{id}", s.deleteDiscoverSource)
				r.Get("/system", s.system)
				r.Get("/stats", s.stats)
				r.Get("/activity", s.activity)
				r.Delete("/activity/{id}", s.killActivity)
				r.Get("/settings", s.getSettings)
				r.Get("/overlays", s.adminOverlays)
				r.Get("/overlays/preview", s.overlayPreview)
				r.Post("/overlays/image", s.uploadOverlayImage)
				r.Post("/overlays", s.saveOverlay)
				r.Put("/overlays/order", s.orderOverlays)
				r.Put("/overlays/{id}", s.saveOverlay)
				r.Delete("/overlays/{id}", s.deleteOverlay)
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
				r.Get("/items/{id}/collections", s.itemCollections)
				r.Put("/items/{id}/collections", s.setItemCollections)
				r.Get("/items/{id}/artwork", s.artworkOptions)
				r.Put("/items/{id}/artwork", s.setArtwork)
				r.Post("/items/{id}/artwork/upload", s.uploadArtwork)
				r.Get("/items/{id}/tpdb", s.tpdbSearch)
				r.Get("/tpdb/{poster}/thumb", s.tpdbThumb)
				r.Post("/items/{id}/match", s.matchItem)
				r.Post("/items/{id}/unmatch", s.unmatchItem)
				r.Put("/items/{id}/markers", s.setMarkers)
				r.Post("/items/{id}/detect-markers", s.detectMarkers)
				r.Get("/match", s.matchSearch)

				r.Get("/libraries/{id}/labels", s.libraryLabels)
				r.Get("/libraries/{id}/facets", s.libraryFacets)
				r.Post("/libraries/{id}/collections", s.createCollection)
				r.Put("/collections/{id}", s.updateCollection)
				r.Put("/collections/{id}/visibility", s.setCollectionVisibility)
				r.Put("/collections/{id}/artwork", s.setCollectionArtwork)
				r.Post("/collections/{id}/artwork", s.setCollectionArtwork)
				r.Post("/libraries/{id}/kometa", s.importKometa)
				r.Get("/libraries/{id}/rows", s.libraryRows)
				r.Put("/libraries/{id}/rows", s.setLibraryRows)
				r.Delete("/collections/{id}", s.deleteCollection)

				r.Get("/live/sources", s.liveSources)
				r.Post("/live/sources", s.saveLiveSource)
				r.Put("/live/sources/{id}", s.saveLiveSource)
				r.Delete("/live/sources/{id}", s.deleteLiveSource)
				r.Post("/live/sources/{id}/refresh", s.refreshLiveSource)
				r.Get("/live/sources/{id}/groups", s.liveSourceGroups)
				r.Put("/live/sources/{id}/groups", s.saveLiveSourceGroups)
				r.Get("/live/streams", s.liveStreams)
				r.Get("/live/groups", s.liveGroups)
				r.Get("/live/channels", s.adminChannels)
				r.Post("/live/channels", s.createChannels)
				r.Put("/live/channels/{id}", s.updateChannel)
				r.Delete("/live/channels/{id}", s.deleteChannel)
				r.Get("/live/epg-channels", s.epgChannels)
				r.Get("/live/kids", s.kidChannels)
				r.Put("/live/kids/channels", s.setKidChannels)
				r.Put("/live/kids/group", s.setKidGroup)
				r.Post("/live/dispatcharr/preview", s.dispatcharrPreview)
				r.Post("/live/dispatcharr/import", s.dispatcharrImport)
				r.Get("/live/dispatcharr/import", s.dispatcharrStatus)
				r.Get("/live/logos", s.adminLogos)
				r.Post("/live/logos", s.uploadLogos)
				r.Post("/live/logos/match", s.matchAllLogos)
				r.Put("/live/logos/{id}", s.renameLogo)
				r.Delete("/live/logos/{id}", s.deleteLogo)
				r.Post("/live/logos/{id}/assign", s.assignLogo)
				r.Get("/live/content-sources", s.contentSources)
				r.Post("/live/custom", s.saveCustomChannel)
				r.Get("/live/custom/{id}", s.customChannel)
				r.Put("/live/custom/{id}", s.saveCustomChannel)
				r.Get("/live/custom/{id}/schedule", s.customSchedule)
				r.Get("/live/activity", s.liveActivity)
				r.Post("/live/test-encoder", s.testEncoder)
				r.Delete("/live/activity/{id}", s.stopRelay)
				r.Delete("/live/viewers/{viewer}", s.kickViewer)

				r.Post("/dvr/recordings/{id}/approve", s.approveRecording)
				r.Post("/dvr/rules/{id}/approve", s.approveRule)

				r.Get("/history", s.adminHistory)
				r.Get("/statistics", s.adminStatistics)
				r.Get("/tasks", s.listTasks)
				r.Put("/tasks/{id}", s.configureTask)
				r.Post("/tasks/{id}/run", s.runTask)
				r.Get("/backups", s.listBackups)
				r.Post("/backups/{name}/restore", s.restoreBackup)
				r.Get("/backups/{name}/download", s.downloadBackup)
				r.Post("/backups/upload", s.uploadRestore)
				r.Post("/backups/remote/test", s.testRemoteBackups)
				r.Post("/backups/remote/{name}/restore", s.restoreRemoteBackup)
				r.Get("/logs", s.logLines)
				r.Get("/logs/download", s.downloadLog)

				r.Put("/users/{id}/libraries", s.setUserLibraries)
				r.Delete("/users/{id}/2fa", s.adminReset2FA)
				r.Delete("/users/{id}/passkeys", s.adminDeletePasskeys)
				r.Delete("/users/{id}/passcode", s.clearUserPasscode)
				r.Put("/users/{id}/live-tv", s.setUserLiveTV)
				r.Put("/users/{id}/kid", s.setUserKid)
				r.Get("/webhooks", s.webhooks)
				r.Post("/webhooks", s.saveWebhook)
				r.Put("/webhooks/{id}", s.saveWebhook)
				r.Delete("/webhooks/{id}", s.deleteWebhook)
				r.Post("/webhooks/{id}/test", s.testWebhook)
				r.Get("/plugins", s.adminPlugins)
				r.Post("/plugins", s.addPlugin)
				r.Get("/plugins/catalog", s.pluginCatalog)
				r.Post("/plugins/catalog/install", s.installPlugin)
				r.Post("/plugins/{id}/uninstall", s.uninstallPlugin)
				r.Put("/plugins/{id}", s.updatePlugin)
				r.Put("/plugins/{id}/settings", s.savePluginSettings)
				r.Post("/plugins/{id}/restart", s.restartPlugin)
				r.Delete("/plugins/{id}", s.deletePlugin)
				r.Post("/import/connect", s.importConnect)
				r.Post("/import/start", s.importStart)
				r.Get("/import/status", s.importStatus)
				r.Post("/import/cancel", s.importCancel)
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

// sweepPlayers drops players that stopped pinging, so player.stop is sent for them too.
func (s *Server) sweepPlayers(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Activity.List()
		}
	}
}
