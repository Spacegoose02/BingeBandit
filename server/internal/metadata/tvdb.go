package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// tvdbBase is TheTVDB's v4 API; a variable so tests can point it elsewhere.
var tvdbBase = "https://api4.thetvdb.com/v4"

// TVDB episode orders a show can use. "official" is the aired order.
var TVDBOrders = []string{"official", "dvd", "absolute"}

// ValidTVDBOrder reports whether o is "" (aired) or a known order.
func ValidTVDBOrder(o string) bool {
	if o == "" {
		return true
	}
	for _, v := range TVDBOrders {
		if v == o {
			return true
		}
	}
	return false
}

// TVDB is a TheTVDB v4 client. Requests carry a bearer token from /login,
// which lasts a month; it is fetched on first use and again after a 401.
type TVDB struct {
	key, pin string
	lang     string // ISO 639-2, e.g. "eng"
	hc       *http.Client

	mu    sync.Mutex
	token string
}

func NewTVDB(key, pin, lang string) *TVDB {
	return &TVDB{key: strings.TrimSpace(key), pin: strings.TrimSpace(pin), lang: tvdbLang(lang), hc: &http.Client{Timeout: 20 * time.Second}}
}

// tvdbLang maps a TMDB-style language ("en-US") to TheTVDB's three-letter codes.
func tvdbLang(lang string) string {
	code := strings.ToLower(strings.SplitN(lang, "-", 2)[0])
	if l, ok := map[string]string{
		"en": "eng", "de": "deu", "fr": "fra", "es": "spa", "it": "ita", "nl": "nld", "pt": "por", "sv": "swe",
		"da": "dan", "no": "nor", "nb": "nor", "fi": "fin", "pl": "pol", "cs": "ces", "hu": "hun", "ru": "rus",
		"ja": "jpn", "ko": "kor", "zh": "zho", "tr": "tur", "el": "ell", "he": "heb", "ar": "ara", "hi": "hin",
	}[code]; ok {
		return l
	}
	return "eng"
}

var errTVDBAuth = errors.New("TheTVDB rejected the API key or PIN")

func (t *TVDB) login(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{"apikey": t.key, "pin": t.pin})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tvdbBase+"/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", errTVDBAuth
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tvdb login: %s", resp.Status)
	}
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Data.Token == "" {
		return "", errTVDBAuth
	}
	return out.Data.Token, nil
}

// get fetches path and decodes its "data" field into out.
func (t *TVDB) get(ctx context.Context, path string, q url.Values, out any) error {
	_, err := t.getPage(ctx, path, q, out)
	return err
}

// getPage is get for paged lists; more reports whether another page follows.
func (t *TVDB) getPage(ctx context.Context, path string, q url.Values, out any) (more bool, err error) {
	u := tvdbBase + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	for attempt := 0; ; attempt++ {
		t.mu.Lock()
		token := t.token
		t.mu.Unlock()
		if token == "" {
			var err error
			if token, err = t.login(ctx); err != nil {
				return false, err
			}
			t.mu.Lock()
			t.token = token
			t.mu.Unlock()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return false, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := t.hc.Do(req)
		if err != nil {
			return false, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			t.mu.Lock()
			t.token = ""
			t.mu.Unlock()
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return false, errNotFound
		}
		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("tvdb %s: %s", path, resp.Status)
		}
		var env struct {
			Data  any `json:"data"`
			Links struct {
				Next *string `json:"next"`
			} `json:"links"`
		}
		env.Data = out
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			return false, err
		}
		return env.Links.Next != nil && *env.Links.Next != "", nil
	}
}

var errNotFound = errors.New("not found")

// Search lists series matching query.
func (t *TVDB) Search(ctx context.Context, query string, year int) ([]SearchResult, error) {
	q := url.Values{"query": {query}, "type": {"series"}, "limit": {"20"}}
	if year != 0 {
		q.Set("year", strconv.Itoa(year))
	}
	var res []struct {
		TVDBID       string            `json:"tvdb_id"`
		Name         string            `json:"name"`
		Year         string            `json:"year"`
		Overview     string            `json:"overview"`
		Image        string            `json:"image_url"`
		Translations map[string]string `json:"translations"`
		Overviews    map[string]string `json:"overviews"`
	}
	if err := t.get(ctx, "/search", q, &res); err != nil {
		return nil, err
	}
	out := []SearchResult{}
	for _, r := range res {
		id, _ := strconv.ParseInt(r.TVDBID, 10, 64)
		if id == 0 {
			continue
		}
		name, overview := r.Name, r.Overview
		if v := r.Translations[t.lang]; v != "" {
			name = v
		}
		if v := r.Overviews[t.lang]; v != "" {
			overview = v
		}
		y, _ := strconv.Atoi(r.Year)
		out = append(out, SearchResult{TVDBID: id, Title: name, Year: y, Overview: overview, Poster: r.Image})
	}
	return out, nil
}

type tvdbArtwork struct {
	Image    string  `json:"image"`
	Type     int     `json:"type"`
	Language string  `json:"language"`
	Score    float64 `json:"score"`
}

// TheTVDB artwork type ids.
const (
	tvdbPoster     = 2
	tvdbBackground = 3
	tvdbClearLogo  = 23
)

type tvdbSeries struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Image        string `json:"image"`
	FirstAired   string `json:"firstAired"`
	AverageRun   int    `json:"averageRuntime"`
	OriginalName string `json:"-"` // the untranslated name, when the name was translated
	Genres       []struct {
		Name string `json:"name"`
	} `json:"genres"`
	OriginalNetwork *struct {
		Name string `json:"name"`
	} `json:"originalNetwork"`
	LatestNetwork *struct {
		Name string `json:"name"`
	} `json:"latestNetwork"`
	Artworks  []tvdbArtwork `json:"artworks"`
	RemoteIDs []struct {
		ID         string `json:"id"`
		SourceName string `json:"sourceName"`
	} `json:"remoteIds"`
	Characters []struct {
		Name         string `json:"name"`
		PeopleType   string `json:"peopleType"`
		PersonName   string `json:"personName"`
		PersonImgURL string `json:"personImgURL"`
		Image        string `json:"image"`
		Sort         int    `json:"sort"`
	} `json:"characters"`
	ContentRatings []struct {
		Name    string `json:"name"`
		Country string `json:"country"`
	} `json:"contentRatings"`
	Seasons []struct {
		Number int    `json:"number"`
		Image  string `json:"image"`
		Name   string `json:"name"`
		Type   struct {
			Type string `json:"type"`
		} `json:"type"`
	} `json:"seasons"`
}

// Series fetches a show with its artwork, cast and seasons, with the name
// and overview in the metadata language when TheTVDB has them.
func (t *TVDB) Series(ctx context.Context, id int64) (*tvdbSeries, error) {
	var s tvdbSeries
	if err := t.get(ctx, fmt.Sprintf("/series/%d/extended", id), nil, &s); err != nil {
		return nil, err
	}
	var tr struct {
		Name     string `json:"name"`
		Overview string `json:"overview"`
	}
	if err := t.get(ctx, fmt.Sprintf("/series/%d/translations/%s", id, t.lang), nil, &tr); err == nil {
		if tr.Name != "" && tr.Name != s.Name {
			s.OriginalName, s.Name = s.Name, tr.Name
		}
		if tr.Overview != "" {
			s.Overview = tr.Overview
		}
	}
	return &s, nil
}

type tvdbEpisode struct {
	SeasonNumber int    `json:"seasonNumber"`
	Number       int    `json:"number"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Aired        string `json:"aired"`
	Runtime      int    `json:"runtime"`
	Image        string `json:"image"`
}

// Episodes lists every episode of a show in the given order ("official",
// "dvd" or "absolute"), translated where TheTVDB has it.
func (t *TVDB) Episodes(ctx context.Context, id int64, order string) ([]tvdbEpisode, error) {
	if order == "" {
		order = "official"
	}
	var all []tvdbEpisode
	path := fmt.Sprintf("/series/%d/episodes/%s/%s", id, order, t.lang)
	for page := 0; page < 100; page++ {
		var res struct {
			Episodes []tvdbEpisode `json:"episodes"`
		}
		more, err := t.getPage(ctx, path, url.Values{"page": {strconv.Itoa(page)}}, &res)
		if errors.Is(err, errNotFound) && page == 0 {
			// Nothing in this language: use TheTVDB's default text.
			path = fmt.Sprintf("/series/%d/episodes/%s", id, order)
			more, err = t.getPage(ctx, path, url.Values{"page": {"0"}}, &res)
		}
		if err != nil {
			return nil, err
		}
		all = append(all, res.Episodes...)
		if !more {
			break
		}
	}
	return all, nil
}

// bestArtwork picks the highest-scored image of a type, preferring the
// metadata language, then language-neutral, then anything.
func (t *TVDB) bestArtwork(arts []tvdbArtwork, typ int) string {
	var cands []tvdbArtwork
	for _, a := range arts {
		if a.Type == typ && a.Image != "" {
			cands = append(cands, a)
		}
	}
	rank := func(a tvdbArtwork) int {
		switch a.Language {
		case t.lang:
			return 0
		case "", "zxx":
			return 1
		}
		return 2
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if ri, rj := rank(cands[i]), rank(cands[j]); ri != rj {
			return ri < rj
		}
		return cands[i].Score > cands[j].Score
	})
	if len(cands) == 0 {
		return ""
	}
	return cands[0].Image
}
