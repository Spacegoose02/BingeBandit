package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"bingebandit/internal/store"
)

// Ratings providers. Both have free API keys (about 1,000 lookups a day).
// They're variables so tests can point them at a local server.
var (
	mdblistBase = "https://api.mdblist.com"
	omdbBase    = "https://www.omdbapi.com"
)

// ErrRatingsAuth means the provider refused the key or the daily limit was
// reached; there's no point trying more items now.
var ErrRatingsAuth = errors.New("the ratings provider refused the API key or the daily limit was reached")

var ratingsHTTP = &http.Client{Timeout: 15 * time.Second}

// RatingsEnabled reports whether a ratings provider and key are set.
func (s *Service) RatingsEnabled() bool {
	p := s.st.Setting(store.SettingRatingsProvider)
	return (p == "mdblist" || p == "omdb") && strings.TrimSpace(s.st.Setting(store.SettingRatingsKey)) != ""
}

// RefreshRatings looks up IMDb, Rotten Tomatoes and Metacritic scores for a
// movie or show and stores them (an empty result too, so it isn't retried
// right away).
func (s *Service) RefreshRatings(ctx context.Context, t store.RatingTarget) error {
	key := strings.TrimSpace(s.st.Setting(store.SettingRatingsKey))
	var r *store.ExternalRatings
	var err error
	switch s.st.Setting(store.SettingRatingsProvider) {
	case "mdblist":
		r, err = mdblistRatings(ctx, key, t)
	case "omdb":
		imdbID := ""
		if old, err := s.st.ExternalRatings(t.ID); err == nil {
			imdbID = old.IMDbID
		}
		if imdbID == "" {
			if imdbID, err = s.imdbID(ctx, t); err != nil {
				return err
			}
		}
		r, err = omdbRatings(ctx, key, imdbID)
	default:
		return errors.New("no ratings provider is set")
	}
	if err != nil {
		return err
	}
	r.FetchedAt = time.Now().Unix()
	return s.st.SaveExternalRatings(t.ID, r)
}

// imdbID asks TMDB for the IMDb id (OMDb looks titles up by it).
func (s *Service) imdbID(ctx context.Context, t store.RatingTarget) (string, error) {
	c := s.client()
	if c == nil {
		return "", errors.New("OMDb needs a TMDB key to find IMDb ids")
	}
	kind := "movie"
	if t.Kind == "show" {
		kind = "tv"
	}
	var ids struct {
		IMDbID string `json:"imdb_id"`
	}
	if err := c.get(ctx, fmt.Sprintf("/%s/%d/external_ids", kind, t.TMDBID), nil, &ids); err != nil {
		return "", err
	}
	return ids.IMDbID, nil
}

func getRatingsJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := ratingsHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return ErrRatingsAuth
	case resp.StatusCode == http.StatusNotFound:
		return nil // unknown title: leave out empty
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("ratings provider answered %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// mdblistRatings looks the title up by its TMDB id.
func mdblistRatings(ctx context.Context, key string, t store.RatingTarget) (*store.ExternalRatings, error) {
	kind := "movie"
	if t.Kind == "show" {
		kind = "show"
	}
	var res struct {
		IDs struct {
			IMDb string `json:"imdb"`
		} `json:"ids"`
		Ratings []struct {
			Source string   `json:"source"`
			Value  *float64 `json:"value"`
			Votes  *int     `json:"votes"`
		} `json:"ratings"`
		Error string `json:"error"`
	}
	u := fmt.Sprintf("%s/tmdb/%s/%d?apikey=%s", mdblistBase, kind, t.TMDBID, url.QueryEscape(key))
	if err := getRatingsJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(res.Error), "api key") {
		return nil, ErrRatingsAuth
	}
	r := &store.ExternalRatings{IMDbID: res.IDs.IMDb}
	for _, x := range res.Ratings {
		if x.Value == nil || *x.Value <= 0 {
			continue
		}
		v := *x.Value
		switch x.Source {
		case "imdb":
			r.IMDb = v
			if x.Votes != nil {
				r.IMDbVotes = *x.Votes
			}
		case "tomatoes":
			r.RT = int(v + 0.5)
		case "popcorn", "tomatoesaudience":
			r.RTAudience = int(v + 0.5)
		case "metacritic":
			r.Metacritic = int(v + 0.5)
		}
	}
	return r, nil
}

// omdbRatings looks the title up by its IMDb id.
func omdbRatings(ctx context.Context, key, imdbID string) (*store.ExternalRatings, error) {
	r := &store.ExternalRatings{IMDbID: imdbID}
	if imdbID == "" {
		return r, nil // nothing to look up with
	}
	var res struct {
		Response   string `json:"Response"`
		Error      string `json:"Error"`
		IMDbRating string `json:"imdbRating"`
		IMDbVotes  string `json:"imdbVotes"`
		Metascore  string `json:"Metascore"`
		Ratings    []struct {
			Source string `json:"Source"`
			Value  string `json:"Value"`
		} `json:"Ratings"`
	}
	u := fmt.Sprintf("%s/?i=%s&apikey=%s", omdbBase, url.QueryEscape(imdbID), url.QueryEscape(key))
	if err := getRatingsJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	if res.Response == "False" {
		if e := strings.ToLower(res.Error); strings.Contains(e, "api key") || strings.Contains(e, "limit") {
			return nil, ErrRatingsAuth
		}
		return r, nil // not found
	}
	r.IMDb, _ = strconv.ParseFloat(res.IMDbRating, 64)
	r.IMDbVotes, _ = strconv.Atoi(strings.ReplaceAll(res.IMDbVotes, ",", ""))
	r.Metacritic, _ = strconv.Atoi(res.Metascore)
	for _, x := range res.Ratings {
		if x.Source == "Rotten Tomatoes" {
			r.RT, _ = strconv.Atoi(strings.TrimSuffix(x.Value, "%"))
		}
	}
	return r, nil
}
