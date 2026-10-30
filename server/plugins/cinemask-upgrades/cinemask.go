package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Cinemask is a client for the parts of Cinemask's API this plugin uses.
// The key must belong to a Cinemask manager or admin, with full access.
type Cinemask struct {
	base string
	key  string
	http *http.Client
}

var errNotSetUp = errors.New("set Cinemask's address and an API key in this plugin's settings first")

func newCinemask(address, key string) (*Cinemask, error) {
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
	// Searches ask every indexer, which can take a while.
	return &Cinemask{base: address, key: strings.TrimSpace(key), http: &http.Client{Timeout: 2 * time.Minute}}, nil
}

// Cinemask (or the indexers behind it) answers 429 Too Many Requests when
// asked too often. do waits as long as Retry-After says (or 15 s, 30 s, 60 s,
// 120 s without it) and tries again before giving up.
var rateLimitWaits = []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute}

// maxRetryAfter caps a Retry-After so a bad header can't stall a search for hours.
const maxRetryAfter = 5 * time.Minute

func (c *Cinemask) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	for attempt := 0; ; attempt++ {
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("X-Api-Key", c.key)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("couldn't reach Cinemask: %w", err)
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt >= len(rateLimitWaits) {
				return errRateLimited
			}
			wait := retryAfter(resp.Header.Get("Retry-After"), rateLimitWaits[attempt])
			log.Printf("Cinemask is rate limiting %s %s; trying again in %s", method, path, wait)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		if resp.StatusCode >= 300 {
			var e struct {
				Error string `json:"error"`
			}
			json.Unmarshal(b, &e)
			switch {
			case resp.StatusCode == http.StatusUnauthorized:
				return errors.New("Cinemask didn't accept the API key")
			case resp.StatusCode == http.StatusForbidden && method != "GET":
				return fmt.Errorf("Cinemask refused (%s): the key needs full access and a manager or admin owner", orStatus(e.Error, resp))
			case e.Error != "":
				return fmt.Errorf("Cinemask: %s", e.Error)
			}
			return fmt.Errorf("Cinemask answered %s", resp.Status)
		}
		if out != nil {
			if err := json.Unmarshal(b, out); err != nil {
				return fmt.Errorf("unexpected answer from Cinemask: %w", err)
			}
		}
		return nil
	}
}

var errRateLimited = errors.New("Cinemask kept answering 429 Too Many Requests; try again later or search fewer titles at once")

// retryAfter reads a Retry-After header: seconds, or an HTTP date.
func retryAfter(h string, fallback time.Duration) time.Duration {
	wait := fallback
	if n, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && n >= 0 {
		wait = time.Duration(n) * time.Second
	} else if t, err := http.ParseTime(h); err == nil {
		wait = time.Until(t)
	}
	return min(max(wait, 0), maxRetryAfter)
}

// rateLimited tells whether an error (ours, or one Cinemask passed on from an
// indexer) is about asking too often.
func rateLimited(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errRateLimited) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") || strings.Contains(msg, "too many requests") || strings.Contains(msg, "rate limit")
}

func orStatus(msg string, resp *http.Response) string {
	if msg != "" {
		return msg
	}
	return resp.Status
}

// Profile is a Cinemask quality profile.
type Profile struct {
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	Cutoff          string   `json:"cutoff"`
	UpgradesAllowed bool     `json:"upgradesAllowed"`
	Qualities       []string `json:"qualities"`
}

// Monitored is a title Cinemask follows.
type Monitored struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	Monitored   bool     `json:"monitored"`
	ProfileID   int64    `json:"profileId"`
	Profile     *Profile `json:"profile"`
	FileQuality string   `json:"fileQuality"`
	Episodes    []struct {
		Season      int    `json:"season"`
		Episode     int    `json:"episode"`
		FileQuality string `json:"fileQuality"`
	} `json:"episodes"`
}

// Me checks the key and says whose it is.
func (c *Cinemask) Me(ctx context.Context) (username, role string, err error) {
	var me struct {
		User struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	err = c.do(ctx, "GET", "/api/auth/me", nil, &me)
	return me.User.Username, me.User.Role, err
}

// Lookup finds a title Cinemask monitors; nil when it doesn't.
func (c *Cinemask) Lookup(ctx context.Context, kind string, tmdbID int64) (*Monitored, error) {
	var out struct {
		Monitored *Monitored `json:"monitored"`
	}
	if err := c.do(ctx, "GET", "/api/monitored/lookup/"+kind+"/"+strconv.FormatInt(tmdbID, 10), nil, &out); err != nil {
		return nil, err
	}
	if out.Monitored != nil && out.Monitored.Profile == nil {
		return nil, errors.New("the API key's owner isn't a Cinemask manager, so it can't search")
	}
	return out.Monitored, nil
}

func (c *Cinemask) Profiles(ctx context.Context) ([]Profile, error) {
	var out []Profile
	return out, c.do(ctx, "GET", "/api/profiles", nil, &out)
}

// Scope narrows a show's search to a season or an episode.
type Scope struct {
	Season  *int `json:"season,omitempty"`
	Episode *int `json:"episode,omitempty"`
}

func (s Scope) query() string {
	v := url.Values{}
	if s.Season != nil {
		v.Set("season", strconv.Itoa(*s.Season))
	}
	if s.Episode != nil {
		v.Set("episode", strconv.Itoa(*s.Episode))
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// SearchResult is what an automatic search did.
type SearchResult struct {
	Grabbed int      `json:"grabbed"`
	Message string   `json:"message"`
	Errors  []string `json:"errors"`
}

// Search has Cinemask search its indexers and grab the best release that
// beats what's on disk, as its quality profile allows.
func (c *Cinemask) Search(ctx context.Context, id int64, s Scope) (SearchResult, error) {
	var out SearchResult
	err := c.do(ctx, "POST", "/api/monitored/"+strconv.FormatInt(id, 10)+"/search", s, &out)
	return out, err
}

// Release is one search result, with Cinemask's verdict.
type Release struct {
	GUID       string   `json:"guid"`
	Title      string   `json:"title"`
	Indexer    string   `json:"indexer"`
	Protocol   string   `json:"protocol"`
	Size       float64  `json:"size"` // bytes; JavaScript numbers, so not always integers
	Seeders    *int     `json:"seeders"`
	Quality    string   `json:"quality"`
	Accepted   bool     `json:"accepted"`
	Rejections []string `json:"rejections"`
	Grabbed    *string  `json:"grabbed"`
}

func (c *Cinemask) Releases(ctx context.Context, id int64, s Scope) ([]Release, []string, error) {
	var out struct {
		Errors   []string  `json:"errors"`
		Releases []Release `json:"releases"`
	}
	err := c.do(ctx, "GET", "/api/monitored/"+strconv.FormatInt(id, 10)+"/releases"+s.query(), nil, &out)
	return out.Releases, out.Errors, err
}

// Grab sends a release from the last search to Cinemask's download client.
func (c *Cinemask) Grab(ctx context.Context, id int64, guid string) error {
	return c.do(ctx, "POST", "/api/monitored/"+strconv.FormatInt(id, 10)+"/grab", map[string]string{"guid": guid}, nil)
}

// SetProfile moves a monitored title to another quality profile.
func (c *Cinemask) SetProfile(ctx context.Context, id, profileID int64) error {
	return c.do(ctx, "PATCH", "/api/monitored/"+strconv.FormatInt(id, 10), map[string]int64{"profileId": profileID}, nil)
}

// RootFolder is one of Cinemask's library folders: the main Movies and TV
// ones ("default") and extras such as Kids Movies.
type RootFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func (c *Cinemask) RootFolders(ctx context.Context) ([]RootFolder, error) {
	var out struct {
		Roots []RootFolder `json:"roots"`
	}
	return out.Roots, c.do(ctx, "GET", "/api/root-folders", nil, &out)
}

// rootFor is the id of the folder of this kind named like a BingeBandit
// library, or "" for Cinemask's choice.
func rootFor(roots []RootFolder, kind, library string) string {
	for _, r := range roots {
		if r.Kind == kind && strings.EqualFold(strings.TrimSpace(r.Name), strings.TrimSpace(library)) {
			return r.ID
		}
	}
	return ""
}

// Monitor starts following a title, without searching yet, in root folder
// rootID ("" for the main one).
func (c *Cinemask) Monitor(ctx context.Context, kind string, tmdbID, profileID int64, rootID string, search bool) (*Monitored, error) {
	body := map[string]any{"kind": kind, "tmdbId": tmdbID, "search": search}
	if profileID > 0 {
		body["profileId"] = profileID
	}
	if rootID != "" {
		body["rootId"] = rootID
	}
	var out Monitored
	if err := c.do(ctx, "POST", "/api/monitored", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Link is the title's page in Cinemask.
func (c *Cinemask) Link(kind string, tmdbID int64) string {
	path := "/movie/"
	if kind == "show" {
		path = "/tv/"
	}
	return c.base + path + strconv.FormatInt(tmdbID, 10)
}
