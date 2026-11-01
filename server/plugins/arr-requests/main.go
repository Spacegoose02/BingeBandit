// Command arr-requests is the Sonarr & Radarr plugin: approved title
// requests go to Radarr (movies) and Sonarr (shows, just the seasons asked
// for), and 4K requests to a second pair when one is set up. While they
// download, the queue's progress and ETA show on the Requests page. A
// problem report an admin sends off has the old copy marked failed, so the
// *arr blocklists it and finds another.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"sync"
	"time"

	"bingebandit/pkg/bbplugin"
)

var manifest = bbplugin.Manifest{
	ID:          "arr-requests",
	Name:        "Sonarr & Radarr",
	Version:     "1.0.0",
	Description: "Sends approved title requests to Sonarr and Radarr (and 4K copies to a second pair), shows their download progress, and replaces files people report problems with.",
	Author:      "BingeBandit",
	Events:      []string{"request.approved", "problem.refetch"},
	Settings: []bbplugin.Setting{
		{Key: "radarr_url", Label: "Radarr address", Type: "text", Help: "For example http://192.168.1.10:7878"},
		{Key: "radarr_key", Label: "Radarr API key", Type: "password", Help: "Radarr → Settings → General → API Key."},
		{Key: "radarr_profile", Label: "Radarr quality profile", Type: "text", Help: "Its name. Empty uses the first one."},
		{Key: "radarr_root", Label: "Radarr root folder", Type: "text", Help: "Its path, e.g. /movies. Empty uses the first one."},
		{Key: "sonarr_url", Label: "Sonarr address", Type: "text", Help: "For example http://192.168.1.10:8989"},
		{Key: "sonarr_key", Label: "Sonarr API key", Type: "password", Help: "Sonarr → Settings → General → API Key."},
		{Key: "sonarr_profile", Label: "Sonarr quality profile", Type: "text", Help: "Its name. Empty uses the first one."},
		{Key: "sonarr_root", Label: "Sonarr root folder", Type: "text", Help: "Its path, e.g. /tv. Empty uses the first one."},
		{Key: "radarr4k_url", Label: "4K Radarr address", Type: "text", Help: "Optional: a second Radarr for 4K copies. 4K requests are turned on in Settings → Library."},
		{Key: "radarr4k_key", Label: "4K Radarr API key", Type: "password"},
		{Key: "radarr4k_profile", Label: "4K Radarr quality profile", Type: "text"},
		{Key: "radarr4k_root", Label: "4K Radarr root folder", Type: "text"},
		{Key: "sonarr4k_url", Label: "4K Sonarr address", Type: "text", Help: "Optional: a second Sonarr for 4K copies."},
		{Key: "sonarr4k_key", Label: "4K Sonarr API key", Type: "password"},
		{Key: "sonarr4k_profile", Label: "4K Sonarr quality profile", Type: "text"},
		{Key: "sonarr4k_root", Label: "4K Sonarr root folder", Type: "text"},
	},
}

// request is a title request, as the plugin API lists it.
type request struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	TMDBID      int64  `json:"tmdbId"`
	Title       string `json:"title"`
	Year        int    `json:"year"`
	Seasons     []int  `json:"seasons"`
	FourK       bool   `json:"fourK"`
	ReleaseDate string `json:"releaseDate"`
}

// problem is a problem report an admin sent off to be fixed.
type problem struct {
	ID       int64  `json:"id"`
	ItemKind string `json:"itemKind"`
	TMDBID   int64  `json:"tmdbId"`
	Title    string `json:"title"`
	Season   *int   `json:"seasonNumber"`
	Episode  *int   `json:"episodeNumber"`
}

type app struct {
	p *bbplugin.Plugin
	// call reaches BingeBandit's plugin API (tests replace it).
	call func(ctx context.Context, method, path string, body, out any) error

	mu       sync.Mutex
	settings map[string]string
}

func main() {
	a := &app{}
	a.p = &bbplugin.Plugin{Manifest: manifest, OnEvent: a.onEvent}
	a.call = a.p.Call
	a.p.Start = func(ctx context.Context) { a.watch(ctx) }
	log.SetFlags(0)
	if err := a.p.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *app) onEvent(_ context.Context, e bbplugin.Event) {
	switch e.Event {
	case "request.approved":
		go a.sendRequests(context.Background())
	case "problem.refetch":
		go a.refetch(context.Background(), e.Data)
	case "plugin.settings":
		var s map[string]string
		if json.Unmarshal(e.Data, &s) == nil {
			a.mu.Lock()
			a.settings = s
			a.mu.Unlock()
			go a.sendRequests(context.Background())
		}
	}
}

func (a *app) conf(ctx context.Context) map[string]string {
	a.mu.Lock()
	s := a.settings
	a.mu.Unlock()
	if s != nil {
		return s
	}
	s, err := a.p.Settings(ctx)
	if err != nil {
		return map[string]string{}
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	return s
}

// instance is the *arr for a kind of title, the 4K one when fourK.
func (a *app) instance(ctx context.Context, kind string, fourK bool) (*arr, error) {
	name := "radarr"
	if kind == "show" {
		name = "sonarr"
	}
	prefix := name
	if fourK {
		prefix += "4k"
	}
	s := a.conf(ctx)
	c, err := newArr(name, s[prefix+"_url"], s[prefix+"_key"], s[prefix+"_profile"], s[prefix+"_root"])
	if errors.Is(err, errNotSetUp) {
		label := map[string]string{"radarr": "Radarr", "sonarr": "Sonarr", "radarr4k": "a 4K Radarr", "sonarr4k": "a 4K Sonarr"}[prefix]
		return nil, errors.New("set up " + label + " in the Sonarr & Radarr plugin's settings first")
	}
	return c, err
}

func (a *app) watch(ctx context.Context) {
	send := time.NewTicker(15 * time.Minute)
	defer send.Stop()
	track := time.NewTicker(2 * time.Minute)
	defer track.Stop()
	a.sendRequests(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-send.C:
			a.sendRequests(ctx)
		case <-track.C:
			a.trackRequests(ctx)
		}
	}
}

func (a *app) requests(ctx context.Context, status string) ([]request, error) {
	var out []request
	return out, a.call(ctx, "GET", "/requests?status="+status, nil, &out)
}

var sendMu sync.Mutex

// sendRequests hands each approved request to Radarr or Sonarr (and the 4K
// one too when the 4K copy is wanted), the most wanted first.
func (a *app) sendRequests(ctx context.Context) {
	sendMu.Lock()
	defer sendMu.Unlock()
	list, err := a.requests(ctx, "approved")
	if err != nil {
		log.Printf("requests: %v", err)
		return
	}
	for _, r := range list {
		if ctx.Err() != nil {
			return
		}
		link, err := a.send(ctx, r, false)
		if err == nil && r.FourK {
			_, err = a.send(ctx, r, true)
		}
		status, reason := "sent", ""
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			status, reason = "failed", err.Error()
			log.Printf("request %q: %v", r.Title, err)
		}
		if err := a.call(ctx, "PUT", "/requests/"+strconv.FormatInt(r.ID, 10),
			map[string]string{"status": status, "reason": reason, "link": link}, nil); err != nil {
			log.Printf("request %q: telling BingeBandit: %v", r.Title, err)
		}
	}
}

func (a *app) send(ctx context.Context, r request, fourK bool) (string, error) {
	if r.Kind != "movie" && r.Kind != "show" {
		return "", errors.New("only movies and shows can be fetched")
	}
	c, err := a.instance(ctx, r.Kind, fourK)
	if err != nil {
		return "", err
	}
	have, err := c.find(ctx, r.TMDBID)
	if err != nil {
		return "", err
	}
	if have != nil {
		return c.link(have, r.TMDBID), c.fetchMore(ctx, have, r.Seasons)
	}
	t, err := c.lookup(ctx, r.TMDBID, r.Title, r.Year)
	if err != nil {
		return "", err
	}
	return c.link(t, r.TMDBID), c.add(ctx, t, r.Seasons)
}

// trackRequests passes on the queue's progress for each sent request.
func (a *app) trackRequests(ctx context.Context) {
	list, err := a.requests(ctx, "sent")
	if err != nil || len(list) == 0 {
		return
	}
	queues := map[string][]queueItem{}
	for _, r := range list {
		if ctx.Err() != nil {
			return
		}
		c, err := a.instance(ctx, r.Kind, false)
		if err != nil {
			continue
		}
		q, ok := queues[c.kind]
		if !ok {
			if q, err = c.queue(ctx); err != nil {
				log.Printf("requests: %s queue: %v", c.name(), err)
				continue
			}
			queues[c.kind] = q
		}
		t, err := c.find(ctx, r.TMDBID)
		if err != nil || t == nil {
			continue
		}
		p := progressOf(r, c.kind, t.ID, q, time.Now())
		if err := a.call(ctx, "PUT", "/requests/"+strconv.FormatInt(r.ID, 10),
			map[string]any{"status": "sent", "stage": p.Stage, "progress": p.Progress, "eta": p.ETA}, nil); err != nil {
			log.Printf("request %q: progress: %v", r.Title, err)
		}
	}
}

type progress struct {
	Stage    string
	Progress float64
	ETA      int64
}

// progressOf sums up a title's queue entries: bytes done over bytes in
// all, the latest ETA, and importing once everything is down.
func progressOf(r request, kind string, id int64, queue []queueItem, now time.Time) progress {
	var p progress
	var size, left float64
	n, importing := 0, 0
	for _, q := range queue {
		if (kind == "radarr" && q.MovieID != id) || (kind == "sonarr" && q.SeriesID != id) {
			continue
		}
		n++
		size += q.Size
		left += q.SizeLeft
		if t, err := time.Parse(time.RFC3339, q.EstimatedCompletionTime); err == nil && t.After(now) {
			p.ETA = max(p.ETA, t.Unix())
		}
		if q.TrackedDownloadState == "importPending" || q.TrackedDownloadState == "importing" || q.Status == "completed" {
			importing++
		}
	}
	switch {
	case n == 0:
		p.Stage = "searching"
		if r.ReleaseDate != "" && r.ReleaseDate > now.Format("2006-01-02") {
			p.Stage = "waiting"
		}
	case importing == n:
		p.Stage, p.Progress = "importing", 1
	default:
		p.Stage = "downloading"
		if size > 0 {
			p.Progress = (size - left) / size
		}
	}
	return p
}

// refetch handles problem.refetch: the *arr marks the old copy failed and
// finds another, and BingeBandit hears how that went.
func (a *app) refetch(ctx context.Context, data json.RawMessage) {
	var p problem
	if json.Unmarshal(data, &p) != nil || p.ID == 0 {
		return
	}
	reply, err := a.replace(ctx, p)
	status := "resolved"
	if err != nil {
		status, reply = "open", "Couldn't get another copy: "+err.Error()
	}
	if err := a.call(ctx, "PUT", "/problems/"+strconv.FormatInt(p.ID, 10), map[string]string{"status": status, "reply": reply}, nil); err != nil {
		log.Printf("problem %d: telling BingeBandit: %v", p.ID, err)
	}
}

func (a *app) replace(ctx context.Context, p problem) (string, error) {
	kind := "movie"
	if p.ItemKind == "episode" {
		kind = "show"
	}
	if p.TMDBID == 0 {
		return "", errors.New("the title isn't matched to TMDB")
	}
	c, err := a.instance(ctx, kind, false)
	if err != nil {
		return "", err
	}
	t, err := c.find(ctx, p.TMDBID)
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", errors.New(c.name() + " doesn't have this title")
	}
	return c.replace(ctx, t, p.Season, p.Episode)
}
