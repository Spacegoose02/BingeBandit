package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Cinemask brings over the collections kept in Cinemask (Michael's
// Sonarr/Radarr-style manager), which lists each title by TMDB id, so they
// match exactly. It has no history.
type Cinemask struct {
	BaseURL string
	Key     string
	client  *http.Client
}

func NewCinemask(baseURL, key string) *Cinemask {
	return &Cinemask{BaseURL: strings.TrimRight(baseURL, "/"), Key: key, client: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Cinemask) Kind() string { return "cinemask" }

func (c *Cinemask) open(ctx context.Context, path string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.Key)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach Cinemask: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return resp.Body, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		err = fmt.Errorf("Cinemask didn't accept the API key")
	case resp.StatusCode == http.StatusNotFound:
		err = errNotFound
	default:
		err = fmt.Errorf("Cinemask answered %s", resp.Status)
	}
	resp.Body.Close()
	return nil, err
}

func (c *Cinemask) get(ctx context.Context, path string, out any) error {
	body, err := c.open(ctx, path)
	if err != nil {
		return err
	}
	defer body.Close()
	return json.NewDecoder(io.LimitReader(body, 64<<20)).Decode(out)
}

// Image fetches a collection poster. Cinemask keeps TMDB paths (turned into
// tmdb:/ references before this), https links, and covers on Plex, Jellyfin
// or its own disk, which it serves at /api/images.
func (c *Cinemask) Image(ctx context.Context, ref string) (io.ReadCloser, error) {
	if strings.HasPrefix(ref, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
		}
		return resp.Body, nil
	}
	return c.open(ctx, "/api/images?src="+url.QueryEscape(ref))
}

var tmdbPoster = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(jpe?g|png|webp)$`)

type cinemaskCollection struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Poster      string `json:"poster"`    // TMDB path, https link, or plex:/jellyfin:/local: image
	PlexLabel   string `json:"plexLabel"` // put on every member in Plex
	FromLabel   string `json:"fromLabel"` // members are everything with this label
	Items       []struct {
		Kind   string `json:"kind"` // movie | show
		TMDBID int64  `json:"tmdbId"`
		Title  string `json:"title"`
		Year   int    `json:"year"`
	} `json:"items"`
}

// Users checks the address and key; Cinemask has no history to map.
func (c *Cinemask) Users(ctx context.Context) ([]SourceUser, error) {
	var list []cinemaskCollection
	if err := c.get(ctx, "/api/collections", &list); err != nil {
		return nil, err
	}
	return []SourceUser{}, nil
}

func (c *Cinemask) Records(ctx context.Context, state bool, emit func(Record) error, progress func(string, int, int)) error {
	return nil
}

func (c *Cinemask) Collections(ctx context.Context, emit func(Collection) error) error {
	var list []cinemaskCollection
	if err := c.get(ctx, "/api/collections", &list); err != nil {
		return err
	}
	for _, sum := range list {
		var col cinemaskCollection
		if err := c.get(ctx, "/api/collections/"+strconv.FormatInt(sum.ID, 10), &col); err != nil {
			return err
		}
		out := Collection{Name: col.Name, Summary: col.Description, Poster: col.Poster, Label: col.FromLabel, ItemLabel: col.PlexLabel}
		if tmdbPoster.MatchString(col.Poster) {
			out.Poster = "tmdb:" + col.Poster
		}
		for _, it := range col.Items {
			if it.Kind != "movie" && it.Kind != "show" {
				continue
			}
			out.Items = append(out.Items, Record{Key: fmt.Sprintf("cinemask:%s:%d", it.Kind, it.TMDBID), Kind: it.Kind, Title: it.Title, Year: it.Year, TMDB: it.TMDBID})
		}
		if err := emit(out); err != nil {
			return err
		}
	}
	return nil
}
