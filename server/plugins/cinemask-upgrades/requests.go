package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Title requests: when an admin approves a movie or show someone asked for
// in BingeBandit, Cinemask monitors it (just the seasons asked for, and at
// the 4K profile when the 4K copy was) and searches for it at once, and
// BingeBandit is told it was sent (or why it couldn't be). Approved
// requests are picked up on the request.approved event and every 15
// minutes, so ones approved while the plugin was off or Cinemask was down
// go through later. While they download, their progress and ETA are
// passed on every 2 minutes. A problem report an admin sends off
// (problem.refetch) has Cinemask grab a different copy.

// request is a title request, as the plugin API lists it.
type request struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	TMDBID      int64  `json:"tmdbId"`
	Title       string `json:"title"`
	Seasons     []int  `json:"seasons"`
	FourK       bool   `json:"fourK"`
	ReleaseDate string `json:"releaseDate"`
}

// progress is how a sent request is getting on.
type progress struct {
	Stage    string  `json:"stage"`
	Progress float64 `json:"progress"`
	ETA      int64   `json:"eta,omitempty"`
}

var (
	requestInterval  = 15 * time.Minute
	progressInterval = 2 * time.Minute
)

func (a *app) requestsOn(ctx context.Context) bool { return a.conf(ctx)["requests"] != "false" }

func (a *app) requestsWith(ctx context.Context, status string) ([]request, error) {
	var out []request
	return out, a.p.Call(ctx, "GET", "/requests?status="+status, nil, &out)
}

func (a *app) approvedRequests(ctx context.Context) ([]request, error) {
	return a.requestsWith(ctx, "approved")
}

func (a *app) reportRequest(ctx context.Context, id int64, status, reason, link string) error {
	return a.p.Call(ctx, "PUT", "/requests/"+strconv.FormatInt(id, 10),
		map[string]string{"status": status, "reason": reason, "link": link}, nil)
}

func (a *app) reportProgress(ctx context.Context, id int64, p progress) error {
	return a.p.Call(ctx, "PUT", "/requests/"+strconv.FormatInt(id, 10),
		map[string]any{"status": "sent", "stage": p.Stage, "progress": p.Progress, "eta": p.ETA}, nil)
}

func (a *app) watchRequests(ctx context.Context) {
	t := time.NewTicker(requestInterval)
	defer t.Stop()
	p := time.NewTicker(progressInterval)
	defer p.Stop()
	for {
		a.sendRequests(ctx)
		a.trackRequests(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-p.C:
			a.trackRequests(ctx)
			continue
		}
	}
}

// requestsMu keeps two runs (an event and the ticker) from sending the
// same request twice.
var requestsMu sync.Mutex

// sendRequests hands every approved request to Cinemask, the most wanted first.
func (a *app) sendRequests(ctx context.Context) {
	requestsMu.Lock()
	defer requestsMu.Unlock()
	if a.listRequests == nil || !a.requestsOn(ctx) {
		return
	}
	list, err := a.listRequests(ctx)
	if err != nil {
		log.Printf("requests: %v", err)
		return
	}
	if len(list) == 0 {
		return
	}
	cm, err := a.cinemask(ctx)
	if err != nil {
		// Not set up yet: they stay approved until it is.
		log.Printf("requests: %d waiting: %v", len(list), err)
		return
	}
	for _, r := range list {
		if ctx.Err() != nil {
			return
		}
		status, reason := "sent", ""
		if err := sendRequest(ctx, cm, r, a.conf(ctx)["profile_4k"]); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			status, reason = "failed", err.Error()
			log.Printf("request %q: %v", r.Title, err)
		}
		if err := a.setRequest(ctx, r.ID, status, reason, cm.Link(r.Kind, r.TMDBID)); err != nil {
			log.Printf("request %q: telling BingeBandit: %v", r.Title, err)
		}
	}
}

// profileNamed finds a quality profile by name (any case), or by id.
func profileNamed(ctx context.Context, cm *Cinemask, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("set the 4K quality profile in the Cinemask Upgrades settings to fetch 4K requests")
	}
	list, err := cm.Profiles(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range list {
		if strings.EqualFold(p.Name, name) || strconv.FormatInt(p.ID, 10) == name {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("Cinemask has no quality profile called %q", name)
}

// sendRequest has Cinemask monitor the title and search for it, or just
// search when it already monitors it. A 4K request uses (or moves the title
// to) the profile named profile4K.
func sendRequest(ctx context.Context, cm *Cinemask, r request, profile4K string) error {
	if r.Kind != "movie" && r.Kind != "show" {
		return errors.New("only movies and shows can be fetched")
	}
	var profile int64
	if r.FourK {
		var err error
		if profile, err = profileNamed(ctx, cm, profile4K); err != nil {
			return err
		}
	}
	have, err := cm.Lookup(ctx, r.Kind, r.TMDBID)
	if err != nil {
		return err
	}
	if have == nil {
		_, err = cm.Add(ctx, r.Kind, r.TMDBID, profile, r.Seasons, true)
		return err
	}
	if profile > 0 && have.ProfileID != profile {
		if err := cm.SetProfile(ctx, have.ID, profile); err != nil {
			return err
		}
	}
	scopes := []Scope{{}}
	if r.Kind == "show" && len(r.Seasons) > 0 {
		scopes = scopes[:0]
		for _, n := range r.Seasons {
			scopes = append(scopes, Scope{Season: &n})
		}
	}
	for _, sc := range scopes {
		res, err := cm.Search(ctx, have.ID, sc)
		if err == nil && len(res.Errors) > 0 {
			err = errors.New(res.Errors[0])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// trackRequests passes on how each sent request's downloads are doing.
func (a *app) trackRequests(ctx context.Context) {
	if a.listSent == nil || !a.requestsOn(ctx) {
		return
	}
	cm, err := a.cinemask(ctx)
	if err != nil {
		return
	}
	list, err := a.listSent(ctx)
	if err != nil || len(list) == 0 {
		return
	}
	downloads, err := cm.Downloads(ctx)
	if err != nil {
		log.Printf("requests: downloads: %v", err)
		return
	}
	for _, r := range list {
		if ctx.Err() != nil {
			return
		}
		m, err := cm.Lookup(ctx, r.Kind, r.TMDBID)
		if err != nil || m == nil {
			continue
		}
		if err := a.setProgress(ctx, r.ID, progressOf(r, m.ID, downloads, time.Now())); err != nil {
			log.Printf("request %q: progress: %v", r.Title, err)
		}
	}
}

// progressOf sums up a title's active downloads: their combined progress,
// the latest ETA, and the stage of the one furthest behind. With none, it's
// searching, or waiting when it isn't out yet.
func progressOf(r request, monitoredID int64, downloads []Download, now time.Time) progress {
	var p progress
	n := 0
	for _, d := range downloads {
		if d.MonitoredID != monitoredID {
			continue
		}
		n++
		p.Progress += d.Progress
		if d.ETA != nil && *d.ETA > 0 {
			p.ETA = max(p.ETA, now.Unix()+int64(*d.ETA))
		}
		stage := "downloading"
		if d.Status == "completed" || d.Status == "importing" {
			stage = "importing"
		}
		if p.Stage == "" || stage == "downloading" {
			p.Stage = stage
		}
	}
	if n == 0 {
		p.Stage = "searching"
		if r.ReleaseDate != "" && r.ReleaseDate > now.Format("2006-01-02") {
			p.Stage = "waiting"
		}
		return p
	}
	p.Progress /= float64(n)
	return p
}

// problem is a problem report an admin sent off to be fixed.
type problem struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	ItemKind string `json:"itemKind"`
	TMDBID   int64  `json:"tmdbId"`
	Title    string `json:"title"`
	Show     string `json:"showTitle"`
	Season   *int   `json:"seasonNumber"`
	Episode  *int   `json:"episodeNumber"`
}

func (a *app) onRefetch(ctx context.Context, data json.RawMessage) {
	var p problem
	if json.Unmarshal(data, &p) != nil || p.ID == 0 {
		return
	}
	status, reply := "resolved", ""
	cm, err := a.cinemask(ctx)
	if err == nil {
		reply, err = refetch(ctx, cm, p)
	}
	if err != nil {
		status, reply = "open", "Cinemask couldn't get another copy: "+err.Error()
	}
	if err := a.p.Call(ctx, "PUT", "/problems/"+strconv.FormatInt(p.ID, 10), map[string]string{"status": status, "reply": reply}, nil); err != nil {
		log.Printf("problem %d: telling BingeBandit: %v", p.ID, err)
	}
}

// replaceable are rejections that only say the file on disk is good
// enough, which a bad file isn't.
var replaceable = regexp.MustCompile(`(?i)upgrade|cutoff|already|existing|on disk`)

// refetch has Cinemask grab a different release of the movie or episode
// than the ones it already got.
func refetch(ctx context.Context, cm *Cinemask, p problem) (string, error) {
	kind := "movie"
	sc := Scope{}
	if p.ItemKind == "episode" {
		kind, sc = "show", Scope{Season: p.Season, Episode: p.Episode}
	}
	if p.TMDBID == 0 {
		return "", errors.New("the title isn't matched to TMDB")
	}
	m, err := cm.Lookup(ctx, kind, p.TMDBID)
	if err != nil {
		return "", err
	}
	if m == nil {
		if m, err = cm.Add(ctx, kind, p.TMDBID, 0, nil, false); err != nil {
			return "", err
		}
	}
	releases, errs, err := cm.Releases(ctx, m.ID, sc)
	if err != nil {
		return "", err
	}
	for _, r := range releases {
		if r.Grabbed != nil {
			continue
		}
		ok := r.Accepted
		if !ok && len(r.Rejections) > 0 {
			ok = true
			for _, why := range r.Rejections {
				ok = ok && replaceable.MatchString(why)
			}
		}
		if !ok {
			continue
		}
		if err := cm.Grab(ctx, m.ID, r.GUID); err != nil {
			return "", err
		}
		return "Getting a new copy: " + r.Title, nil
	}
	if len(errs) > 0 {
		return "", errors.New(errs[0])
	}
	return "", errors.New("its indexers have no other copy right now")
}
