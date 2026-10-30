package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"bingebandit/pkg/bbplugin"
)

//go:embed page.html
var page []byte

// routes are the Upgrades page and its calls, for admins only. Each call is
// a fixed Cinemask operation; nothing passes an arbitrary path through.
func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	admin := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if u, ok := bbplugin.UserOf(r); !ok || !u.Admin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "admins only"})
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /{$}", admin(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(page)
	}))
	mux.HandleFunc("GET /report", admin(a.report))
	mux.HandleFunc("GET /title", admin(a.title))
	mux.HandleFunc("POST /monitor", admin(a.monitor))
	mux.HandleFunc("POST /search", admin(a.search))
	mux.HandleFunc("POST /releases", admin(a.releases))
	mux.HandleFunc("POST /grab", admin(a.grab))
	mux.HandleFunc("POST /search-all", admin(a.searchAll))
	mux.HandleFunc("GET /collections/{id}", admin(a.collection))
	mux.HandleFunc("PUT /collections/{id}", admin(a.setCollection))
	mux.HandleFunc("POST /search-all/stop", admin(func(w http.ResponseWriter, r *http.Request) {
		a.stopBulk()
		w.WriteHeader(http.StatusNoContent)
	}))
	return mux
}

// report is the upgrade list, with whether Cinemask is reachable.
func (a *app) report(w http.ResponseWriter, r *http.Request) {
	rep, at, err := a.scan(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "couldn't read the library: " + err.Error()})
		return
	}
	cm := map[string]any{"ok": false}
	if c, err := a.cinemask(r.Context()); err != nil {
		cm["error"] = err.Error()
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		user, role, err := c.Me(ctx)
		cancel()
		switch {
		case err != nil:
			cm["error"] = err.Error()
		case role != "manager" && role != "admin" && role != "owner":
			cm["error"] = "the API key belongs to " + user + ", who isn't a Cinemask manager, so it can't search"
		default:
			cm["ok"], cm["user"] = true, user
		}
	}
	a.mu.Lock()
	bulk := a.bulk
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"report": rep, "scannedAt": at, "cinemask": cm, "bulk": bulk})
}

// titleReq names a movie or show by TMDB id, and for shows optionally a
// season or episode.
type titleReq struct {
	Kind      string `json:"kind"`
	TMDBID    int64  `json:"tmdbId"`
	Season    *int   `json:"season"`
	Episode   *int   `json:"episode"`
	ProfileID int64  `json:"profileId"`
	GUID      string `json:"guid"`
}

func (t titleReq) valid() error {
	if t.Kind != "movie" && t.Kind != "show" {
		return errors.New("kind must be movie or show")
	}
	if t.TMDBID <= 0 {
		return errors.New("this title has no TMDB match, so Cinemask can't find it; fix the match in BingeBandit first")
	}
	if t.Kind == "movie" && (t.Season != nil || t.Episode != nil) {
		return errors.New("movies have no seasons")
	}
	if t.Episode != nil && t.Season == nil {
		return errors.New("an episode needs its season")
	}
	return nil
}

func (t titleReq) scope() Scope { return Scope{Season: t.Season, Episode: t.Episode} }

func queryTitle(r *http.Request) titleReq {
	q := r.URL.Query()
	t := titleReq{Kind: q.Get("kind")}
	t.TMDBID, _ = strconv.ParseInt(q.Get("tmdb"), 10, 64)
	return t
}

func readTitle(w http.ResponseWriter, r *http.Request) (titleReq, bool) {
	var t titleReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&t); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return t, false
	}
	if err := t.valid(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return t, false
	}
	return t, true
}

// monitored finds the title in Cinemask, answering the request itself when
// it can't.
func (a *app) monitored(w http.ResponseWriter, r *http.Request, t titleReq) (*Cinemask, *Monitored, bool) {
	c, err := a.cinemask(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return nil, nil, false
	}
	m, err := c.Lookup(r.Context(), t.Kind, t.TMDBID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return nil, nil, false
	}
	if m == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Cinemask doesn't monitor this title yet; add it first"})
		return nil, nil, false
	}
	return c, m, true
}

// title is how Cinemask sees a title: monitored or not, its profile and the
// profiles it could move to.
func (a *app) title(w http.ResponseWriter, r *http.Request) {
	t := queryTitle(r)
	if err := t.valid(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	c, err := a.cinemask(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	m, err := c.Lookup(r.Context(), t.Kind, t.TMDBID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	profiles, err := c.Profiles(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"monitored": m, "profiles": profiles, "link": c.Link(t.Kind, t.TMDBID)})
}

// monitor adds a title to Cinemask, so it can be searched.
func (a *app) monitor(w http.ResponseWriter, r *http.Request) {
	t, ok := readTitle(w, r)
	if !ok {
		return
	}
	c, err := a.cinemask(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	m, err := c.Monitor(r.Context(), t.Kind, t.TMDBID, t.ProfileID, "", false)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// withProfile moves the title to another profile first when one is given:
// a higher cutoff is what makes a file Cinemask thinks is good enough worth
// upgrading, and releases are judged against the profile.
func withProfile(w http.ResponseWriter, r *http.Request, c *Cinemask, m *Monitored, t titleReq) bool {
	if t.ProfileID > 0 && t.ProfileID != m.ProfileID {
		if err := c.SetProfile(r.Context(), m.ID, t.ProfileID); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return false
		}
	}
	return true
}

// search has Cinemask look for a better copy and grab it.
func (a *app) search(w http.ResponseWriter, r *http.Request) {
	t, ok := readTitle(w, r)
	if !ok {
		return
	}
	c, m, ok := a.monitored(w, r, t)
	if !ok || !withProfile(w, r, c, m, t) {
		return
	}
	res, err := c.Search(r.Context(), m.ID, t.scope())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// releases is a manual search: everything Cinemask found, and why it would
// or wouldn't take each one.
func (a *app) releases(w http.ResponseWriter, r *http.Request) {
	t, ok := readTitle(w, r)
	if !ok {
		return
	}
	c, m, ok := a.monitored(w, r, t)
	if !ok || !withProfile(w, r, c, m, t) {
		return
	}
	rel, errs, err := c.Releases(r.Context(), m.ID, t.scope())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if rel == nil {
		rel = []Release{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": rel, "errors": errs})
}

// grab downloads one release from the last manual search.
func (a *app) grab(w http.ResponseWriter, r *http.Request) {
	t, ok := readTitle(w, r)
	if !ok {
		return
	}
	if t.GUID == "" || len(t.GUID) > 2000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pick a release"})
		return
	}
	c, m, ok := a.monitored(w, r, t)
	if !ok {
		return
	}
	if err := c.Grab(r.Context(), m.ID, t.GUID); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// searchAll searches every listed title Cinemask monitors, one at a time.
func (a *app) searchAll(w http.ResponseWriter, r *http.Request) {
	c, err := a.cinemask(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rep, _, err := a.scan(r.Context(), false)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if err := a.startBulk(rep, c); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
