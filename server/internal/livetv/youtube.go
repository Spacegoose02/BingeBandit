package livetv

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"bingebandit/internal/media"
)

// YTDLP is the yt-dlp program that finds a YouTube live stream's HLS
// address; the Docker image has it on the PATH (with Deno, which it needs
// to solve YouTube's JavaScript challenge).
var YTDLP = "yt-dlp"

// youtubeClient reads YouTube's pages; tests replace it.
var youtubeClient = upstreamClient

func isYouTube(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	return h == "youtube.com" || h == "m.youtube.com" || h == "youtu.be"
}

// resolveYouTube finds the HLS playlist of a live stream (a /watch link or
// a channel's /live page). yt-dlp is asked first; when it can't (it needs a
// JavaScript runtime for YouTube, and YouTube changes often), the page's own
// hlsManifestUrl is used, or else YouTube's player API is asked as its apps
// ask it, which still hands live streams out as HLS. The address expires
// after some hours, so it's looked up each time the stream is opened.
func resolveYouTube(ctx context.Context, link string) (string, error) {
	hls, ytErr := ytdlpHLS(ctx, link)
	if ytErr == nil {
		return hls, nil
	}
	hls, pageErr := pageHLS(ctx, link)
	if pageErr == nil {
		return hls, nil
	}
	return "", fmt.Errorf("%v; reading the page: %v", ytErr, pageErr)
}

// ytdlpInfo is the part of yt-dlp's JSON that holds the streams.
type ytdlpInfo struct {
	ID      string        `json:"id"`
	Formats []ytdlpFormat `json:"formats"`
}

type ytdlpFormat struct {
	URL         string  `json:"url"`
	ManifestURL string  `json:"manifest_url"`
	Protocol    string  `json:"protocol"`
	VCodec      string  `json:"vcodec"`
	ACodec      string  `json:"acodec"`
	Height      int     `json:"height"`
	TBR         float64 `json:"tbr"`
}

func ytdlpHLS(ctx context.Context, link string) (string, error) {
	path, err := exec.LookPath(YTDLP)
	if err != nil {
		return "", errors.New("yt-dlp isn't installed (the Docker image has it)")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	// The JSON is read rather than asking for a format with -f: when
	// YouTube holds some formats back, yt-dlp fails the whole request even
	// though an HLS playlist is there. Warnings are kept: they say why
	// formats are missing (no JavaScript runtime, a token YouTube wants).
	cmd := exec.CommandContext(ctx, path, "--no-playlist", "--ignore-no-formats-error", "-J", link)
	media.HideWindow(cmd)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	runErr := cmd.Run()
	var info ytdlpInfo
	if runErr == nil {
		runErr = json.Unmarshal(out.Bytes(), &info)
	}
	if runErr != nil {
		return "", fmt.Errorf("yt-dlp: %s", ytdlpMessage(stderr.String(), runErr))
	}
	if hls := pickHLS(info.Formats); hls != "" {
		return hls, nil
	}
	for _, f := range info.Formats {
		if f.ManifestURL != "" && isHLSManifest(f.ManifestURL) {
			if playlist, err := youtubeGet(ctx, f.ManifestURL); err == nil {
				if v := bestVariant(f.ManifestURL, playlist); v != "" {
					return v, nil
				}
			}
			return f.ManifestURL, nil
		}
	}
	msg := "found no stream with sound and picture; the channel may not be live"
	if w := ytdlpMessage(stderr.String(), nil); w != "" {
		msg += " (" + w + ")"
	}
	return "", errors.New("yt-dlp " + msg)
}

// pickHLS takes the tallest HLS format up to 1080p that has sound and
// picture together, as live streams come.
func pickHLS(formats []ytdlpFormat) string {
	var best *ytdlpFormat
	for i := range formats {
		f := &formats[i]
		if !strings.HasPrefix(f.Protocol, "m3u8") || f.URL == "" || f.VCodec == "none" || f.ACodec == "none" || f.Height > maxVariantHeight {
			continue
		}
		if best == nil || f.Height > best.Height || (f.Height == best.Height && f.TBR > best.TBR) {
			best = f
		}
	}
	if best == nil {
		return ""
	}
	return best.URL
}

func isHLSManifest(u string) bool {
	return strings.Contains(u, "/hls_") || strings.Contains(u, ".m3u8")
}

// ytdlpMessage is the last ERROR from yt-dlp, or else its last WARNING
// (which says why formats are missing, e.g. no JavaScript runtime).
func ytdlpMessage(stderr string, runErr error) string {
	var errMsg, warn string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "ERROR:"); i >= 0 {
			errMsg = strings.TrimSpace(line[i+len("ERROR:"):])
		} else if i := strings.Index(line, "WARNING:"); i >= 0 {
			warn = strings.TrimSpace(line[i+len("WARNING:"):])
		}
	}
	switch {
	case errMsg != "" && warn != "":
		return errMsg + " (" + warn + ")"
	case errMsg != "":
		return errMsg
	case warn != "":
		return warn
	case runErr != nil:
		return runErr.Error()
	}
	return ""
}

var (
	reResolution = regexp.MustCompile(`RESOLUTION=\d+x(\d+)`)
	reBandwidth  = regexp.MustCompile(`(?:^|,)BANDWIDTH=(\d+)`)
)

var (
	reHLSManifest = regexp.MustCompile(`"hlsManifestUrl"\s*:\s*"(https?:[^"]+)"`)
	reNChallenge  = regexp.MustCompile(`/n/[^/]+/`)
	reVideoID     = regexp.MustCompile(`(?:<link rel="canonical" href="https://www\.youtube\.com/watch\?v=|"videoDetails"\s*:\s*\{\s*"videoId"\s*:\s*")([A-Za-z0-9_-]{11})`)
)

// pageHLS reads the live page's hlsManifestUrl (a master playlist) and picks
// its best variant, as streamlink does. YouTube's web page often leaves it
// out now, so then the player API is asked for the page's video.
func pageHLS(ctx context.Context, link string) (string, error) {
	body, err := youtubeGet(ctx, link)
	if err != nil {
		return "", err
	}
	var master string
	if m := reHLSManifest.FindStringSubmatch(body); m != nil {
		master = unescapeJSONURL(m[1])
	} else {
		id := videoID(link)
		if m := reVideoID.FindStringSubmatch(body); id == "" && m != nil {
			id = m[1]
		}
		if id == "" {
			return "", errors.New("no live stream on the page")
		}
		if master, err = playerHLS(ctx, link, id); err != nil {
			return "", err
		}
	}
	playlist, err := youtubeGet(ctx, master)
	if err != nil {
		return "", err
	}
	if v := bestVariant(master, playlist); v != "" {
		return v, nil
	}
	return master, nil
}

func unescapeJSONURL(s string) string {
	return strings.NewReplacer(`\u0026`, "&", `\/`, "/").Replace(s)
}

// videoID reads the id from a /watch, /live/<id> or youtu.be link.
func videoID(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	if v := u.Query().Get("v"); len(v) == 11 {
		return v
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	last := parts[len(parts)-1]
	if len(last) == 11 && (strings.EqualFold(u.Hostname(), "youtu.be") || (len(parts) == 2 && parts[0] == "live")) {
		return last
	}
	return ""
}

// innertubeClient is one of YouTube's apps, as its player API knows it.
type innertubeClient struct {
	id        string // X-YouTube-Client-Name
	userAgent string
	context   map[string]any
}

// youtubeApps are the apps whose live HLS YouTube still hands out without
// signing in. visionOS needs nothing more; iOS wants a proof-of-origin token
// after a while, so it comes second; the TV app's playlists carry a
// JavaScript challenge, so they're only used when they don't.
var youtubeApps = []innertubeClient{
	{"101", "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7_3) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15", map[string]any{
		"clientName": "VISIONOS", "clientVersion": "1.02", "deviceMake": "Apple", "deviceModel": "RealityDevice17,1",
		"osName": "visionOS", "osVersion": "26.5.23O471", "hl": "en", "gl": "US"}},
	{"5", "com.google.ios.youtube/21.26.4 (iPhone16,2; U; CPU iOS 18_3_2 like Mac OS X;)", map[string]any{
		"clientName": "IOS", "clientVersion": "21.26.4", "deviceMake": "Apple", "deviceModel": "iPhone16,2",
		"osName": "iPhone", "osVersion": "18.3.2.22D82", "hl": "en", "gl": "US"}},
	{"7", "Mozilla/5.0 (SMART-TV; Linux; Tizen 2.4.0) AppleWebKit/538.1 (KHTML, like Gecko) Version/2.4.0 TV Safari/538.1", map[string]any{
		"clientName": "TVHTML5", "clientVersion": "5.20260707", "deviceMake": "Samsung", "deviceModel": "UKS9800",
		"osName": "Tizen", "osVersion": "2.4.0", "hl": "en", "gl": "US"}},
}

// playerHLS asks YouTube's player API, as its apps do, for a live video's
// HLS master playlist.
func playerHLS(ctx context.Context, link, id string) (string, error) {
	api := "https://www.youtube.com/youtubei/v1/player?prettyPrint=false"
	if u, err := url.Parse(link); err == nil && !strings.EqualFold(u.Hostname(), "youtu.be") {
		api = u.Scheme + "://" + u.Host + "/youtubei/v1/player?prettyPrint=false"
	}
	reason := "no live stream on the page"
	for _, c := range youtubeApps {
		hls, why, err := askPlayer(ctx, api, id, c)
		if err != nil {
			return "", err
		}
		if hls != "" {
			return hls, nil
		}
		if why != "" {
			reason = why
		}
	}
	return "", errors.New(reason)
}

func askPlayer(ctx context.Context, api, id string, c innertubeClient) (hls, reason string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]any{
		"videoId": id, "contentCheckOk": true, "racyCheckOk": true,
		"context": map[string]any{"client": c.context},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-YouTube-Client-Name", c.id)
	req.Header.Set("X-YouTube-Client-Version", fmt.Sprint(c.context["clientVersion"]))
	req.Header.Set("Origin", "https://www.youtube.com")
	resp, err := youtubeClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Sprintf("YouTube's player answered %s", resp.Status), nil
	}
	var r struct {
		PlayabilityStatus struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"playabilityStatus"`
		StreamingData struct {
			HLSManifestURL string `json:"hlsManifestUrl"`
		} `json:"streamingData"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&r); err != nil {
		return "", "YouTube's player sent something unreadable", nil
	}
	if hls := r.StreamingData.HLSManifestURL; hls != "" && !reNChallenge.MatchString(hls) {
		return hls, "", nil
	}
	if r.PlayabilityStatus.Reason != "" {
		return "", "YouTube says: " + r.PlayabilityStatus.Reason, nil
	}
	return "", "", nil
}

func youtubeGet(ctx context.Context, link string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	// Past the EU cookie consent page.
	req.AddCookie(&http.Cookie{Name: "SOCS", Value: "CAI"})
	req.AddCookie(&http.Cookie{Name: "CONSENT", Value: "YES+1"})
	resp, err := youtubeClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("YouTube answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return string(b), err
}

// maxVariantHeight keeps the relay from pulling 4K it would only pass on.
const maxVariantHeight = 1080

// bestVariant picks the tallest variant up to 1080p (then the highest
// bandwidth) from a master playlist, or "" when it isn't one.
func bestVariant(master, playlist string) string {
	base, err := url.Parse(master)
	if err != nil {
		return ""
	}
	var best string
	bestH, bestBW := -1, -1
	sc := bufio.NewScanner(strings.NewReader(playlist))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var h, bw int
	inf := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			inf, h, bw = true, 0, 0
			attrs := line[len("#EXT-X-STREAM-INF:"):]
			if m := reResolution.FindStringSubmatch(attrs); m != nil {
				h, _ = strconv.Atoi(m[1])
			}
			if m := reBandwidth.FindStringSubmatch(attrs); m != nil {
				bw, _ = strconv.Atoi(m[1])
			}
		case inf && line != "" && !strings.HasPrefix(line, "#"):
			inf = false
			if h > maxVariantHeight {
				continue
			}
			if h > bestH || (h == bestH && bw > bestBW) {
				if u, err := base.Parse(line); err == nil {
					best, bestH, bestBW = u.String(), h, bw
				}
			}
		}
	}
	return best
}
