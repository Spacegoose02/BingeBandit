// Package opensubs searches and downloads subtitles from OpenSubtitles.com
// (REST API v1). Searching needs only the server's API key; downloads also
// count against a daily limit, which signing in with an account raises.
package opensubs

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultBase = "https://api.opensubtitles.com/api/v1"

// Client talks to OpenSubtitles. Key, User and Password are read on each call
// through the funcs, so changed settings apply at once.
type Client struct {
	Base      string // API root; tests point it at a fake
	UserAgent string
	Key       func() string
	Login     func() (user, password string)
	HTTP      *http.Client

	mu      sync.Mutex
	token   string
	tokenOf string // user the token belongs to
	tokenAt time.Time
	apiBase string // the base the login answered with (VIP accounts get their own)
}

// ErrNoKey means the admin hasn't set an API key.
var ErrNoKey = errors.New("subtitle search needs an OpenSubtitles API key in Settings")

// Query is what to search for. IDs beat the title when known.
type Query struct {
	Languages []string // ISO 639-1 codes, e.g. en, es, pt-br
	TMDBID    int64    // the movie, or for an episode the show
	IMDbID    string
	Episode   bool
	Season    int
	Number    int
	Title     string
	Year      int
	Hash      string // OpenSubtitles hash of the video file
}

// Result is one subtitle file found.
type Result struct {
	FileID          int64  `json:"fileId"`
	Name            string `json:"name"`
	Language        string `json:"language"`
	Downloads       int    `json:"downloads"`
	HearingImpaired bool   `json:"hearingImpaired"`
	HashMatch       bool   `json:"hashMatch"` // made for this exact file
	Trusted         bool   `json:"trusted"`
	AI              bool   `json:"ai"` // machine or AI translated
	Uploader        string `json:"uploader,omitempty"`
}

func (c *Client) base() string {
	if c.Base != "" {
		return c.Base
	}
	return defaultBase
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) do(ctx context.Context, method, base, path string, body any, token string, out any) error {
	key := strings.TrimSpace(c.Key())
	if key == "" {
		return ErrNoKey
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Api-Key", key)
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("OpenSubtitles: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		}
		json.Unmarshal(b, &e)
		msg := e.Message
		if msg == "" && len(e.Errors) > 0 {
			msg = strings.Join(e.Errors, "; ")
		}
		if msg == "" {
			msg = resp.Status
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			msg = "OpenSubtitles turned down the API key or sign-in: " + msg
		}
		return &Error{Status: resp.StatusCode, Message: msg}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

// Error is an answer from OpenSubtitles other than success.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Search finds subtitles, best first: made for this file, then most downloaded.
func (c *Client) Search(ctx context.Context, q Query) ([]Result, error) {
	v := url.Values{}
	if len(q.Languages) > 0 {
		v.Set("languages", strings.Join(q.Languages, ","))
	}
	if q.Hash != "" {
		v.Set("moviehash", q.Hash)
	}
	imdb := strings.TrimLeft(strings.TrimPrefix(strings.ToLower(q.IMDbID), "tt"), "0")
	switch {
	case q.Episode:
		if q.TMDBID > 0 {
			v.Set("parent_tmdb_id", strconv.FormatInt(q.TMDBID, 10))
		} else if imdb != "" {
			v.Set("parent_imdb_id", imdb)
		} else {
			v.Set("query", strings.ToLower(q.Title))
		}
		v.Set("season_number", strconv.Itoa(q.Season))
		v.Set("episode_number", strconv.Itoa(q.Number))
		v.Set("type", "episode")
	default:
		if q.TMDBID > 0 {
			v.Set("tmdb_id", strconv.FormatInt(q.TMDBID, 10))
		} else if imdb != "" {
			v.Set("imdb_id", imdb)
		} else {
			v.Set("query", strings.ToLower(q.Title))
			if q.Year > 0 {
				v.Set("year", strconv.Itoa(q.Year))
			}
		}
		v.Set("type", "movie")
	}
	// The API redirects unless parameters are sorted, which Encode does.
	var resp struct {
		Data []struct {
			Attributes struct {
				Language          string `json:"language"`
				DownloadCount     int    `json:"download_count"`
				HearingImpaired   bool   `json:"hearing_impaired"`
				Release           string `json:"release"`
				FromTrusted       bool   `json:"from_trusted"`
				AITranslated      bool   `json:"ai_translated"`
				MachineTranslated bool   `json:"machine_translated"`
				MoviehashMatch    bool   `json:"moviehash_match"`
				Uploader          struct {
					Name string `json:"name"`
				} `json:"uploader"`
				Files []struct {
					FileID   int64  `json:"file_id"`
					FileName string `json:"file_name"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, c.base(), "/subtitles?"+v.Encode(), nil, "", &resp); err != nil {
		return nil, err
	}
	out := []Result{}
	for _, d := range resp.Data {
		a := d.Attributes
		if len(a.Files) == 0 {
			continue
		}
		name := a.Release
		if name == "" {
			name = a.Files[0].FileName
		}
		out = append(out, Result{
			FileID: a.Files[0].FileID, Name: name, Language: a.Language, Downloads: a.DownloadCount,
			HearingImpaired: a.HearingImpaired, HashMatch: a.MoviehashMatch, Trusted: a.FromTrusted,
			AI: a.AITranslated || a.MachineTranslated, Uploader: a.Uploader.Name,
		})
	}
	rank := func(r Result) int {
		n := 0
		if r.HashMatch {
			n -= 4
		}
		if r.AI {
			n += 2
		}
		return n
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i].Downloads > out[j].Downloads
	})
	return out, nil
}

// login returns a token for the configured account ("" without one) and the
// API base to download from.
func (c *Client) login(ctx context.Context) (string, string, error) {
	user, pass := "", ""
	if c.Login != nil {
		user, pass = c.Login()
	}
	user = strings.TrimSpace(user)
	c.mu.Lock()
	defer c.mu.Unlock()
	if user == "" || pass == "" {
		return "", c.base(), nil
	}
	if c.token != "" && c.tokenOf == user && time.Since(c.tokenAt) < 12*time.Hour {
		return c.token, c.apiBase, nil
	}
	var resp struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	if err := c.do(ctx, http.MethodPost, c.base(), "/login", map[string]string{"username": user, "password": pass}, "", &resp); err != nil {
		return "", "", err
	}
	c.token, c.tokenOf, c.tokenAt, c.apiBase = resp.Token, user, time.Now(), c.base()
	// VIP accounts are sent to their own host; keep tests' fake base as is.
	if resp.BaseURL != "" && c.Base == "" {
		c.apiBase = "https://" + strings.TrimPrefix(strings.TrimPrefix(resp.BaseURL, "https://"), "http://") + "/api/v1"
	}
	return c.token, c.apiBase, nil
}

// Download fetches a subtitle file as SubRip text.
func (c *Client) Download(ctx context.Context, fileID int64) ([]byte, error) {
	token, base, err := c.login(ctx)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Link      string `json:"link"`
		Remaining int    `json:"remaining"`
		Message   string `json:"message"`
	}
	if err := c.do(ctx, http.MethodPost, base, "/download", map[string]any{"file_id": fileID, "sub_format": "srt"}, token, &resp); err != nil {
		return nil, err
	}
	if resp.Link == "" {
		msg := resp.Message
		if msg == "" {
			msg = "OpenSubtitles gave no download link"
		}
		return nil, errors.New(msg)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resp.Link, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	r, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenSubtitles download: %w", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenSubtitles download: %s", r.Status)
	}
	return io.ReadAll(io.LimitReader(r.Body, 8<<20))
}

// Hash is the OpenSubtitles hash of a file: its size plus the sum of its
// first and last 64 KiB read as little-endian uint64s. "" for small files.
func Hash(path string) string {
	const chunk = 64 * 1024
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < chunk*2 {
		return ""
	}
	sum := uint64(st.Size())
	buf := make([]byte, chunk)
	for _, at := range []int64{0, st.Size() - chunk} {
		if _, err := f.ReadAt(buf, at); err != nil {
			return ""
		}
		for i := 0; i < chunk; i += 8 {
			sum += binary.LittleEndian.Uint64(buf[i:])
		}
	}
	return fmt.Sprintf("%016x", sum)
}

// iso1 maps the ISO 639-2 codes ffprobe uses to the codes OpenSubtitles uses.
var iso1 = map[string]string{
	"eng": "en", "spa": "es", "fre": "fr", "ger": "de", "ita": "it", "por": "pt-pt,pt-br", "dut": "nl", "swe": "sv",
	"nor": "no", "dan": "da", "fin": "fi", "pol": "pl", "rus": "ru", "ukr": "uk", "cze": "cs", "gre": "el",
	"tur": "tr", "ara": "ar", "heb": "he", "hin": "hi", "jpn": "ja", "kor": "ko", "chi": "zh-cn,zh-tw", "tha": "th",
	"vie": "vi", "ind": "id", "hun": "hu", "rum": "ro", "bul": "bg", "hrv": "hr", "srp": "sr",
}

// Code is the OpenSubtitles code (or codes, comma-separated) for an ISO
// 639-2 code ("" when unknown).
func Code(iso2 string) string { return iso1[iso2] }
