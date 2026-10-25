package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func (c *Cinemask) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.Key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Cinemask: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("Cinemask didn't accept the API key")
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("Cinemask answered %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(out)
}

type cinemaskCollection struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
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
		out := Collection{Name: col.Name, Summary: col.Description}
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
