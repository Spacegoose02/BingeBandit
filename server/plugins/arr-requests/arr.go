package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// arr is a client for the parts of the Sonarr and Radarr v3 APIs this
// plugin uses.
type arr struct {
	kind    string // radarr | sonarr
	base    string
	key     string
	profile string // quality profile name (or id); "" for the first
	root    string // root folder path; "" for the first
	http    *http.Client
}

var errNotSetUp = errors.New("not set up")

func newArr(kind, address, key, profile, root string) (*arr, error) {
	address = strings.TrimRight(strings.TrimSpace(address), "/")
	if address == "" || strings.TrimSpace(key) == "" {
		return nil, errNotSetUp
	}
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q isn't a web address", address)
	}
	return &arr{kind: kind, base: address, key: strings.TrimSpace(key), profile: strings.TrimSpace(profile), root: strings.TrimSpace(root),
		http: &http.Client{Timeout: time.Minute}}, nil
}

func (a *arr) name() string {
	if a.kind == "sonarr" {
		return "Sonarr"
	}
	return "Radarr"
}

func (a *arr) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+"/api/v3"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", a.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach %s: %w", a.name(), err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%s didn't accept the API key", a.name())
	case resp.StatusCode >= 300:
		return fmt.Errorf("%s answered %s: %s", a.name(), resp.Status, arrMessage(b))
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

// arrMessage pulls the error out of an *arr answer (an object with
// "message", or a list of validation errors).
func arrMessage(b []byte) string {
	var one struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &one) == nil && one.Message != "" {
		return one.Message
	}
	var many []struct {
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(b, &many) == nil && len(many) > 0 {
		return many[0].ErrorMessage
	}
	if len(b) > 200 {
		b = b[:200]
	}
	return strings.TrimSpace(string(b))
}

type arrProfile struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type arrRoot struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
}

// profileID finds the configured quality profile, or the first one.
func (a *arr) profileID(ctx context.Context) (int64, error) {
	var list []arrProfile
	if err := a.do(ctx, "GET", "/qualityprofile", nil, &list); err != nil {
		return 0, err
	}
	for _, p := range list {
		if a.profile == "" || strings.EqualFold(p.Name, a.profile) || strconv.FormatInt(p.ID, 10) == a.profile {
			return p.ID, nil
		}
	}
	if a.profile == "" {
		return 0, fmt.Errorf("%s has no quality profiles", a.name())
	}
	return 0, fmt.Errorf("%s has no quality profile called %q", a.name(), a.profile)
}

// rootPath finds the configured root folder, or the first one.
func (a *arr) rootPath(ctx context.Context) (string, error) {
	var list []arrRoot
	if err := a.do(ctx, "GET", "/rootfolder", nil, &list); err != nil {
		return "", err
	}
	for _, r := range list {
		if a.root == "" || strings.TrimRight(r.Path, `/\`) == strings.TrimRight(a.root, `/\`) {
			return r.Path, nil
		}
	}
	if a.root == "" {
		return "", fmt.Errorf("%s has no root folders", a.name())
	}
	return "", fmt.Errorf("%s has no root folder %q", a.name(), a.root)
}

// title is a movie (Radarr) or series (Sonarr), as both APIs send them;
// the rest of the object is kept so it can be sent back as it came.
type title struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Year    int    `json:"year"`
	TMDBID  int64  `json:"tmdbId"`
	TVDBID  int64  `json:"tvdbId"`
	Seasons []struct {
		SeasonNumber int  `json:"seasonNumber"`
		Monitored    bool `json:"monitored"`
	} `json:"seasons"`
	raw map[string]any
}

func decodeTitles(b []byte) ([]title, error) {
	var raws []map[string]any
	if err := json.Unmarshal(b, &raws); err != nil {
		return nil, err
	}
	out := make([]title, 0, len(raws))
	for _, r := range raws {
		b, _ := json.Marshal(r)
		var t title
		json.Unmarshal(b, &t)
		t.raw = r
		out = append(out, t)
	}
	return out, nil
}

func (a *arr) titles(ctx context.Context, path string) ([]title, error) {
	var raw json.RawMessage
	if err := a.do(ctx, "GET", path, nil, &raw); err != nil {
		return nil, err
	}
	return decodeTitles(raw)
}

// find returns the movie or series the *arr already has for a TMDB id, or nil.
func (a *arr) find(ctx context.Context, tmdbID int64) (*title, error) {
	path := "/movie?tmdbId=" + strconv.FormatInt(tmdbID, 10)
	if a.kind == "sonarr" {
		path = "/series"
	}
	list, err := a.titles(ctx, path)
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		if t.TMDBID == tmdbID {
			return &t, nil
		}
	}
	return nil, nil
}

// lookup finds a title to add by its TMDB id. Sonarr is keyed on TheTVDB,
// so it's asked with "tmdb:" (Sonarr 4) and then by name, keeping the hit
// with this TMDB id (or the same name and year).
func (a *arr) lookup(ctx context.Context, tmdbID int64, name string, year int) (*title, error) {
	if a.kind == "radarr" {
		var raw json.RawMessage
		if err := a.do(ctx, "GET", "/movie/lookup/tmdb?tmdbId="+strconv.FormatInt(tmdbID, 10), nil, &raw); err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		b, _ := json.Marshal([]map[string]any{m})
		list, err := decodeTitles(b)
		if err != nil || len(list) == 0 {
			return nil, err
		}
		return &list[0], nil
	}
	for _, term := range []string{"tmdb:" + strconv.FormatInt(tmdbID, 10), name} {
		if strings.TrimSpace(term) == "" {
			continue
		}
		list, err := a.titles(ctx, "/series/lookup?term="+url.QueryEscape(term))
		if err != nil {
			continue
		}
		for _, t := range list {
			if t.TMDBID == tmdbID && t.TVDBID > 0 {
				return &t, nil
			}
		}
		for _, t := range list {
			if t.TMDBID == 0 && t.TVDBID > 0 && strings.EqualFold(t.Title, name) && (year == 0 || t.Year == year) {
				return &t, nil
			}
		}
	}
	return nil, fmt.Errorf("Sonarr couldn't find %q", name)
}

// add has the *arr follow a title and search for it. For a show, only the
// seasons given are followed (all of them when none are).
func (a *arr) add(ctx context.Context, t *title, seasons []int) error {
	profile, err := a.profileID(ctx)
	if err != nil {
		return err
	}
	root, err := a.rootPath(ctx)
	if err != nil {
		return err
	}
	body := t.raw
	body["qualityProfileId"] = profile
	body["rootFolderPath"] = root
	body["monitored"] = true
	if a.kind == "radarr" {
		body["minimumAvailability"] = "released"
		body["addOptions"] = map[string]any{"searchForMovie": true}
		return a.do(ctx, "POST", "/movie", body, nil)
	}
	body["seasonFolder"] = true
	body["seasons"] = seasonFlags(t, seasons, nil)
	body["addOptions"] = map[string]any{"searchForMissingEpisodes": true, "ignoreEpisodesWithFiles": true}
	return a.do(ctx, "POST", "/series", body, nil)
}

// seasonFlags marks the seasons to follow: the ones asked for (every
// season but specials when none are) plus the ones already followed.
func seasonFlags(t *title, want []int, keep map[int]bool) []map[string]any {
	out := []map[string]any{}
	for _, s := range t.Seasons {
		on := keep[s.SeasonNumber]
		if len(want) == 0 {
			on = on || s.SeasonNumber > 0
		}
		for _, n := range want {
			on = on || n == s.SeasonNumber
		}
		out = append(out, map[string]any{"seasonNumber": s.SeasonNumber, "monitored": on})
	}
	return out
}

// fetchMore has a title the *arr already follows also follow these seasons
// (or the whole show), then searches for them.
func (a *arr) fetchMore(ctx context.Context, t *title, seasons []int) error {
	if a.kind == "radarr" {
		if !isTrue(t.raw["monitored"]) {
			t.raw["monitored"] = true
			if err := a.do(ctx, "PUT", "/movie/"+strconv.FormatInt(t.ID, 10), t.raw, nil); err != nil {
				return err
			}
		}
		return a.command(ctx, map[string]any{"name": "MoviesSearch", "movieIds": []int64{t.ID}})
	}
	keep := map[int]bool{}
	for _, s := range t.Seasons {
		keep[s.SeasonNumber] = s.Monitored
	}
	t.raw["monitored"] = true
	t.raw["seasons"] = seasonFlags(t, seasons, keep)
	if err := a.do(ctx, "PUT", "/series/"+strconv.FormatInt(t.ID, 10), t.raw, nil); err != nil {
		return err
	}
	if len(seasons) == 0 {
		return a.command(ctx, map[string]any{"name": "SeriesSearch", "seriesId": t.ID})
	}
	for _, n := range seasons {
		if err := a.command(ctx, map[string]any{"name": "SeasonSearch", "seriesId": t.ID, "seasonNumber": n}); err != nil {
			return err
		}
	}
	return nil
}

func isTrue(v any) bool { b, _ := v.(bool); return b }

func (a *arr) command(ctx context.Context, body map[string]any) error {
	return a.do(ctx, "POST", "/command", body, nil)
}

// link is the title's page in the *arr's web app.
func (a *arr) link(t *title, tmdbID int64) string {
	if a.kind == "radarr" {
		return a.base + "/movie/" + strconv.FormatInt(tmdbID, 10)
	}
	if t != nil {
		if slug, _ := t.raw["titleSlug"].(string); slug != "" {
			return a.base + "/series/" + url.PathEscape(slug)
		}
	}
	return a.base + "/series"
}

// queueItem is one download in the *arr's queue.
type queueItem struct {
	MovieID                 int64   `json:"movieId"`
	SeriesID                int64   `json:"seriesId"`
	EpisodeID               int64   `json:"episodeId"`
	Size                    float64 `json:"size"`
	SizeLeft                float64 `json:"sizeleft"`
	Status                  string  `json:"status"`
	TrackedDownloadState    string  `json:"trackedDownloadState"`
	EstimatedCompletionTime string  `json:"estimatedCompletionTime"`
}

func (a *arr) queue(ctx context.Context) ([]queueItem, error) {
	var out struct {
		Records []queueItem `json:"records"`
	}
	return out.Records, a.do(ctx, "GET", "/queue?pageSize=500", nil, &out)
}

// history is one grab in the *arr's history.
type history struct {
	ID        int64 `json:"id"`
	EpisodeID int64 `json:"episodeId"`
}

// replace marks the last grab of a movie (or an episode) as failed, which
// blocklists that release and has the *arr search for another. With no grab
// in its history (a file added by hand), it just searches.
func (a *arr) replace(ctx context.Context, t *title, season, episode *int) (string, error) {
	var episodeID int64
	if a.kind == "sonarr" {
		if season == nil || episode == nil {
			return "", errors.New("which episode is it?")
		}
		var eps []struct {
			ID            int64 `json:"id"`
			SeasonNumber  int   `json:"seasonNumber"`
			EpisodeNumber int   `json:"episodeNumber"`
		}
		if err := a.do(ctx, "GET", "/episode?seriesId="+strconv.FormatInt(t.ID, 10), nil, &eps); err != nil {
			return "", err
		}
		for _, e := range eps {
			if e.SeasonNumber == *season && e.EpisodeNumber == *episode {
				episodeID = e.ID
			}
		}
		if episodeID == 0 {
			return "", fmt.Errorf("Sonarr doesn't list S%02dE%02d", *season, *episode)
		}
	}
	path := "/history/movie?eventType=grabbed&movieId=" + strconv.FormatInt(t.ID, 10)
	if a.kind == "sonarr" {
		path = fmt.Sprintf("/history/series?eventType=grabbed&seriesId=%d&seasonNumber=%d", t.ID, *season)
	}
	var grabs []history
	if err := a.do(ctx, "GET", path, nil, &grabs); err != nil {
		return "", err
	}
	for _, h := range grabs {
		if a.kind == "sonarr" && h.EpisodeID != episodeID {
			continue
		}
		if err := a.do(ctx, "POST", "/history/failed/"+strconv.FormatInt(h.ID, 10), nil, nil); err != nil {
			return "", err
		}
		return a.name() + " blocklisted the old copy and is looking for another.", nil
	}
	cmd := map[string]any{"name": "MoviesSearch", "movieIds": []int64{t.ID}}
	if a.kind == "sonarr" {
		cmd = map[string]any{"name": "EpisodeSearch", "episodeIds": []int64{episodeID}}
	}
	if err := a.command(ctx, cmd); err != nil {
		return "", err
	}
	return a.name() + " is searching for a new copy.", nil
}
