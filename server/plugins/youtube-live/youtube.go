package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// baseURL is YouTube; tests point it at a fake.
var baseURL = "https://www.youtube.com"

var client = &http.Client{Timeout: 20 * time.Second}

var (
	reLiveNow   = regexp.MustCompile(`"isLiveNow"\s*:\s*true`)
	reStarted   = regexp.MustCompile(`"liveBroadcastDetails"\s*:\s*\{[^}]*"startTimestamp"\s*:\s*"([^"]+)"`)
	reWatch     = regexp.MustCompile(`<link rel="canonical" href="https://www\.youtube\.com/watch\?v=([A-Za-z0-9_-]{11})"`)
	reMetaTitle = regexp.MustCompile(`<meta name="title" content="([^"]*)"`)
	reOGTitle   = regexp.MustCompile(`<meta property="og:title" content="([^"]*)"`)
	reOGImage   = regexp.MustCompile(`<meta property="og:image" content="([^"]*)"`)
	reOGDesc    = regexp.MustCompile(`<meta property="og:description" content="([^"]*)"`)
)

// get reads a YouTube page, past the EU cookie consent wall.
func get(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	req.AddCookie(&http.Cookie{Name: "SOCS", Value: "CAI"})
	req.AddCookie(&http.Cookie{Name: "CONSENT", Value: "YES+1"})
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", errors.New("YouTube has no such channel")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("YouTube answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return string(b), err
}

func first(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return html.UnescapeString(m[1])
	}
	return ""
}

// profile is a channel's name and picture.
type profile struct {
	Name, Avatar string
}

func channelProfile(ctx context.Context, id string) (profile, error) {
	page, err := get(ctx, "/channel/"+id)
	if err != nil {
		return profile{}, err
	}
	p := profile{Name: first(reOGTitle, page), Avatar: first(reOGImage, page)}
	if p.Name == "" {
		return p, errors.New("couldn't read the channel's name")
	}
	return p, nil
}

// liveState is what a channel's /live page says.
type liveState struct {
	Live        bool
	VideoID     string
	Title       string
	Description string
	StartedAt   int64
}

// checkLive reads a channel's /live page: while it streams, YouTube shows
// the stream there (canonical /watch link, isLiveNow true); otherwise the
// channel page or an upcoming stream, which doesn't count.
func checkLive(ctx context.Context, id string) (liveState, error) {
	page, err := get(ctx, "/channel/"+id+"/live")
	if err != nil {
		return liveState{}, err
	}
	var s liveState
	s.VideoID = first(reWatch, page)
	if s.VideoID == "" || !reLiveNow.MatchString(page) {
		return liveState{}, nil
	}
	s.Live = true
	s.Title = first(reMetaTitle, page)
	if s.Title == "" {
		s.Title = first(reOGTitle, page)
	}
	s.Description = first(reOGDesc, page)
	if ts := first(reStarted, page); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			s.StartedAt = t.Unix()
		} else if n, err := strconv.ParseInt(ts, 10, 64); err == nil {
			s.StartedAt = n
		}
	}
	return s, nil
}

// splitList reads the YouTubers setting: one per line or comma.
func splitList(raw string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' }) {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
