package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"bingebandit/pkg/bbplugin"
)

//go:embed page.html
var page []byte

// routes are reached through /api/plugins/watchlist-rss/, always for a signed-in user.
func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	user := func(h func(w http.ResponseWriter, r *http.Request, u bbplugin.User)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			u, ok := bbplugin.UserOf(r)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no user"})
				return
			}
			h(w, r, u)
		}
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(page)
	})
	mux.HandleFunc("GET /feed.xml", user(a.feed))
	mux.HandleFunc("GET /subscriptions", user(func(w http.ResponseWriter, r *http.Request, u bbplugin.User) {
		writeJSON(w, http.StatusOK, a.subs.forUser(u.ID))
	}))
	mux.HandleFunc("POST /subscriptions", user(a.subscribe))
	mux.HandleFunc("DELETE /subscriptions/{id}", user(func(w http.ResponseWriter, r *http.Request, u bbplugin.User) {
		if !a.subs.remove(u.ID, r.PathValue("id")) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /subscriptions/{id}/check", user(func(w http.ResponseWriter, r *http.Request, u bbplugin.User) {
		s := a.subs.get(u.ID, r.PathValue("id"))
		if s == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		a.check(r.Context(), s)
		writeJSON(w, http.StatusOK, public(a.subs.get(u.ID, s.ID)))
	}))
	return mux
}

// feed is the person's watchlist as RSS. Feed readers can't sign in, so the
// page tells people to add ?token=<API key>; BingeBandit checks it and
// strips it before the request gets here.
func (a *app) feed(w http.ResponseWriter, r *http.Request, u bbplugin.User) {
	var items []libItem
	if err := a.p.Call(r.Context(), http.MethodGet, "/users/"+strconv.FormatInt(u.ID, 10)+"/watchlist", nil, &items); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	b, err := buildFeed(u.Username, bbplugin.ExternalURL(r), items)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write(b)
}

func (a *app) subscribe(w http.ResponseWriter, r *http.Request, u bbplugin.User) {
	var req struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	parsed, err := url.Parse(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enter the feed's http(s) address"})
		return
	}
	// Read it once first so a typo is caught now, not on the next poll.
	body, err := a.fetch(r.Context(), req.URL, u.Admin)
	if err == nil {
		_, err = parseFeed(body)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "couldn't read that feed: " + err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = parsed.Host + parsed.Path
	}
	s, err := a.subs.add(u.ID, req.URL, name, u.Admin)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	a.check(r.Context(), s)
	writeJSON(w, http.StatusOK, public(a.subs.get(u.ID, s.ID)))
}

// public drops the seen list, which only the plugin needs.
func public(s *Sub) *Sub {
	if s != nil {
		s.Seen = nil
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
