package metadata

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/store"
)

// Titles for requests: what TMDB knows of movies and shows the server may
// not have, for the Requests page's rows, season picking, a person's other
// work and "because you watched". Answers are kept for an hour so busy
// pages don't spend the TMDB key's allowance.

// Requestable is a movie or show from TMDB.
type Requestable struct {
	Kind        string  `json:"kind"` // movie | show
	TMDBID      int64   `json:"tmdbId"`
	Title       string  `json:"title"`
	Year        int     `json:"year,omitempty"`
	Overview    string  `json:"overview,omitempty"`
	Poster      string  `json:"poster,omitempty"` // URL
	ReleaseDate string  `json:"releaseDate,omitempty"`
	Popularity  float64 `json:"-"`
	Role        string  `json:"role,omitempty"` // on a person's list: who they played, or their job
}

// SeasonInfo is one of a show's seasons on TMDB.
type SeasonInfo struct {
	Number   int    `json:"number"`
	Name     string `json:"name"`
	Episodes int    `json:"episodes"`
	AirDate  string `json:"airDate,omitempty"`
}

// TitleInfo is a movie or show with its seasons and whether it's out yet.
type TitleInfo struct {
	Requestable
	Backdrop string       `json:"backdrop,omitempty"`
	Status   string       `json:"status,omitempty"` // TMDB's: Released, In Production, Returning Series, Ended…
	Seasons  []SeasonInfo `json:"seasons,omitempty"`
}

type cached struct {
	at  time.Time
	val any
}

var (
	reqCacheMu sync.Mutex
	reqCache   = map[string]cached{}
)

const reqCacheTTL = time.Hour

func remember[T any](key string, fetch func() (T, error)) (T, error) {
	reqCacheMu.Lock()
	if c, ok := reqCache[key]; ok && time.Since(c.at) < reqCacheTTL {
		reqCacheMu.Unlock()
		return c.val.(T), nil
	}
	reqCacheMu.Unlock()
	v, err := fetch()
	if err != nil {
		return v, err
	}
	reqCacheMu.Lock()
	if len(reqCache) > 2000 {
		reqCache = map[string]cached{}
	}
	reqCache[key] = cached{time.Now(), v}
	reqCacheMu.Unlock()
	return v, nil
}

// tmdbListEntry is a title in one of TMDB's lists (trending, upcoming,
// recommendations, a person's credits).
type tmdbListEntry struct {
	ID           int64   `json:"id"`
	MediaType    string  `json:"media_type"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	Popularity   float64 `json:"popularity"`
	Adult        bool    `json:"adult"`
	Character    string  `json:"character"`
	Job          string  `json:"job"`
	GenreIDs     []int   `json:"genre_ids"`
}

func (e tmdbListEntry) requestable(kind string) (Requestable, bool) {
	if e.MediaType == "movie" {
		kind = "movie"
	} else if e.MediaType == "tv" {
		kind = "show"
	} else if e.MediaType != "" {
		return Requestable{}, false // people
	}
	if e.ID <= 0 || e.Adult {
		return Requestable{}, false
	}
	title, date := e.Title, e.ReleaseDate
	if kind == "show" {
		title, date = e.Name, e.FirstAirDate
	}
	if title == "" {
		return Requestable{}, false
	}
	role := e.Character
	if role == "" {
		role = e.Job
	}
	return Requestable{Kind: kind, TMDBID: e.ID, Title: title, Year: yearOf(date, 0), Overview: e.Overview,
		Poster: store.ImageURL(img(e.PosterPath)), ReleaseDate: date, Popularity: e.Popularity, Role: role}, true
}

func (t *TMDB) list(ctx context.Context, path, kind string, q url.Values) ([]Requestable, error) {
	var res struct {
		Results []tmdbListEntry `json:"results"`
	}
	if err := t.get(ctx, path, q, &res); err != nil {
		return nil, err
	}
	out := []Requestable{}
	for _, e := range res.Results {
		if r, ok := e.requestable(kind); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// DiscoverLists are the rows the Requests page offers.
var DiscoverLists = map[string]struct {
	Path, Kind string
}{
	"trending":        {"/trending/all/week", ""},
	"popular-movies":  {"/movie/popular", "movie"},
	"popular-shows":   {"/tv/popular", "show"},
	"upcoming-movies": {"/movie/upcoming", "movie"},
	"upcoming-shows":  {"/tv/on_the_air", "show"},
}

// Discover lists one of DiscoverLists.
func (s *Service) Discover(ctx context.Context, list string) ([]Requestable, error) {
	c := s.client()
	if c == nil {
		return nil, ErrNoKey
	}
	l, ok := DiscoverLists[list]
	if !ok {
		return nil, fmt.Errorf("no list called %q", list)
	}
	return remember("discover:"+list+":"+c.lang, func() ([]Requestable, error) {
		out, err := c.list(ctx, l.Path, l.Kind, nil)
		if err != nil || !strings.HasPrefix(list, "upcoming") {
			return out, err
		}
		// "Upcoming" on TMDB includes what came out last week; keep what's
		// still to come or just out, soonest first.
		cut := time.Now().AddDate(0, 0, -14).Format("2006-01-02")
		kept := out[:0]
		for _, r := range out {
			if r.ReleaseDate >= cut {
				kept = append(kept, r)
			}
		}
		sort.SliceStable(kept, func(i, j int) bool { return kept[i].ReleaseDate < kept[j].ReleaseDate })
		return kept, nil
	})
}

// Recommendations are TMDB's "if you liked this" titles for a movie or show.
func (s *Service) Recommendations(ctx context.Context, kind string, id int64) ([]Requestable, error) {
	c := s.client()
	if c == nil {
		return nil, ErrNoKey
	}
	path := fmt.Sprintf("/movie/%d/recommendations", id)
	if kind == "show" {
		path = fmt.Sprintf("/tv/%d/recommendations", id)
	}
	return remember("recs:"+path+":"+c.lang, func() ([]Requestable, error) { return c.list(ctx, path, kind, nil) })
}

// PersonCredits finds a person by name and lists the movies and shows they
// were in or made, most popular first.
func (s *Service) PersonCredits(ctx context.Context, name string) ([]Requestable, error) {
	c := s.client()
	if c == nil {
		return nil, ErrNoKey
	}
	name = strings.TrimSpace(name)
	return remember("person:"+strings.ToLower(name)+":"+c.lang, func() ([]Requestable, error) {
		id, err := c.findPerson(ctx, name)
		if err != nil {
			return nil, err
		}
		if id == 0 {
			return []Requestable{}, nil
		}
		var credits struct {
			Cast []tmdbListEntry `json:"cast"`
			Crew []tmdbListEntry `json:"crew"`
		}
		if err := c.get(ctx, fmt.Sprintf("/person/%d/combined_credits", id), nil, &credits); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		out := []Requestable{}
		for _, e := range append(credits.Cast, credits.Crew...) {
			r, ok := e.requestable("")
			// Talk shows and award shows list everyone who ever sat on the couch.
			if !ok || seen[r.Kind+fmt.Sprint(r.TMDBID)] || (r.Kind == "show" && slicesHas(e.GenreIDs, 10767, 10763)) {
				continue
			}
			seen[r.Kind+fmt.Sprint(r.TMDBID)] = true
			out = append(out, r)
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Popularity > out[j].Popularity })
		if len(out) > 60 {
			out = out[:60]
		}
		return out, nil
	})
}

func slicesHas(list []int, any ...int) bool {
	for _, v := range list {
		for _, a := range any {
			if v == a {
				return true
			}
		}
	}
	return false
}

// TitleInfo is a movie or show with its seasons, for picking seasons to
// request and noticing new ones.
func (s *Service) TitleInfo(ctx context.Context, kind string, id int64) (*TitleInfo, error) {
	c := s.client()
	if c == nil {
		return nil, ErrNoKey
	}
	return remember(fmt.Sprintf("info:%s:%d:%s", kind, id, c.lang), func() (*TitleInfo, error) {
		if kind == "movie" {
			var m struct {
				Title        string `json:"title"`
				Overview     string `json:"overview"`
				PosterPath   string `json:"poster_path"`
				BackdropPath string `json:"backdrop_path"`
				ReleaseDate  string `json:"release_date"`
				Status       string `json:"status"`
			}
			if err := c.get(ctx, fmt.Sprintf("/movie/%d", id), nil, &m); err != nil {
				return nil, err
			}
			return &TitleInfo{Requestable: Requestable{Kind: kind, TMDBID: id, Title: m.Title, Year: yearOf(m.ReleaseDate, 0),
				Overview: m.Overview, Poster: store.ImageURL(img(m.PosterPath)), ReleaseDate: m.ReleaseDate},
				Backdrop: store.ImageURL(img(m.BackdropPath)), Status: m.Status}, nil
		}
		var sh struct {
			Name         string `json:"name"`
			Overview     string `json:"overview"`
			PosterPath   string `json:"poster_path"`
			BackdropPath string `json:"backdrop_path"`
			FirstAirDate string `json:"first_air_date"`
			Status       string `json:"status"`
			Seasons      []struct {
				SeasonNumber int    `json:"season_number"`
				Name         string `json:"name"`
				EpisodeCount int    `json:"episode_count"`
				AirDate      string `json:"air_date"`
			} `json:"seasons"`
		}
		if err := c.get(ctx, fmt.Sprintf("/tv/%d", id), nil, &sh); err != nil {
			return nil, err
		}
		info := &TitleInfo{Requestable: Requestable{Kind: kind, TMDBID: id, Title: sh.Name, Year: yearOf(sh.FirstAirDate, 0),
			Overview: sh.Overview, Poster: store.ImageURL(img(sh.PosterPath)), ReleaseDate: sh.FirstAirDate},
			Backdrop: store.ImageURL(img(sh.BackdropPath)), Status: sh.Status, Seasons: []SeasonInfo{}}
		for _, se := range sh.Seasons {
			if se.SeasonNumber <= 0 { // specials
				continue
			}
			info.Seasons = append(info.Seasons, SeasonInfo{Number: se.SeasonNumber, Name: se.Name, Episodes: se.EpisodeCount, AirDate: se.AirDate})
		}
		return info, nil
	})
}

// Filmography is a person's page on TMDB: who they are and every movie and
// show that credits them, newest first.
type Filmography struct {
	TMDBID     int64            `json:"tmdbId"`
	Name       string           `json:"name"`
	Photo      string           `json:"photo,omitempty"`
	Biography  string           `json:"biography,omitempty"`
	Birthday   string           `json:"birthday,omitempty"`
	Deathday   string           `json:"deathday,omitempty"`
	Birthplace string           `json:"birthplace,omitempty"`
	KnownFor   string           `json:"knownFor,omitempty"` // TMDB's department: Acting, Directing…
	Credits    []FilmographyRow `json:"credits"`
}

// FilmographyRow is one title on a person's filmography.
type FilmographyRow struct {
	Requestable
	Crew     bool `json:"crew,omitempty"`     // credited behind the camera only (Role is the job)
	Episodes int  `json:"episodes,omitempty"` // on a show: how many episodes they were in
}

// maxFilmography caps a filmography; voice actors and character actors
// can have hundreds of credits.
const maxFilmography = 250

// findPerson is TMDB's id for a cast or crew member by name (credits carry
// no ids), the most popular exact match; 0 when there's none.
func (t *TMDB) findPerson(ctx context.Context, name string) (int64, error) {
	var found struct {
		Results []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := t.get(ctx, "/search/person", url.Values{"query": {name}, "include_adult": {"false"}}, &found); err != nil {
		return 0, err
	}
	for _, p := range found.Results {
		if strings.EqualFold(p.Name, name) {
			return p.ID, nil
		}
	}
	return 0, nil
}

// PersonFilmography looks up a cast or crew member by name on TMDB: nil
// (and no error) when TMDB has no one by that name.
func (s *Service) PersonFilmography(ctx context.Context, name string) (*Filmography, error) {
	c := s.client()
	if c == nil {
		return nil, ErrNoKey
	}
	name = strings.TrimSpace(name)
	return remember("filmography:"+strings.ToLower(name)+":"+c.lang, func() (*Filmography, error) {
		id, err := c.findPerson(ctx, name)
		if err != nil || id == 0 {
			return nil, err
		}
		var p struct {
			ID              int64  `json:"id"`
			Name            string `json:"name"`
			Biography       string `json:"biography"`
			Birthday        string `json:"birthday"`
			Deathday        string `json:"deathday"`
			PlaceOfBirth    string `json:"place_of_birth"`
			ProfilePath     string `json:"profile_path"`
			KnownFor        string `json:"known_for_department"`
			CombinedCredits struct {
				Cast []tmdbCreditEntry `json:"cast"`
				Crew []tmdbCreditEntry `json:"crew"`
			} `json:"combined_credits"`
		}
		if err := c.get(ctx, fmt.Sprintf("/person/%d", id), url.Values{"append_to_response": {"combined_credits"}}, &p); err != nil {
			return nil, err
		}
		f := &Filmography{TMDBID: p.ID, Name: p.Name, Photo: store.ImageURL(img(p.ProfilePath)), Biography: strings.TrimSpace(p.Biography),
			Birthday: p.Birthday, Deathday: p.Deathday, Birthplace: p.PlaceOfBirth, KnownFor: p.KnownFor, Credits: []FilmographyRow{}}
		if f.Name == "" {
			f.Name = name
		}
		at := map[string]int{} // kind:id → index in f.Credits
		add := func(e tmdbCreditEntry, crew bool) {
			r, ok := e.requestable("")
			// Talk shows, news and award shows list everyone who ever sat on the couch.
			if !ok || (r.Kind == "show" && slicesHas(e.GenreIDs, 10767, 10763)) {
				return
			}
			key := r.Kind + ":" + fmt.Sprint(r.TMDBID)
			if i, seen := at[key]; seen {
				// Acted in it and directed it, or played two parts: one row.
				row := &f.Credits[i]
				if r.Role != "" && !strings.Contains(row.Role, r.Role) && (crew == row.Crew || !crew) {
					row.Role = strings.TrimPrefix(row.Role+", "+r.Role, ", ")
				}
				row.Episodes = max(row.Episodes, e.EpisodeCount)
				return
			}
			at[key] = len(f.Credits)
			f.Credits = append(f.Credits, FilmographyRow{Requestable: r, Crew: crew, Episodes: e.EpisodeCount})
		}
		for _, e := range p.CombinedCredits.Cast {
			add(e, false)
		}
		for _, e := range p.CombinedCredits.Crew {
			add(e, true)
		}
		// Newest first; titles with no date yet are announced ones, so they go on top.
		sort.SliceStable(f.Credits, func(i, j int) bool {
			a, b := f.Credits[i].ReleaseDate, f.Credits[j].ReleaseDate
			if (a == "") != (b == "") {
				return a == ""
			}
			if a != b {
				return a > b
			}
			return f.Credits[i].Popularity > f.Credits[j].Popularity
		})
		if len(f.Credits) > maxFilmography {
			// Keep the best known ones, still newest first.
			keep := append([]FilmographyRow(nil), f.Credits...)
			sort.SliceStable(keep, func(i, j int) bool { return keep[i].Popularity > keep[j].Popularity })
			cut := keep[maxFilmography-1].Popularity
			kept := f.Credits[:0]
			for _, r := range f.Credits {
				if r.Popularity >= cut && len(kept) < maxFilmography {
					kept = append(kept, r)
				}
			}
			f.Credits = kept
		}
		return f, nil
	})
}

// tmdbCreditEntry is a title on a person's combined credits.
type tmdbCreditEntry struct {
	tmdbListEntry
	EpisodeCount int `json:"episode_count"`
}
