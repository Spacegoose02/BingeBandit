package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bingebandit/pkg/bbplugin"
)

// ---- reading feeds ----

// entry is what an RSS item tells us about a film or show.
type entry struct {
	Key    string // stable id for the seen list
	Title  string
	Year   int
	TMDB   int64
	Kind   string // movie, show, or "" when the feed doesn't say
	Origin string // the item's title as shown in the feed
}

type xmlField struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

type rssDoc struct {
	Items []struct {
		Title  string     `xml:"title"`
		Link   string     `xml:"link"`
		GUID   string     `xml:"guid"`
		Fields []xmlField `xml:",any"`
	} `xml:"channel>item"`
}

// "Title (2019)", "Title, 2019", "Title, 2019 - ★★★½"
var titleYear = regexp.MustCompile(`^(.+?)(?:\s*\((\d{4})\)|,\s*(\d{4}))(?:\s*[-–—].*)?$`)

func parseFeed(b []byte) ([]entry, error) {
	var doc rssDoc
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("not an RSS feed: %w", err)
	}
	out := make([]entry, 0, len(doc.Items))
	for _, it := range doc.Items {
		e := entry{Origin: strings.TrimSpace(it.Title)}
		e.Key = strings.TrimSpace(it.GUID)
		if e.Key == "" {
			e.Key = strings.TrimSpace(it.Link)
		}
		if e.Key == "" {
			e.Key = e.Origin
		}
		// Letterboxd-style extras: <tmdb:movieId>, <tmdb:tvId>, <letterboxd:filmTitle>, <letterboxd:filmYear>.
		for _, f := range it.Fields {
			v := strings.TrimSpace(f.Value)
			switch f.XMLName.Local {
			case "movieId", "tmdbMovieId":
				e.TMDB, _ = strconv.ParseInt(v, 10, 64)
				e.Kind = "movie"
			case "tvId", "tmdbTvId":
				e.TMDB, _ = strconv.ParseInt(v, 10, 64)
				e.Kind = "show"
			case "filmTitle":
				e.Title = v
			case "filmYear":
				e.Year, _ = strconv.Atoi(v)
			}
		}
		if e.Title == "" {
			e.Title = e.Origin
			if m := titleYear.FindStringSubmatch(e.Origin); m != nil {
				e.Title = strings.TrimSpace(m[1])
				e.Year, _ = strconv.Atoi(m[2] + m[3])
			}
		}
		if e.Title == "" && e.TMDB == 0 {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

var errPrivate = errors.New("feeds on this network can only be added by an admin")

// publicOnly refuses connections to loopback and private addresses, so
// people who aren't admins can't use the plugin to probe the local network.
func publicOnly(_, address string, _ syscall.RawConn) error {
	host, _, _ := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return errPrivate
	}
	return nil
}

var (
	anyClient = &http.Client{}
	// No proxy here: the address check has to see the real destination.
	publicClient = &http.Client{Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: publicOnly}).DialContext,
	}}
)

func fetchURL(ctx context.Context, u string, allowPrivate bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BingeBandit-WatchlistRSS/"+manifest.Version)
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")
	client := publicClient
	if allowPrivate {
		client = anyClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 5<<20))
}

type libItem struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Year        int    `json:"year"`
	Overview    string `json:"overview"`
	TMDBID      int64  `json:"tmdbId"`
	AddedAt     int64  `json:"addedAt"`
	OnWatchlist bool   `json:"onWatchlist"`
}

// check reads a subscription's feed and adds what's in the library to the
// person's watchlist.
func (a *app) check(ctx context.Context, s *Sub) {
	s.LastCheck, s.LastError = time.Now().Unix(), ""
	defer a.subs.update(s)
	body, err := a.fetch(ctx, s.URL, s.Private)
	if err != nil {
		s.LastError = err.Error()
		return
	}
	entries, err := parseFeed(body)
	if err != nil {
		s.LastError = err.Error()
		return
	}
	seen := map[string]bool{}
	for _, k := range s.Seen {
		seen[k] = true
	}
	user := "/users/" + strconv.FormatInt(s.UserID, 10)
	s.Missing = []string{}
	for _, e := range entries {
		if seen[e.Key] {
			continue
		}
		q := url.Values{}
		if e.Kind != "" {
			q.Set("kind", e.Kind)
		}
		if e.TMDB > 0 {
			q.Set("tmdb", strconv.FormatInt(e.TMDB, 10))
		} else {
			q.Set("title", e.Title)
			if e.Year > 0 {
				q.Set("year", strconv.Itoa(e.Year))
			}
		}
		var found []libItem
		if err := a.p.Call(ctx, http.MethodGet, user+"/lookup?"+q.Encode(), nil, &found); err != nil {
			var apiErr *bbplugin.APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
				s.LastError = "this user no longer exists"
			} else {
				s.LastError = err.Error()
			}
			return
		}
		// A TMDB id that isn't matched in the library may still match by title.
		if len(found) == 0 && e.TMDB > 0 && e.Title != "" {
			q.Del("tmdb")
			q.Set("title", e.Title)
			if e.Year > 0 {
				q.Set("year", strconv.Itoa(e.Year))
			}
			a.p.Call(ctx, http.MethodGet, user+"/lookup?"+q.Encode(), nil, &found)
		}
		if len(found) == 0 {
			if len(s.Missing) < 50 {
				s.Missing = append(s.Missing, e.Origin)
			}
			continue
		}
		it := found[0]
		if !it.OnWatchlist {
			if err := a.p.Call(ctx, http.MethodPost, user+"/watchlist/"+strconv.FormatInt(it.ID, 10), nil, nil); err != nil {
				s.LastError = err.Error()
				return
			}
			s.Added++
		}
		s.Seen = append(s.Seen, e.Key)
	}
}

// ---- writing the person's own watchlist as a feed ----

type rssOut struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	NS      string   `xml:"xmlns:bingebandit,attr"`
	Channel struct {
		Title       string    `xml:"title"`
		Link        string    `xml:"link"`
		Description string    `xml:"description"`
		Items       []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssGUID struct {
	PermaLink string `xml:"isPermaLink,attr"`
	Value     string `xml:",chardata"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	GUID        rssGUID `xml:"guid"`
	Description string  `xml:"description,omitempty"`
	Kind        string  `xml:"bingebandit:kind"`
	TMDBID      int64   `xml:"bingebandit:tmdbId,omitempty"`
	Year        int     `xml:"bingebandit:year,omitempty"`
}

func buildFeed(username, external string, items []libItem) ([]byte, error) {
	var doc rssOut
	doc.Version, doc.NS = "2.0", "urn:bingebandit"
	doc.Channel.Title = username + "'s watchlist"
	doc.Channel.Link = external + "/watchlist"
	doc.Channel.Description = "Movies and shows on " + username + "'s BingeBandit watchlist"
	doc.Channel.Items = []rssItem{}
	for _, it := range items {
		title := it.Title
		if it.Year > 0 {
			title += " (" + strconv.Itoa(it.Year) + ")"
		}
		ri := rssItem{
			Title: title, Link: external + "/item/" + strconv.FormatInt(it.ID, 10),
			GUID: rssGUID{"false", "bingebandit-item-" + strconv.FormatInt(it.ID, 10)}, Description: it.Overview,
			Kind: it.Kind, TMDBID: it.TMDBID, Year: it.Year,
		}
		doc.Channel.Items = append(doc.Channel.Items, ri)
	}
	b, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), b...), nil
}
