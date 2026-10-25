// Package discover reads YouTube playlists and channels that an admin picks
// (trailer channels, "new on Netflix" playlists…) for the Discover page. It
// uses YouTube's public Atom feeds, so no API key is needed; each feed holds
// the newest 15 videos.
package discover

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// BaseURL is YouTube; tests point it at a fake.
var BaseURL = "https://www.youtube.com"

var client = &http.Client{Timeout: 20 * time.Second}

// Source is a parsed playlist or channel reference.
type Source struct {
	Kind string // playlist | channel
	ID   string
}

var (
	rePlaylistID = regexp.MustCompile(`^(PL|UU|LL|FL|OL|RD)[A-Za-z0-9_-]{10,}$`)
	reChannelID  = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	reHandle     = regexp.MustCompile(`^@[A-Za-z0-9._-]{3,30}$`)
	// A channel page names its own id in the canonical link and in its data.
	reCanonical = regexp.MustCompile(`(?:<link rel="canonical" href="https://www\.youtube\.com/channel/|"externalId":")(UC[A-Za-z0-9_-]{22})`)
)

var ErrUnknownSource = errors.New("paste a YouTube playlist or channel link, a playlist id (PL…), a channel id (UC…) or an @handle")

// Parse understands playlist links (…?list=PL…), channel links
// (/channel/UC…, /@handle), bare ids and @handles. Handles are looked up
// on YouTube to find the channel id.
func Parse(ctx context.Context, in string) (Source, error) {
	in = strings.TrimSpace(in)
	switch {
	case rePlaylistID.MatchString(in):
		return Source{"playlist", in}, nil
	case reChannelID.MatchString(in):
		return Source{"channel", in}, nil
	case reHandle.MatchString(in):
		return resolveHandle(ctx, in)
	}
	u, err := url.Parse(in)
	if err != nil || u.Host == "" {
		if !strings.Contains(in, "://") && strings.Contains(in, "youtube.com/") {
			return Parse(ctx, "https://"+in)
		}
		return Source{}, ErrUnknownSource
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host != "youtube.com" && host != "m.youtube.com" && host != "music.youtube.com" {
		return Source{}, ErrUnknownSource
	}
	if list := u.Query().Get("list"); rePlaylistID.MatchString(list) {
		return Source{"playlist", list}, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(parts) >= 2 && parts[0] == "channel" && reChannelID.MatchString(parts[1]):
		return Source{"channel", parts[1]}, nil
	case len(parts) >= 1 && reHandle.MatchString(parts[0]):
		return resolveHandle(ctx, parts[0])
	}
	return Source{}, ErrUnknownSource
}

func resolveHandle(ctx context.Context, handle string) (Source, error) {
	body, err := get(ctx, BaseURL+"/"+url.PathEscape(handle), 2<<20)
	if err != nil {
		return Source{}, fmt.Errorf("looking up %s: %w", handle, err)
	}
	m := reCanonical.FindSubmatch(body)
	if m == nil {
		return Source{}, fmt.Errorf("couldn't find the channel %s on YouTube", handle)
	}
	return Source{"channel", string(m[1])}, nil
}

// Video is one entry of a feed.
type Video struct {
	ID          string
	Title       string
	Description string
	Published   time.Time
}

// Feed is a playlist's or channel's title and newest videos.
type Feed struct {
	Title  string
	Videos []Video
}

// Fetch reads the source's feed.
func Fetch(ctx context.Context, s Source) (*Feed, error) {
	param := "playlist_id"
	if s.Kind == "channel" {
		param = "channel_id"
	}
	body, err := get(ctx, BaseURL+"/feeds/videos.xml?"+param+"="+url.QueryEscape(s.ID), 4<<20)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Title   string `xml:"title"`
		Entries []struct {
			VideoID   string `xml:"http://www.youtube.com/xml/schemas/2015 videoId"`
			Title     string `xml:"title"`
			Published string `xml:"published"`
			Group     struct {
				Description string `xml:"description"`
			} `xml:"http://search.yahoo.com/mrss/ group"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("reading the feed: %w", err)
	}
	f := &Feed{Title: strings.TrimSpace(doc.Title)}
	for _, e := range doc.Entries {
		if e.VideoID == "" {
			continue
		}
		t, _ := time.Parse(time.RFC3339, e.Published)
		desc := strings.TrimSpace(e.Group.Description)
		if r := []rune(desc); len(r) > 500 {
			desc = string(r[:500]) + "…"
		}
		f.Videos = append(f.Videos, Video{ID: e.VideoID, Title: strings.TrimSpace(e.Title), Description: desc, Published: t})
	}
	return f, nil
}

func get(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// Without these YouTube may answer with a cookie-consent page.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BingeBandit)")
	req.Header.Set("Accept-Language", "en")
	req.AddCookie(&http.Cookie{Name: "CONSENT", Value: "YES+1"})
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("YouTube says it doesn't exist (or it's private)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("YouTube: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}
