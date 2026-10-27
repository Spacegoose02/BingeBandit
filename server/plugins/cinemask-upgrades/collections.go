package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"bingebandit/pkg/bbplugin"
)

// Collections can have Cinemask monitor (and search for) the titles they're
// missing: the rest of a TMDB collection, or a followed list's titles that
// no library has. It's off until the admin turns on the "collections"
// setting, and then each collection's two switches start off too.

// wanted is a title a collection is missing, as BingeBandit lists it.
type wanted struct {
	Kind   string `json:"kind"`
	TMDBID int64  `json:"tmdbId"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
}

// colConfig is one collection's switches and what came of the last run.
type colConfig struct {
	Monitor bool `json:"monitor"`
	Search  bool `json:"search"`
	// Added is the titles already handed to Cinemask ("movie:603"), so a
	// title someone took out of Cinemask isn't put back every run.
	Added   []string `json:"added,omitempty"`
	LastRun int64    `json:"lastRun,omitempty"`
	Error   string   `json:"error,omitempty"`
	Missing int      `json:"missing"`
}

type colStore struct {
	dir func() string
	mu  sync.Mutex
}

func (s *colStore) path() string { return filepath.Join(s.dir(), "collections.json") }

func (s *colStore) load() map[string]*colConfig {
	out := map[string]*colConfig{}
	if b, err := os.ReadFile(s.path()); err == nil {
		json.Unmarshal(b, &out)
	}
	return out
}

// update changes one collection's entry (nil removes it) under the lock.
func (s *colStore) update(id int64, f func(c *colConfig) *colConfig) (colConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	k := strconv.FormatInt(id, 10)
	c := all[k]
	if c == nil {
		c = &colConfig{}
	}
	c = f(c)
	if c == nil || (!c.Monitor && !c.Search && len(c.Added) == 0) {
		delete(all, k)
	} else {
		all[k] = c
	}
	b, _ := json.MarshalIndent(all, "", "  ")
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return colConfig{}, err
	}
	if err := os.WriteFile(s.path(), b, 0o600); err != nil {
		return colConfig{}, err
	}
	if c == nil {
		return colConfig{}, nil
	}
	return *c, nil
}

func (s *colStore) get(id int64) colConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.load()[strconv.FormatInt(id, 10)]; c != nil {
		return *c
	}
	return colConfig{}
}

func (a *app) collectionsOn(ctx context.Context) bool { return a.conf(ctx)["collections"] == "true" }

func (a *app) collectionMissing(ctx context.Context, id int64) ([]wanted, error) {
	var out struct {
		Missing []wanted `json:"missing"`
	}
	err := a.p.Call(ctx, "GET", "/collections/"+strconv.FormatInt(id, 10)+"/missing", nil, &out)
	return out.Missing, err
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// collection is what the collection editor shows: whether the feature is on
// at all, and this collection's switches.
func (a *app) collection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad collection id"})
		return
	}
	c := a.cols.get(id)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": a.collectionsOn(r.Context()),
		"monitor": c.Monitor, "search": c.Search, "lastRun": c.LastRun, "error": c.Error,
		"missing": c.Missing, "added": len(c.Added)})
}

func (a *app) setCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad collection id"})
		return
	}
	if !a.collectionsOn(r.Context()) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "turn on collections in the Cinemask Upgrades plugin's settings first"})
		return
	}
	var req struct {
		Monitor bool `json:"monitor"`
		Search  bool `json:"search"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		return
	}
	// Searching means monitoring: Cinemask only searches for what it monitors.
	req.Monitor = req.Monitor || req.Search
	c, err := a.cols.update(id, func(c *colConfig) *colConfig {
		c.Monitor, c.Search = req.Monitor, req.Search
		return c
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if c.Monitor && a.kick != nil {
		a.kick(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "monitor": c.Monitor, "search": c.Search})
}

// watchCollections hands each switched-on collection's missing titles to
// Cinemask every six hours; list syncs and new TMDB parts show up that way.
func (a *app) watchCollections(ctx context.Context) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		a.runCollections(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *app) runCollections(ctx context.Context) {
	if !a.collectionsOn(ctx) {
		return
	}
	a.cols.mu.Lock()
	all := a.cols.load()
	a.cols.mu.Unlock()
	for k, c := range all {
		if ctx.Err() != nil {
			return
		}
		if id, _ := strconv.ParseInt(k, 10, 64); id > 0 && c.Monitor {
			a.runCollection(ctx, id)
		}
	}
}

// runCollection adds one collection's missing titles to Cinemask, searching
// for them at once when the collection's Search switch is on.
func (a *app) runCollection(ctx context.Context, id int64) {
	added, err := a.addMissing(ctx, id)
	var gone bool
	a.cols.update(id, func(c *colConfig) *colConfig {
		var apiErr *bbplugin.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			gone = true
			return nil // the collection was deleted
		}
		c.LastRun = time.Now().Unix()
		c.Error = ""
		if err != nil {
			c.Error = err.Error()
		}
		c.Missing = added.missing
		c.Added = appendNew(c.Added, added.keys...)
		return c
	})
	if err != nil && !gone {
		log.Printf("collection %d: %v", id, err)
	}
}

type addResult struct {
	missing int
	keys    []string
}

func (a *app) addMissing(ctx context.Context, id int64) (addResult, error) {
	var res addResult
	if !a.collectionsOn(ctx) {
		return res, nil
	}
	conf := a.cols.get(id)
	if !conf.Monitor {
		return res, nil
	}
	cm, err := a.cinemask(ctx)
	if err != nil {
		return res, err
	}
	missing, err := a.missing(ctx, id)
	if err != nil {
		return res, err
	}
	res.missing = len(missing)
	done := map[string]bool{}
	for _, k := range conf.Added {
		done[k] = true
	}
	var failed []string
	for _, m := range missing {
		k := m.Kind + ":" + strconv.FormatInt(m.TMDBID, 10)
		if done[k] {
			continue
		}
		have, err := cm.Lookup(ctx, m.Kind, m.TMDBID)
		if err == nil && have == nil {
			_, err = cm.Monitor(ctx, m.Kind, m.TMDBID, 0, conf.Search)
		} else if err == nil && conf.Search {
			// Already monitored but still not in a library: look now.
			_, err = cm.Search(ctx, have.ID, Scope{})
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", m.Title, err))
			continue
		}
		res.keys = append(res.keys, k)
	}
	if len(failed) > 0 {
		return res, fmt.Errorf("%d of %d couldn't be added (%s)", len(failed), len(missing), failed[0])
	}
	return res, nil
}

func appendNew(list []string, add ...string) []string {
	have := map[string]bool{}
	for _, k := range list {
		have[k] = true
	}
	for _, k := range add {
		if !have[k] {
			list, have[k] = append(list, k), true
		}
	}
	return list
}
