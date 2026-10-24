// Package trakt syncs watch history with Trakt (trakt.tv): people connect
// with a device code, and finished plays are sent to their Trakt history.
package trakt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client talks to the Trakt API with the server's app credentials.
type Client struct {
	API      string // https://api.trakt.tv
	ClientID string
	Secret   string
	HTTP     *http.Client
}

func NewClient(api, clientID, secret string) *Client {
	return &Client{API: strings.TrimRight(api, "/"), ClientID: clientID, Secret: secret, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

var (
	ErrPending  = errors.New("waiting for approval")
	ErrSlowDown = errors.New("polling too fast")
	ErrExpired  = errors.New("the code expired; start again")
	ErrDenied   = errors.New("the request was declined on Trakt")
	ErrAuth     = errors.New("Trakt no longer accepts this connection; connect again")
)

// statusError is a non-2xx answer.
type statusError struct {
	code int
	body string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("Trakt answered %d: %s", e.code, strings.TrimSpace(e.body))
}

func (c *Client) do(ctx context.Context, method, path, token string, in, out any) (http.Header, error) {
	var body *bytes.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.API+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", "2")
	req.Header.Set("trakt-api-key", c.ClientID)
	req.Header.Set("User-Agent", "BingeBandit")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach Trakt: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		return resp.Header, &statusError{resp.StatusCode, buf.String()}
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.Header, fmt.Errorf("Trakt sent an unexpected answer: %w", err)
		}
	}
	return resp.Header, nil
}

// DeviceCode is what the person types at VerificationURL.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

func (c *Client) DeviceCode(ctx context.Context) (*DeviceCode, error) {
	var d DeviceCode
	_, err := c.do(ctx, http.MethodPost, "/oauth/device/code", "", map[string]string{"client_id": c.ClientID}, &d)
	return &d, err
}

// Token is an OAuth token pair.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	CreatedAt    int64  `json:"created_at"`
}

func (t *Token) ExpiresAt() int64 {
	created := t.CreatedAt
	if created == 0 {
		created = time.Now().Unix()
	}
	return created + t.ExpiresIn
}

// PollToken checks whether the person approved the device code yet.
func (c *Client) PollToken(ctx context.Context, deviceCode string) (*Token, error) {
	var t Token
	_, err := c.do(ctx, http.MethodPost, "/oauth/device/token", "",
		map[string]string{"code": deviceCode, "client_id": c.ClientID, "client_secret": c.Secret}, &t)
	var se *statusError
	if errors.As(err, &se) {
		switch se.code {
		case 400:
			return nil, ErrPending
		case 429:
			return nil, ErrSlowDown
		case 404, 409, 410:
			return nil, ErrExpired
		case 418:
			return nil, ErrDenied
		}
	}
	return &t, err
}

func (c *Client) Refresh(ctx context.Context, refresh string) (*Token, error) {
	var t Token
	_, err := c.do(ctx, http.MethodPost, "/oauth/token", "", map[string]string{
		"refresh_token": refresh, "client_id": c.ClientID, "client_secret": c.Secret,
		"redirect_uri": "urn:ietf:wg:oauth:2.0:oob", "grant_type": "refresh_token",
	}, &t)
	var se *statusError
	if errors.As(err, &se) && (se.code == 400 || se.code == 401) {
		return nil, ErrAuth
	}
	return &t, err
}

// Revoke ends the token on Trakt's side (best effort).
func (c *Client) Revoke(ctx context.Context, token string) {
	c.do(ctx, http.MethodPost, "/oauth/revoke", "", map[string]string{"token": token, "client_id": c.ClientID, "client_secret": c.Secret}, nil)
}

func (c *Client) Username(ctx context.Context, token string) (string, error) {
	var me struct {
		Username string `json:"username"`
	}
	_, err := c.do(ctx, http.MethodGet, "/users/me", token, nil, &me)
	return me.Username, err
}

// IDs are Trakt's id set for a movie, show or episode.
type IDs struct {
	Trakt int64  `json:"trakt,omitempty"`
	Slug  string `json:"slug,omitempty"`
	IMDB  string `json:"imdb,omitempty"`
	TMDB  int64  `json:"tmdb,omitempty"`
	TVDB  int64  `json:"tvdb,omitempty"`
}

type Media struct {
	Title string `json:"title,omitempty"`
	Year  int    `json:"year,omitempty"`
	IDs   IDs    `json:"ids"`
}

type Episode struct {
	Season int    `json:"season"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	IDs    IDs    `json:"ids"`
}

// HistoryEntry is one play in someone's Trakt history.
type HistoryEntry struct {
	ID        int64     `json:"id"`
	WatchedAt time.Time `json:"watched_at"`
	Type      string    `json:"type"`
	Movie     *Media    `json:"movie"`
	Show      *Media    `json:"show"`
	Episode   *Episode  `json:"episode"`
}

// History returns one page of movie or episode history and the page count.
func (c *Client) History(ctx context.Context, token, kind string, page int) ([]HistoryEntry, int, error) {
	var out []HistoryEntry
	h, err := c.do(ctx, http.MethodGet, "/sync/history/"+kind+"?limit=100&page="+strconv.Itoa(page), token, nil, &out)
	if err != nil {
		return nil, 0, err
	}
	pages, _ := strconv.Atoi(h.Get("X-Pagination-Page-Count"))
	return out, pages, nil
}

// AddHistory is the body of POST /sync/history.
type AddHistory struct {
	Movies []HistoryMovie `json:"movies,omitempty"`
	Shows  []HistoryShow  `json:"shows,omitempty"`
}

type HistoryMovie struct {
	WatchedAt string `json:"watched_at"`
	Title     string `json:"title,omitempty"`
	Year      int    `json:"year,omitempty"`
	IDs       IDs    `json:"ids"`
}

type HistoryShow struct {
	Title   string          `json:"title,omitempty"`
	Year    int             `json:"year,omitempty"`
	IDs     IDs             `json:"ids"`
	Seasons []HistorySeason `json:"seasons"`
}

type HistorySeason struct {
	Number   int              `json:"number"`
	Episodes []HistoryEpisode `json:"episodes"`
}

type HistoryEpisode struct {
	Number    int    `json:"number"`
	WatchedAt string `json:"watched_at"`
}

// AddResult says how much Trakt took.
type AddResult struct {
	Added struct {
		Movies   int `json:"movies"`
		Episodes int `json:"episodes"`
	} `json:"added"`
	NotFound struct {
		Movies   []json.RawMessage `json:"movies"`
		Shows    []json.RawMessage `json:"shows"`
		Episodes []json.RawMessage `json:"episodes"`
	} `json:"not_found"`
}

func (c *Client) AddHistory(ctx context.Context, token string, body *AddHistory) (*AddResult, error) {
	var r AddResult
	_, err := c.do(ctx, http.MethodPost, "/sync/history", token, body, &r)
	return &r, err
}

// IsAuth reports whether err means the token is no longer accepted.
func IsAuth(err error) bool {
	var se *statusError
	return errors.Is(err, ErrAuth) || (errors.As(err, &se) && se.code == http.StatusUnauthorized)
}
