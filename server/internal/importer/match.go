// Package importer brings watch history, watched state and collections over
// from Plex and Tautulli.
package importer

import (
	"regexp"
	"strings"

	"bingebandit/internal/store"
)

// Record is one thing to import: a play from the history, or (StateOnly) the
// current watched/resume state of an item.
type Record struct {
	Key       string // unique within the source; becomes plays.import_key
	User      string // source user name
	Kind      string // movie | episode | show (collections, artwork) | season (artwork)
	Title     string // movie or episode title
	Year      int
	ShowTitle string
	ShowYear  int
	Season    int
	Episode   int
	TMDB      int64  // the movie's, or for episodes and shows the show's
	File      string // base name of the media file, if known

	ViewedAt  int64 // when the play ended (or the item was last viewed)
	StartedAt int64
	Played    int64   // seconds actually played
	Duration  float64 // seconds
	Completed bool
	Client    string

	StateOnly bool    // not a play: just watched state / resume point
	Position  float64 // resume point in seconds (StateOnly)
}

// Label is how a record reads in the "not found" list.
func (r *Record) Label() string {
	switch r.Kind {
	case "episode":
		return fmtEpisode(r.ShowTitle, r.Season, r.Episode, r.Title)
	default:
		if r.Year > 0 {
			return r.Title + " (" + itoa(r.Year) + ")"
		}
		return r.Title
	}
}

var reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// foldAccents maps common Latin accented letters to plain ones ("Amélie" = "Amelie").
var foldAccents = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a", "æ", "ae", "ç", "c",
	"è", "e", "é", "e", "ê", "e", "ë", "e", "ì", "i", "í", "i", "î", "i", "ï", "i",
	"ñ", "n", "ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o", "œ", "oe",
	"ù", "u", "ú", "u", "û", "u", "ü", "u", "ý", "y", "ÿ", "y", "ß", "ss",
)

// normTitle folds case, punctuation and a leading article, so
// "The Office (US)" and "office us" compare equal.
func normTitle(s string) string {
	s = foldAccents.Replace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.TrimSpace(reNonAlnum.ReplaceAllString(s, " "))
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(s, a) && len(s) > len(a) {
			s = s[len(a):]
			break
		}
	}
	return s
}

type epKey struct {
	show    int64
	season  int
	episode int
}

// Matcher finds library items for imported records.
type Matcher struct {
	files      map[string]*store.IndexItem
	movieTMDB  map[int64][]*store.IndexItem
	showTMDB   map[int64][]*store.IndexItem
	movieTitle map[string][]*store.IndexItem
	showTitle  map[string][]*store.IndexItem
	episodes   map[epKey]*store.IndexItem
	seasons    map[epKey]*store.IndexItem // episode 0
	byID       map[int64]*store.IndexItem
}

func NewMatcher(items []*store.IndexItem) *Matcher {
	m := &Matcher{
		files: map[string]*store.IndexItem{}, movieTMDB: map[int64][]*store.IndexItem{}, showTMDB: map[int64][]*store.IndexItem{},
		movieTitle: map[string][]*store.IndexItem{}, showTitle: map[string][]*store.IndexItem{}, episodes: map[epKey]*store.IndexItem{},
		seasons: map[epKey]*store.IndexItem{}, byID: map[int64]*store.IndexItem{},
	}
	for _, it := range items {
		m.byID[it.ID] = it
		for _, f := range it.Files {
			m.files[f] = it
		}
		switch it.Kind {
		case "movie":
			if it.TMDBID > 0 {
				m.movieTMDB[it.TMDBID] = append(m.movieTMDB[it.TMDBID], it)
			}
			m.movieTitle[normTitle(it.Title)] = append(m.movieTitle[normTitle(it.Title)], it)
		case "show":
			if it.TMDBID > 0 {
				m.showTMDB[it.TMDBID] = append(m.showTMDB[it.TMDBID], it)
			}
			m.showTitle[normTitle(it.Title)] = append(m.showTitle[normTitle(it.Title)], it)
		case "season":
			if it.ShowID > 0 {
				m.seasons[epKey{it.ShowID, it.Season, 0}] = it
			}
		case "episode":
			if it.ShowID > 0 && it.Episode > 0 {
				m.episodes[epKey{it.ShowID, it.Season, it.Episode}] = it
			}
		}
	}
	return m
}

// Item returns an indexed item by id.
func (m *Matcher) Item(id int64) *store.IndexItem { return m.byID[id] }

// byYear picks the candidates whose year is within one of want (or all, if
// want is unknown), and returns them only if that leaves exactly one.
func byYear(cands []*store.IndexItem, want int) *store.IndexItem {
	var hit *store.IndexItem
	n := 0
	for _, c := range cands {
		if want == 0 || c.Year == 0 || abs(c.Year-want) <= 1 {
			hit = c
			n++
		}
	}
	if n == 1 {
		return hit
	}
	// Several: prefer an exact year.
	if want > 0 {
		hit, n = nil, 0
		for _, c := range cands {
			if c.Year == want {
				hit = c
				n++
			}
		}
		if n == 1 {
			return hit
		}
	}
	return nil
}

// Match finds the library item for a record, or nil.
func (m *Matcher) Match(r *Record) *store.IndexItem {
	if r.File != "" {
		if it := m.files[strings.ToLower(r.File)]; it != nil && (it.Kind == r.Kind || r.Kind == "") {
			return it
		}
	}
	switch r.Kind {
	case "movie":
		if r.TMDB > 0 {
			if c := m.movieTMDB[r.TMDB]; len(c) > 0 {
				return c[0]
			}
		}
		if it := byYear(m.movieTitle[normTitle(r.Title)], r.Year); it != nil {
			return it
		}
		return byYear(m.movieTitle[normTitle(stripParen(r.Title))], r.Year)
	case "show":
		return m.matchShow(r.TMDB, r.Title, r.Year)
	case "episode":
		show := m.matchShow(r.TMDB, r.ShowTitle, r.ShowYear)
		if show == nil || r.Episode == 0 {
			return nil
		}
		return m.episodes[epKey{show.ID, r.Season, r.Episode}]
	case "season":
		if show := m.matchShow(r.TMDB, r.ShowTitle, r.ShowYear); show != nil {
			return m.seasons[epKey{show.ID, r.Season, 0}]
		}
	}
	return nil
}

func (m *Matcher) matchShow(tmdb int64, title string, year int) *store.IndexItem {
	if tmdb > 0 {
		if c := m.showTMDB[tmdb]; len(c) > 0 {
			return c[0]
		}
	}
	if it := byYear(m.showTitle[normTitle(title)], year); it != nil {
		return it
	}
	// "The Office (US)" in Plex vs "The Office" here.
	return byYear(m.showTitle[normTitle(stripParen(title))], year)
}

var reTrailingParen = regexp.MustCompile(`\s*\([^)]*\)\s*$`)

func stripParen(s string) string { return reTrailingParen.ReplaceAllString(s, "") }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
