package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/importer"
	"bingebandit/internal/livetv"
	"bingebandit/internal/plugins"
	"bingebandit/internal/store"
)

// ---- admin: Admin → Plugins ----

func (s *Server) adminPlugins(w http.ResponseWriter, r *http.Request) {
	list, err := s.Plugins.List()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": list, "dirs": s.Plugins.Dirs})
}

func (s *Server) addPlugin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL       string `json:"url"`
		ServerURL string `json:"serverUrl"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		writeErr(w, http.StatusBadRequest, "the plugin's address must start with http:// or https://")
		return
	}
	if req.ServerURL == "" {
		req.ServerURL = baseURL(r)
	}
	p, err := s.Plugins.AddRemote(req.URL, req.ServerURL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	st, _ := s.Plugins.Get(p.ID)
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) updatePlugin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSON(r, &req); err != nil || req.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "send {\"enabled\": true|false}")
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.Store.SetPluginEnabled(id, *req.Enabled); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.Plugins.Sync()
	s.Plugins.Settle(id, 5*time.Second)
	st, err := s.Plugins.Get(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) savePluginSettings(w http.ResponseWriter, r *http.Request) {
	var values map[string]string
	if err := readJSON(r, &values); err != nil {
		writeErr(w, http.StatusBadRequest, "send an object of setting values (strings)")
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.Plugins.SaveSettings(id, values); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeStoreErr(w, err)
		} else {
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	st, _ := s.Plugins.Get(id)
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) restartPlugin(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.Store.Plugin(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.Plugins.Restart(id)
	s.Plugins.Settle(id, 5*time.Second)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deletePlugin(w http.ResponseWriter, r *http.Request) {
	if err := s.Plugins.Remove(chi.URLParam(r, "id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeStoreErr(w, err)
		} else {
			writeErr(w, http.StatusConflict, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- users: plugin pages ----

// pluginPages lists the pages of running plugins this user may open.
func (s *Server) pluginPages(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Plugins.Pages(currentUser(r).IsAdmin))
}

// pluginProxy serves /api/plugins/{id}/* from the plugin.
func (s *Server) pluginProxy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prefix := "/api/plugins/" + id
	s.Plugins.Proxy(w, r, id, chi.URLParam(r, "*"), plugins.Caller{
		User: currentUser(r), Prefix: prefix, ExternalURL: s.externalURL(r),
	})
}

// ---- the plugin API (/api/plugin/v1), called by plugins with their key ----

const pluginKey ctxKey = 2

func currentPlugin(r *http.Request) *store.Plugin {
	p, _ := r.Context().Value(pluginKey).(*store.Plugin)
	return p
}

// requirePlugin accepts a running plugin's key. Handlers shared with the
// user API then see the server itself (uid 0, which sees every library)
// unless the route acts as a user (/users/{uid}/...).
func (s *Server) requirePlugin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		p, err := s.Store.PluginByKey(key)
		if err != nil || !p.Enabled {
			writeErr(w, http.StatusUnauthorized, "unknown or disabled plugin key")
			return
		}
		server := &store.User{ID: 0, Username: "plugin:" + p.ID, IsAdmin: true, AllLibraries: true}
		ctx := context.WithValue(r.Context(), pluginKey, p)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, userKey, server)))
	})
}

// asUser runs the rest of the route as the user in {uid}, so watch state,
// watchlists and library access are that user's.
func (s *Server) asUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, ok := idParam(r, "uid")
		if !ok {
			writeErr(w, http.StatusBadRequest, "bad user id")
			return
		}
		u, err := s.Store.UserByID(uid)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func (s *Server) pluginAPI(r chi.Router) {
	r.Use(s.requirePlugin)
	r.Get("/info", s.pluginInfo)
	r.Get("/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.Plugins.Settings(currentPlugin(r)))
	})
	r.Post("/log", s.pluginLog)
	r.Get("/users", s.pluginUsers)
	r.Get("/libraries", s.libraries)
	r.Get("/libraries/{id}/items", s.libraryItems)
	r.Get("/items/{id}", s.item)
	r.Get("/lookup", s.pluginLookup)
	r.Get("/search", s.search)
	r.Get("/files", s.pluginFiles)
	r.Get("/files/{id}/direct", s.directPlay)
	r.Get("/collections/{id}/missing", s.pluginCollectionMissing)
	r.Put("/live/channels", s.pluginLiveChannels)
	r.Get("/images/tmdb/{file}", s.tmdbImage)
	r.Get("/images/local/{file}", s.localImage)
	r.Route("/users/{uid}", func(r chi.Router) {
		r.Use(s.asUser)
		r.Get("/items/{id}", s.item)
		r.Get("/lookup", s.pluginLookup)
		r.Get("/watchlist", s.watchlist)
		r.Post("/watchlist/{id}", s.setWatchlist(true))
		r.Delete("/watchlist/{id}", s.setWatchlist(false))
		r.Get("/history", s.myHistory)
	})
}

func (s *Server) pluginInfo(w http.ResponseWriter, r *http.Request) {
	p := currentPlugin(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": p.ID, "server": s.Store.Setting(store.SettingServerName), "version": s.Version,
		"settings": s.Plugins.Settings(p),
	})
}

// pluginCollectionMissing lists the titles a collection's TMDB collection or
// lists have that no library does, for plugins that go and get them.
func (s *Server) pluginCollectionMissing(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	c, err := s.Store.Collection(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	missing, unknown, err := importer.NewLists(s.Store).Missing(r.Context(), c)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if missing == nil {
		missing = []importer.Wanted{}
	}
	// The library's name lets Cinemask put what it adds in the matching folder (Kids Movies).
	library := ""
	if l, err := s.Store.Library(c.LibraryID); err == nil {
		library = l.Name
	}
	writeJSON(w, http.StatusOK, map[string]any{"missing": missing, "unknown": unknown, "library": library})
}

func (s *Server) pluginUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Store.ListUsers()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	type pu struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		IsAdmin  bool   `json:"isAdmin"`
	}
	out := make([]pu, 0, len(users))
	for _, u := range users {
		out = append(out, pu{u.ID, u.Username, u.IsAdmin})
	}
	writeJSON(w, http.StatusOK, out)
}

// pluginLookup finds library movies and shows by ?tmdb=, or ?title= and ?year=.
func (s *Server) pluginLookup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tmdb, _ := strconv.ParseInt(q.Get("tmdb"), 10, 64)
	year, _ := strconv.Atoi(q.Get("year"))
	items, err := s.Store.FindItems(currentUser(r).ID, q.Get("kind"), tmdb, q.Get("title"), year)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// pluginFiles pages through every movie and episode file with its quality.
func (s *Server) pluginFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	files, total, err := s.Store.FileSummaries(max(offset, 0), limit)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "total": total})
}

// pluginLiveChannels replaces the Live TV channels a plugin offers (such as
// YouTubers' live streams), each shown while it's live.
func (s *Server) pluginLiveChannels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Group    string                 `json:"group"`
		Channels []livetv.PluginChannel `json:"channels"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Group = strings.TrimSpace(req.Group)
	if req.Group == "" || len(req.Group) > 60 {
		writeErr(w, http.StatusBadRequest, "group must be 1 to 60 characters")
		return
	}
	if err := livetv.ValidPluginChannels(req.Channels); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := livetv.SyncPluginChannels(s.Store, currentPlugin(r).ID, req.Group, req.Channels); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pluginLog writes a line to the server log, for plugins that run elsewhere.
func (s *Server) pluginLog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	msg := strings.TrimSpace(req.Message)
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	level := ""
	if req.Level == "error" || req.Level == "warn" {
		level = req.Level + ": "
	}
	log.Printf("plugin %s: %s%s", currentPlugin(r).ID, level, msg)
	w.WriteHeader(http.StatusNoContent)
}
