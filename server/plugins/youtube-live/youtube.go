package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os/exec"
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
	reWatch     = regexp.MustCompile(`(?:<link rel="canonical" href="https://www\.youtube\.com/watch\?v=|"videoDetails"\s*:\s*\{\s*"videoId"\s*:\s*")([A-Za-z0-9_-]{11})`)
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

// checkLive asks yt-dlp whether a channel is streaming, since it keeps up
// with YouTube's pages; without yt-dlp, or when it fails for some other
// reason than "not live", the /live page is read instead.
func checkLive(ctx context.Context, id string) (liveState, error) {
	if s, ok := ytdlpLive(ctx, baseURL+"/channel/"+id+"/live"); ok {
		return s, nil
	}
	return scrapeLive(ctx, id)
}

// ytdlpLive runs yt-dlp on a /live link (tests replace it). ok is false
// when yt-dlp can't say.
var ytdlpLive = func(ctx context.Context, link string) (liveState, bool) {
	path, err := exec.LookPath("yt-dlp")
	if err != nil {
		return liveState{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--no-warnings", "--skip-download", "--no-playlist", "--ignore-no-formats-error",
		"-O", "%(live_status)s\t%(id)s\t%(release_timestamp)s\t%(title)s", link)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err = cmd.Run()
	return parseYTDLP(out.String(), stderr.String(), err)
}

// parseYTDLP reads yt-dlp's answer: a line of live status, video id,
// start time and title, or an error saying the channel isn't live.
func parseYTDLP(stdout, stderr string, runErr error) (liveState, bool) {
	if runErr != nil {
		msg := strings.ToLower(stderr)
		if strings.Contains(msg, "not currently live") || strings.Contains(msg, "will begin") || strings.Contains(msg, "premieres in") {
			return liveState{}, true
		}
		return liveState{}, false
	}
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(stdout), "\n", 2)[0])
	f := strings.SplitN(line, "\t", 4)
	if len(f) < 4 {
		return liveState{}, false
	}
	if f[0] != "is_live" {
		return liveState{}, true // upcoming, finished or a plain video
	}
	s := liveState{Live: true, VideoID: f[1], Title: f[3]}
	if n, err := strconv.ParseInt(f[2], 10, 64); err == nil {
		s.StartedAt = n
	}
	return s, true
}

// scrapeLive reads a channel's /live page: while it streams, YouTube shows
// the stream there (canonical /watch link, isLiveNow true); otherwise the
// channel page or an upcoming stream, which doesn't count.
func scrapeLive(ctx context.Context, id string) (liveState, error) {
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
