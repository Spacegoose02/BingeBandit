// Package metadata fetches titles, artwork, credits and ratings from TMDB.
package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// tmdbBase is a variable so tests can point it elsewhere.
var tmdbBase = "https://api.themoviedb.org/3"

type TMDB struct {
	key  string
	lang string
	hc   *http.Client
}

func NewTMDB(key, lang string) *TMDB {
	return &TMDB{key: strings.TrimSpace(key), lang: lang, hc: &http.Client{Timeout: 20 * time.Second}}
}

func (t *TMDB) get(ctx context.Context, path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("language", t.lang)
	// v4 read-access tokens are JWTs; v3 keys are 32 hex chars.
	bearer := strings.HasPrefix(t.key, "eyJ")
	if !bearer {
		q.Set("api_key", t.key)
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, tmdbBase+path+"?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		if bearer {
			req.Header.Set("Authorization", "Bearer "+t.key)
		}
		resp, err := t.hc.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			resp.Body.Close()
			wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			select {
			case <-time.After(time.Duration(max(wait, 1)) * time.Second):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("tmdb %s: %s", path, resp.Status)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
}

type tmdbSearch struct {
	Results []struct {
		ID           int64   `json:"id"`
		Title        string  `json:"title"`
		Name         string  `json:"name"`
		ReleaseDate  string  `json:"release_date"`
		FirstAirDate string  `json:"first_air_date"`
		Popularity   float64 `json:"popularity"`
	} `json:"results"`
}

type tmdbCredits struct {
	Cast []struct {
		Name        string `json:"name"`
		Character   string `json:"character"`
		ProfilePath string `json:"profile_path"`
		Order       int    `json:"order"`
	} `json:"cast"`
	Crew []struct {
		Name        string `json:"name"`
		Job         string `json:"job"`
		ProfilePath string `json:"profile_path"`
	} `json:"crew"`
}

type tmdbImages struct {
	Logos []struct {
		FilePath string `json:"file_path"`
		Lang     string `json:"iso_639_1"`
	} `json:"logos"`
}

type tmdbVideos struct {
	Results []struct {
		Key      string `json:"key"`
		Site     string `json:"site"`
		Type     string `json:"type"`
		Official bool   `json:"official"`
	} `json:"results"`
}

type named struct {
	Name string `json:"name"`
}

type tmdbMovie struct {
	ID                  int64   `json:"id"`
	IMDbID              string  `json:"imdb_id"`
	Title               string  `json:"title"`
	OriginalTitle       string  `json:"original_title"`
	Overview            string  `json:"overview"`
	Tagline             string  `json:"tagline"`
	PosterPath          string  `json:"poster_path"`
	BackdropPath        string  `json:"backdrop_path"`
	VoteAverage         float64 `json:"vote_average"`
	Runtime             int     `json:"runtime"`
	ReleaseDate         string  `json:"release_date"`
	Genres              []named `json:"genres"`
	ProductionCompanies []named `json:"production_companies"`
	BelongsToCollection *struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		PosterPath   string `json:"poster_path"`
		BackdropPath string `json:"backdrop_path"`
	} `json:"belongs_to_collection"`
	Credits      tmdbCredits `json:"credits"`
	Images       tmdbImages  `json:"images"`
	Videos       tmdbVideos  `json:"videos"`
	ReleaseDates struct {
		Results []struct {
			Country      string `json:"iso_3166_1"`
			ReleaseDates []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
}

type tmdbShow struct {
	ID             int64       `json:"id"`
	Name           string      `json:"name"`
	OriginalName   string      `json:"original_name"`
	Overview       string      `json:"overview"`
	Tagline        string      `json:"tagline"`
	PosterPath     string      `json:"poster_path"`
	BackdropPath   string      `json:"backdrop_path"`
	VoteAverage    float64     `json:"vote_average"`
	FirstAirDate   string      `json:"first_air_date"`
	EpisodeRunTime []int       `json:"episode_run_time"`
	Genres         []named     `json:"genres"`
	Networks       []named     `json:"networks"`
	Credits        tmdbCredits `json:"-"` // filled from aggregate_credits by UnmarshalJSON
	Images         tmdbImages  `json:"images"`
	Videos         tmdbVideos  `json:"videos"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
	ExternalIDs struct {
		IMDbID string `json:"imdb_id"`
		TVDBID int64  `json:"tvdb_id"`
	} `json:"external_ids"`
}

// aggregate_credits nests characters under "roles"; flatten it into tmdbCredits.
func (s *tmdbShow) UnmarshalJSON(b []byte) error {
	type plain tmdbShow
	var aux struct {
		*plain
		Agg struct {
			Cast []struct {
				Name        string `json:"name"`
				ProfilePath string `json:"profile_path"`
				Order       int    `json:"order"`
				Roles       []struct {
					Character string `json:"character"`
				} `json:"roles"`
			} `json:"cast"`
			Crew []struct {
				Name        string `json:"name"`
				ProfilePath string `json:"profile_path"`
				Jobs        []struct {
					Job string `json:"job"`
				} `json:"jobs"`
			} `json:"crew"`
		} `json:"aggregate_credits"`
	}
	aux.plain = (*plain)(s)
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	s.Credits = tmdbCredits{}
	for _, c := range aux.Agg.Cast {
		role := ""
		if len(c.Roles) > 0 {
			role = c.Roles[0].Character
		}
		s.Credits.Cast = append(s.Credits.Cast, struct {
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
			Order       int    `json:"order"`
		}{c.Name, role, c.ProfilePath, c.Order})
	}
	for _, c := range aux.Agg.Crew {
		for _, j := range c.Jobs {
			s.Credits.Crew = append(s.Credits.Crew, struct {
				Name        string `json:"name"`
				Job         string `json:"job"`
				ProfilePath string `json:"profile_path"`
			}{c.Name, j.Job, c.ProfilePath})
		}
	}
	return nil
}

type tmdbSeason struct {
	Name       string `json:"name"`
	Overview   string `json:"overview"`
	PosterPath string `json:"poster_path"`
	AirDate    string `json:"air_date"`
	Episodes   []struct {
		EpisodeNumber int     `json:"episode_number"`
		Name          string  `json:"name"`
		Overview      string  `json:"overview"`
		StillPath     string  `json:"still_path"`
		AirDate       string  `json:"air_date"`
		VoteAverage   float64 `json:"vote_average"`
		Runtime       int     `json:"runtime"`
	} `json:"episodes"`
}

func (t *TMDB) SearchMovie(ctx context.Context, title string, year int) (int64, error) {
	return t.search(ctx, "/search/movie", "year", title, year)
}

func (t *TMDB) SearchShow(ctx context.Context, title string, year int) (int64, error) {
	return t.search(ctx, "/search/tv", "first_air_date_year", title, year)
}

// search tries with the year first, then without, and takes the top hit.
func (t *TMDB) search(ctx context.Context, path, yearParam, title string, year int) (int64, error) {
	attempts := []int{year}
	if year != 0 {
		attempts = append(attempts, 0)
	}
	for _, y := range attempts {
		q := url.Values{"query": {title}, "include_adult": {"false"}}
		if y != 0 {
			q.Set(yearParam, strconv.Itoa(y))
		}
		var res tmdbSearch
		if err := t.get(ctx, path, q, &res); err != nil {
			return 0, err
		}
		if len(res.Results) > 0 {
			return res.Results[0].ID, nil
		}
	}
	return 0, nil
}

// Find looks a title up by another source's id: source is "imdb_id" or
// "tvdb_id". It returns the TMDB movie and show ids found (0 when none).
func (t *TMDB) Find(ctx context.Context, id, source string) (movie, show int64, err error) {
	var res struct {
		Movies []struct {
			ID int64 `json:"id"`
		} `json:"movie_results"`
		Shows []struct {
			ID int64 `json:"id"`
		} `json:"tv_results"`
	}
	if err := t.get(ctx, "/find/"+url.PathEscape(id), url.Values{"external_source": {source}}, &res); err != nil {
		return 0, 0, err
	}
	if len(res.Movies) > 0 {
		movie = res.Movies[0].ID
	}
	if len(res.Shows) > 0 {
		show = res.Shows[0].ID
	}
	return movie, show, nil
}

func (t *TMDB) Movie(ctx context.Context, id int64) (*tmdbMovie, error) {
	var m tmdbMovie
	q := url.Values{"append_to_response": {"credits,images,videos,release_dates"}, "include_image_language": {t.imageLang() + ",null"}}
	return &m, t.get(ctx, fmt.Sprintf("/movie/%d", id), q, &m)
}

func (t *TMDB) Show(ctx context.Context, id int64) (*tmdbShow, error) {
	var s tmdbShow
	q := url.Values{"append_to_response": {"aggregate_credits,images,videos,content_ratings,external_ids"}, "include_image_language": {t.imageLang() + ",null"}}
	return &s, t.get(ctx, fmt.Sprintf("/tv/%d", id), q, &s)
}

func (t *TMDB) Season(ctx context.Context, showID int64, season int) (*tmdbSeason, error) {
	var s tmdbSeason
	return &s, t.get(ctx, fmt.Sprintf("/tv/%d/season/%d", showID, season), nil, &s)
}

// SearchResult is one candidate for Fix Match.
type SearchResult struct {
	TMDBID   int64  `json:"tmdbId,omitempty"`
	TVDBID   int64  `json:"tvdbId,omitempty"`
	Title    string `json:"title"`
	Year     int    `json:"year,omitempty"`
	Overview string `json:"overview,omitempty"`
	Poster   string `json:"poster,omitempty"` // image ref, e.g. tmdb:/x.jpg, or a TheTVDB URL
}

// Search lists candidates for a movie ("movie") or show ("show").
func (t *TMDB) Search(ctx context.Context, kind, query string, year int) ([]SearchResult, error) {
	path, yearParam := "/search/movie", "year"
	if kind == "show" {
		path, yearParam = "/search/tv", "first_air_date_year"
	}
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if year != 0 {
		q.Set(yearParam, strconv.Itoa(year))
	}
	var res struct {
		Results []struct {
			ID           int64  `json:"id"`
			Title        string `json:"title"`
			Name         string `json:"name"`
			ReleaseDate  string `json:"release_date"`
			FirstAirDate string `json:"first_air_date"`
			Overview     string `json:"overview"`
			PosterPath   string `json:"poster_path"`
		} `json:"results"`
	}
	if err := t.get(ctx, path, q, &res); err != nil {
		return nil, err
	}
	out := []SearchResult{}
	for _, r := range res.Results {
		title, date := r.Title, r.ReleaseDate
		if kind == "show" {
			title, date = r.Name, r.FirstAirDate
		}
		out = append(out, SearchResult{TMDBID: r.ID, Title: title, Year: yearOf(date, 0), Overview: r.Overview, Poster: img(r.PosterPath)})
	}
	return out, nil
}

// ImageOption is one artwork choice offered in the artwork picker.
type ImageOption struct {
	Ref    string  `json:"ref"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Lang   string  `json:"lang,omitempty"`
	Votes  float64 `json:"votes"`
}

// Images lists posters, backdrops or logos for a "movie", "show" or
// "season" (seasons only have posters).
func (t *TMDB) Images(ctx context.Context, kind string, id int64, season int, imageType string) ([]ImageOption, error) {
	path := fmt.Sprintf("/movie/%d/images", id)
	switch kind {
	case "show":
		path = fmt.Sprintf("/tv/%d/images", id)
	case "season":
		path = fmt.Sprintf("/tv/%d/season/%d/images", id, season)
	}
	type tmdbImage struct {
		FilePath    string  `json:"file_path"`
		Width       int     `json:"width"`
		Height      int     `json:"height"`
		Lang        string  `json:"iso_639_1"`
		VoteAverage float64 `json:"vote_average"`
	}
	var res struct {
		Posters   []tmdbImage `json:"posters"`
		Backdrops []tmdbImage `json:"backdrops"`
		Logos     []tmdbImage `json:"logos"`
	}
	// language=null would hide textless art; ask for ours plus language-free images.
	q := url.Values{"include_image_language": {t.imageLang() + ",null"}}
	if err := t.get(ctx, path, q, &res); err != nil {
		return nil, err
	}
	list := res.Posters
	switch imageType {
	case "backdrop":
		list = res.Backdrops
	case "logo":
		list = res.Logos
	}
	out := []ImageOption{}
	for _, i := range list {
		out = append(out, ImageOption{Ref: "tmdb:" + i.FilePath, Width: i.Width, Height: i.Height, Lang: i.Lang, Votes: i.VoteAverage})
	}
	return out, nil
}

func (t *TMDB) imageLang() string {
	lang, _, _ := strings.Cut(t.lang, "-")
	return lang
}
