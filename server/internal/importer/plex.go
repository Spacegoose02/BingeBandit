package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Plex reads a Plex Media Server through its HTTP API with the owner's
// X-Plex-Token (Plex Web → any item → Get Info → View XML shows it in the URL).
type Plex struct {
	BaseURL string
	Token   string
	client  *http.Client

	meta  map[string]*plexMeta // ratingKey → details, cached
	owner string
	users map[int]string // accountID → name
}

func NewPlex(baseURL, token string) *Plex {
	return &Plex{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, client: &http.Client{Timeout: 30 * time.Second}, meta: map[string]*plexMeta{}}
}

func (p *Plex) Kind() string { return "plex" }

type plexGuid struct {
	ID string `json:"id"`
}

type plexPart struct {
	File string `json:"file"`
}

type plexMedia struct {
	Part []plexPart `json:"Part"`
}

type plexMeta struct {
	RatingKey            string      `json:"ratingKey"`
	Key                  string      `json:"key"`
	Type                 string      `json:"type"`
	Title                string      `json:"title"`
	Summary              string      `json:"summary"`
	Thumb                string      `json:"thumb"`
	Year                 int         `json:"year"`
	GrandparentTitle     string      `json:"grandparentTitle"`
	GrandparentRatingKey string      `json:"grandparentRatingKey"`
	GrandparentKey       string      `json:"grandparentKey"`
	ParentIndex          int         `json:"parentIndex"`
	Index                int         `json:"index"`
	Duration             int64       `json:"duration"` // ms
	ViewCount            int         `json:"viewCount"`
	ViewOffset           int64       `json:"viewOffset"` // ms
	LastViewedAt         int64       `json:"lastViewedAt"`
	ViewedAt             int64       `json:"viewedAt"`
	AccountID            int         `json:"accountID"`
	HistoryKey           string      `json:"historyKey"`
	Guid                 []plexGuid  `json:"Guid"`
	Media                []plexMedia `json:"Media"`
}

type plexContainer struct {
	MediaContainer struct {
		Size      int        `json:"size"`
		TotalSize int        `json:"totalSize"`
		Metadata  []plexMeta `json:"Metadata"`
		Directory []struct {
			Key   string `json:"key"`
			Title string `json:"title"`
			Type  string `json:"type"`
		} `json:"Directory"`
		Account []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"Account"`
	} `json:"MediaContainer"`
}

var errPlexAuth = errors.New("Plex refused the token; copy a fresh one from Plex Web (Get Info → View XML)")

func (p *Plex) get(ctx context.Context, path string, q url.Values) (*plexContainer, error) {
	if q == nil {
		q = url.Values{}
	}
	u := p.BaseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", p.Token)
	req.Header.Set("X-Plex-Client-Identifier", "bingebandit-importer")
	req.Header.Set("X-Plex-Product", "BingeBandit")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach Plex at %s: %w", p.BaseURL, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errPlexAuth
	case resp.StatusCode == http.StatusNotFound:
		return nil, errNotFound
	case resp.StatusCode >= 300:
		return nil, fmt.Errorf("Plex answered %s for %s", resp.Status, path)
	}
	var c plexContainer
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, fmt.Errorf("Plex sent something that isn't JSON for %s: %w", path, err)
	}
	return &c, nil
}

var errNotFound = errors.New("not found")

// Users lists the server's accounts (owner, managed and shared users).
func (p *Plex) Users(ctx context.Context) ([]SourceUser, error) {
	if err := p.loadUsers(ctx); err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(p.users))
	for id := range p.users {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids) // the owner (1) first
	var out []SourceUser
	for _, id := range ids {
		out = append(out, SourceUser{Name: p.users[id]})
	}
	return out, nil
}

func (p *Plex) loadUsers(ctx context.Context) error {
	if p.users != nil {
		return nil
	}
	c, err := p.get(ctx, "/accounts", nil)
	if err != nil {
		return err
	}
	p.users = map[int]string{}
	for _, a := range c.MediaContainer.Account {
		if a.Name == "" {
			continue
		}
		p.users[a.ID] = a.Name
		if a.ID == 1 { // Plex numbers the server owner 1
			p.owner = a.Name
		}
	}
	return nil
}

// details fetches an item's guids and file, cached; deleted items return nil.
func (p *Plex) details(ctx context.Context, ratingKey string) *plexMeta {
	if ratingKey == "" {
		return nil
	}
	if m, ok := p.meta[ratingKey]; ok {
		return m
	}
	c, err := p.get(ctx, "/library/metadata/"+ratingKey, url.Values{"includeGuids": {"1"}})
	var m *plexMeta
	if err == nil && len(c.MediaContainer.Metadata) > 0 {
		m = &c.MediaContainer.Metadata[0]
	}
	p.meta[ratingKey] = m
	return m
}

func guidTMDB(gs []plexGuid) int64 {
	for _, g := range gs {
		if v, ok := strings.CutPrefix(g.ID, "tmdb://"); ok {
			n, _ := strconv.ParseInt(v, 10, 64)
			return n
		}
	}
	return 0
}

func firstFile(ms []plexMedia) string {
	for _, m := range ms {
		for _, pt := range m.Part {
			if pt.File != "" {
				f := pt.File
				return f[strings.LastIndexAny(f, `/\`)+1:]
			}
		}
	}
	return ""
}

func ratingKeyFromKey(key string) string {
	return strings.TrimPrefix(key, "/library/metadata/")
}

// record turns a Plex item into a Record, enriched with guids and the file
// name when Plex still has the item.
func (p *Plex) record(ctx context.Context, m *plexMeta) Record {
	r := Record{Kind: m.Type, Title: m.Title, Year: m.Year, Duration: float64(m.Duration) / 1000}
	if d := p.details(ctx, m.RatingKey); d != nil {
		r.File = firstFile(d.Media)
		if r.Year == 0 {
			r.Year = d.Year
		}
		if r.Duration == 0 {
			r.Duration = float64(d.Duration) / 1000
		}
		if m.Type == "movie" || m.Type == "show" {
			r.TMDB = guidTMDB(d.Guid)
		}
		if m.GrandparentRatingKey == "" {
			m.GrandparentRatingKey = d.GrandparentRatingKey
		}
	} else if len(m.Media) > 0 {
		r.File = firstFile(m.Media)
	}
	if m.Type == "episode" {
		r.ShowTitle, r.Season, r.Episode = m.GrandparentTitle, m.ParentIndex, m.Index
		gk := m.GrandparentRatingKey
		if gk == "" {
			gk = ratingKeyFromKey(m.GrandparentKey)
		}
		if show := p.details(ctx, gk); show != nil {
			r.TMDB = guidTMDB(show.Guid)
			r.ShowYear = show.Year
			if r.ShowTitle == "" {
				r.ShowTitle = show.Title
			}
		}
	}
	return r
}

const plexPage = 200

// Records sends every play in the server's history (all accounts), and, with
// state, the owner's current watched marks and resume points.
func (p *Plex) Records(ctx context.Context, state bool, emit func(Record) error, progress func(string, int, int)) error {
	if err := p.loadUsers(ctx); err != nil {
		return err
	}
	for start := 0; ; start += plexPage {
		c, err := p.get(ctx, "/status/sessions/history/all", url.Values{
			"sort": {"viewedAt:desc"}, "X-Plex-Container-Start": {strconv.Itoa(start)}, "X-Plex-Container-Size": {strconv.Itoa(plexPage)},
		})
		if err != nil {
			return err
		}
		total := c.MediaContainer.TotalSize
		for i := range c.MediaContainer.Metadata {
			m := &c.MediaContainer.Metadata[i]
			progress("Reading Plex watch history", start+i+1, total)
			if m.Type != "movie" && m.Type != "episode" {
				continue
			}
			r := p.record(ctx, m)
			r.User = p.users[m.AccountID]
			if r.User == "" {
				r.User = "Plex account " + strconv.Itoa(m.AccountID)
			}
			r.Key = "plex:" + strings.TrimPrefix(m.HistoryKey, "/status/sessions/history/")
			if m.HistoryKey == "" {
				r.Key = fmt.Sprintf("plex:%d:%s:%d", m.AccountID, m.RatingKey, m.ViewedAt)
			}
			// Plex logs a history entry when an item counts as watched.
			r.ViewedAt, r.Completed = m.ViewedAt, true
			r.Played = int64(r.Duration)
			r.Client = "Plex"
			if err := emit(r); err != nil {
				return err
			}
		}
		if len(c.MediaContainer.Metadata) < plexPage || (total > 0 && start+plexPage >= total) {
			break
		}
	}
	if !state || p.owner == "" {
		return nil
	}
	return p.ownerState(ctx, emit, progress)
}

// ownerState reads viewCount/viewOffset from every movie and episode. Plex
// only reports these for the token's own account.
func (p *Plex) ownerState(ctx context.Context, emit func(Record) error, progress func(string, int, int)) error {
	secs, err := p.get(ctx, "/library/sections", nil)
	if err != nil {
		return err
	}
	for _, sec := range secs.MediaContainer.Directory {
		var typ string
		switch sec.Type {
		case "movie":
			typ = "1"
		case "show":
			typ = "4"
		default:
			continue
		}
		c, err := p.get(ctx, "/library/sections/"+sec.Key+"/all", url.Values{"type": {typ}, "includeGuids": {"1"}})
		if err != nil {
			return err
		}
		for i := range c.MediaContainer.Metadata {
			m := &c.MediaContainer.Metadata[i]
			progress("Reading watched state from "+sec.Title, i+1, len(c.MediaContainer.Metadata))
			if m.ViewCount == 0 && m.ViewOffset == 0 {
				continue
			}
			p.meta[m.RatingKey] = m // already has guids and files
			r := p.record(ctx, m)
			r.User, r.StateOnly = p.owner, true
			r.ViewedAt = m.LastViewedAt
			r.Completed = m.ViewCount > 0 && m.ViewOffset == 0
			r.Position = float64(m.ViewOffset) / 1000
			if err := emit(r); err != nil {
				return err
			}
		}
	}
	return nil
}

// Collections sends every collection in movie and show libraries.
func (p *Plex) Collections(ctx context.Context, emit func(Collection) error) error {
	secs, err := p.get(ctx, "/library/sections", nil)
	if err != nil {
		return err
	}
	for _, sec := range secs.MediaContainer.Directory {
		if sec.Type != "movie" && sec.Type != "show" {
			continue
		}
		cols, err := p.get(ctx, "/library/sections/"+sec.Key+"/collections", nil)
		if err != nil {
			if errors.Is(err, errNotFound) {
				continue
			}
			return err
		}
		for _, col := range cols.MediaContainer.Metadata {
			kids, err := p.get(ctx, "/library/collections/"+col.RatingKey+"/children", url.Values{"includeGuids": {"1"}})
			if err != nil {
				if ctx.Err() != nil {
					return err
				}
				// One collection Plex can't list (smart ones sometimes fail) shouldn't stop the rest.
				log.Printf("import (plex): collection %q: %v", col.Title, err)
				continue
			}
			c := Collection{Name: col.Title, Summary: col.Summary, Poster: plexImage(col.Thumb)}
			for i := range kids.MediaContainer.Metadata {
				m := &kids.MediaContainer.Metadata[i]
				p.meta[m.RatingKey] = m
				c.Items = append(c.Items, p.record(ctx, m))
			}
			if err := emit(c); err != nil {
				return err
			}
		}
	}
	return nil
}

// plexImage keeps image paths on the Plex server, which Image fetches. Plex
// makes /composite/ covers from the first few posters; those are kept too, as
// they're what Plex shows.
func plexImage(path string) string {
	if strings.HasPrefix(path, "/library/") {
		return "plex:" + path
	}
	return ""
}

// Image fetches artwork from Plex, such as a collection's poster.
func (p *Plex) Image(ctx context.Context, ref string) (io.ReadCloser, error) {
	path, ok := strings.CutPrefix(ref, "plex:")
	if !ok {
		return nil, fmt.Errorf("not a Plex image: %s", ref)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Plex-Token", p.Token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("Plex answered %s for %s", resp.Status, path)
	}
	return resp.Body, nil
}

// Labels sends every label in movie and show libraries with the titles that
// carry it. Kometa and Cinemask put their labels here.
func (p *Plex) Labels(ctx context.Context, emit func(LabelSet) error) error {
	secs, err := p.get(ctx, "/library/sections", nil)
	if err != nil {
		return err
	}
	for _, sec := range secs.MediaContainer.Directory {
		if sec.Type != "movie" && sec.Type != "show" {
			continue
		}
		labels, err := p.get(ctx, "/library/sections/"+sec.Key+"/label", nil)
		if err != nil {
			if errors.Is(err, errNotFound) {
				continue
			}
			return err
		}
		for _, l := range labels.MediaContainer.Directory {
			items, err := p.get(ctx, "/library/sections/"+sec.Key+"/all", url.Values{"label": {l.Key}, "includeGuids": {"1"}})
			if err != nil {
				if ctx.Err() != nil {
					return err
				}
				log.Printf("import (plex): label %q: %v", l.Title, err)
				continue
			}
			set := LabelSet{Name: l.Title}
			for i := range items.MediaContainer.Metadata {
				m := &items.MediaContainer.Metadata[i]
				p.meta[m.RatingKey] = m
				set.Items = append(set.Items, p.record(ctx, m))
			}
			if err := emit(set); err != nil {
				return err
			}
		}
	}
	return nil
}
