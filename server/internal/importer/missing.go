package importer

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"bingebandit/internal/store"
)

// Wanted is a collection title no library has yet.
type Wanted struct {
	Kind   string `json:"kind"` // movie | show
	TMDBID int64  `json:"tmdbId"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
}

// Missing lists the titles a collection would hold that aren't in any
// library: the rest of its TMDB collection (for movies), and the titles of
// the lists it follows. Hand-picked and smart collections have none.
// unknown counts list titles with no TMDB id, which Cinemask can't add.
func (l *Lists) Missing(ctx context.Context, c *store.Collection) (missing []Wanted, unknown int, err error) {
	lib, err := l.st.Library(c.LibraryID)
	if err != nil {
		return nil, 0, err
	}
	var recs []Record
	if c.TMDBID > 0 && lib.Kind == "movies" {
		parts, err := l.tmdbCollection(ctx, c.TMDBID)
		if err != nil {
			return nil, 0, err
		}
		recs = append(recs, parts...)
	}
	if c.ListURL != "" {
		listed, err := l.Fetch(ctx, c.ListURL, lib.Kind)
		if err != nil {
			return nil, 0, err
		}
		recs = append(recs, listed...)
	}
	if len(recs) == 0 {
		return nil, 0, nil
	}
	idx, err := l.st.ImportIndex()
	if err != nil {
		return nil, 0, err
	}
	// Any library counts: a movie in Kids Movies isn't missing.
	m := NewMatcher(idx)
	seen := map[string]bool{}
	for i := range recs {
		r := &recs[i]
		if r.Kind != "movie" && r.Kind != "show" {
			continue
		}
		if m.Match(r) != nil {
			continue
		}
		if r.TMDB <= 0 {
			unknown++
			continue
		}
		k := r.Kind + ":" + strconv.FormatInt(r.TMDB, 10)
		if seen[k] {
			continue
		}
		seen[k] = true
		missing = append(missing, Wanted{Kind: r.Kind, TMDBID: r.TMDB, Title: r.Title, Year: r.Year})
	}
	return missing, unknown, nil
}

// tmdbCollection lists the movies of a TMDB collection (a franchise).
func (l *Lists) tmdbCollection(ctx context.Context, id int64) ([]Record, error) {
	key := strings.TrimSpace(l.st.Setting(store.SettingTMDBKey))
	if key == "" {
		return nil, fmt.Errorf("finding a collection's missing movies needs the TMDB API key in Settings → Metadata")
	}
	header := http.Header{}
	q := url.Values{"language": {l.st.Setting(store.SettingMetadataLanguage)}}
	if strings.HasPrefix(key, "eyJ") {
		header.Set("Authorization", "Bearer "+key)
	} else {
		q.Set("api_key", key)
	}
	var res struct {
		Parts []tmdbEntry `json:"parts"`
	}
	if err := l.getJSON(ctx, l.tmdbBase+"/collection/"+strconv.FormatInt(id, 10)+"?"+q.Encode(), header, &res); err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range res.Parts {
		// Unreleased parts have no date yet; Cinemask can still watch for them.
		if r, ok := e.record("movie"); ok {
			out = append(out, r)
		}
	}
	return out, nil
}
