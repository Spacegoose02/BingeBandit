package importer

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"bingebandit/internal/store"
)

// Lists keeps collections in step with the lists they follow, the way
// Kometa's mdblist_list and RSS builders do: an MDBList list
// (https://mdblist.com/lists/<user>/<list>) or any RSS feed of movies or
// shows, such as a Letterboxd list's.
type Lists struct {
	st     *store.Store
	client *http.Client
	// mdblistBase is where MDBList's API lives; tests point it elsewhere.
	mdblistBase, mdblistSite string
}

func NewLists(st *store.Store) *Lists {
	return &Lists{st: st, client: &http.Client{Timeout: 30 * time.Second}, mdblistBase: "https://api.mdblist.com", mdblistSite: "https://mdblist.com"}
}

// SyncAll refreshes every collection that follows a list.
func (l *Lists) SyncAll(ctx context.Context) (string, error) {
	cols, err := l.st.ListCollections()
	if err != nil {
		return "", err
	}
	synced, failed := 0, 0
	for _, c := range cols {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if l.Sync(ctx, c) != nil {
			failed++
		} else {
			synced++
		}
	}
	msg := fmt.Sprintf("%d list collections synced", synced)
	if failed > 0 {
		msg += fmt.Sprintf(", %d failed (see each collection)", failed)
	}
	return msg, nil
}

// Sync refreshes one collection from its list. A list that can't be read
// leaves the members as they were and records why.
func (l *Lists) Sync(ctx context.Context, c *store.Collection) error {
	lib, err := l.st.Library(c.LibraryID)
	if err != nil {
		return err
	}
	recs, err := l.Fetch(ctx, c.ListURL, lib.Kind)
	if err != nil {
		l.st.SyncCollectionList(c.ID, nil, err.Error())
		return err
	}
	idx, err := l.st.ImportIndex()
	if err != nil {
		return err
	}
	// Only this library's titles: the same movie in Kids Movies belongs to
	// that library's collection.
	own := idx[:0]
	for _, it := range idx {
		if it.LibraryID == c.LibraryID {
			own = append(own, it)
		}
	}
	m := NewMatcher(own)
	var ids []int64
	for i := range recs {
		if it := m.Match(&recs[i]); it != nil {
			ids = append(ids, it.ID)
		}
	}
	return l.st.SyncCollectionList(c.ID, ids, "")
}

// Fetch reads a list's titles. libraryKind ("movies" or "shows") says what
// an RSS item is when the feed doesn't.
func (l *Lists) Fetch(ctx context.Context, raw, libraryKind string) ([]Record, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("use a full https:// link to an MDBList list or an RSS feed")
	}
	if host := strings.TrimPrefix(u.Hostname(), "www."); host == "mdblist.com" {
		return l.mdblist(ctx, u)
	}
	return l.rss(ctx, u.String(), libraryKind)
}

func (l *Lists) get(ctx context.Context, raw string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BingeBandit")
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't read the list: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("the list site refused the request (%s); check the MDBList API key in Settings → Metadata", resp.Status)
		}
		return nil, fmt.Errorf("the list site answered %s", resp.Status)
	}
	return resp.Body, nil
}

var mdblistPath = regexp.MustCompile(`^/lists/([^/]+)/([^/]+)`)

// mdbItem is one title on an MDBList list. MDBList's "id" is the TMDB id;
// newer responses also carry ids.tmdb.
type mdbItem struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Year      int    `json:"release_year"`
	Mediatype string `json:"mediatype"`
	IDs       struct {
		TMDB int64 `json:"tmdb"`
	} `json:"ids"`
}

// mdblist reads a list through the API when there's a key, and through the
// list's public JSON otherwise.
func (l *Lists) mdblist(ctx context.Context, u *url.URL) ([]Record, error) {
	m := mdblistPath.FindStringSubmatch(u.Path)
	if m == nil {
		return nil, fmt.Errorf("an MDBList link looks like https://mdblist.com/lists/<user>/<list>")
	}
	user, list := url.PathEscape(m[1]), url.PathEscape(m[2])
	key := strings.TrimSpace(l.st.Setting(store.SettingMDBListKey))
	if key == "" && l.st.Setting(store.SettingRatingsProvider) == "mdblist" {
		key = strings.TrimSpace(l.st.Setting(store.SettingRatingsKey))
	}
	src := l.mdblistSite + "/lists/" + user + "/" + list + "/json"
	if key != "" {
		src = l.mdblistBase + "/lists/" + user + "/" + list + "/items?limit=5000&apikey=" + url.QueryEscape(key)
	}
	body, err := l.get(ctx, src)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 32<<20))
	if err != nil {
		return nil, err
	}
	// The public JSON is one array; the API splits movies and shows.
	var items []mdbItem
	if err := json.Unmarshal(data, &items); err != nil {
		var split struct {
			Movies []mdbItem `json:"movies"`
			Shows  []mdbItem `json:"shows"`
		}
		if json.Unmarshal(data, &split) != nil {
			return nil, fmt.Errorf("MDBList sent something that isn't a list")
		}
		items = append(split.Movies, split.Shows...)
	}
	out := make([]Record, 0, len(items))
	for _, it := range items {
		kind := it.Mediatype
		if kind != "movie" && kind != "show" {
			continue
		}
		tmdb := it.IDs.TMDB
		if tmdb == 0 {
			tmdb = it.ID
		}
		out = append(out, Record{Kind: kind, Title: it.Title, Year: it.Year, TMDB: tmdb})
	}
	return out, nil
}

// rssItem reads the parts of a feed item that name a title. Letterboxd adds
// tmdb:movieId and letterboxd:filmTitle/filmYear; other feeds have a title.
type rssItem struct {
	Title     string `xml:"title"`
	FilmTitle string `xml:"filmTitle"`
	FilmYear  int    `xml:"filmYear"`
	MovieID   int64  `xml:"movieId"`
	TVID      int64  `xml:"tvId"`
}

// "Title (2019)", "Title, 2019" and Letterboxd's "Title, 2019 - ★★★★".
var titleYear = regexp.MustCompile(`^(.*?)(?:,\s*|\s+\()((?:18|19|20)\d\d)\)?(?:\s+-\s+.*)?$`)

func (l *Lists) rss(ctx context.Context, src, libraryKind string) ([]Record, error) {
	body, err := l.get(ctx, src)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var feed struct {
		Items []rssItem `xml:"channel>item"`
	}
	if err := xml.NewDecoder(io.LimitReader(body, 32<<20)).Decode(&feed); err != nil {
		return nil, fmt.Errorf("that link isn't an MDBList list or an RSS feed")
	}
	kind := "movie"
	if libraryKind == "shows" {
		kind = "show"
	}
	out := make([]Record, 0, len(feed.Items))
	for _, it := range feed.Items {
		r := Record{Kind: kind, Title: strings.TrimSpace(it.FilmTitle), Year: it.FilmYear}
		switch {
		case it.MovieID > 0:
			r.Kind, r.TMDB = "movie", it.MovieID
		case it.TVID > 0:
			r.Kind, r.TMDB = "show", it.TVID
		}
		if r.Title == "" {
			r.Title = strings.TrimSpace(it.Title)
			if m := titleYear.FindStringSubmatch(r.Title); m != nil {
				r.Title = strings.TrimSpace(m[1])
				r.Year, _ = strconv.Atoi(m[2])
			}
		}
		if r.Title != "" || r.TMDB > 0 {
			out = append(out, r)
		}
	}
	return out, nil
}
