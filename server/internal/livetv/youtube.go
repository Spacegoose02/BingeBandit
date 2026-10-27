package livetv

import (
	"bufio"
	"bytes"
	"context"
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
// hlsManifestUrl is used. The address expires after some hours, so it's
// looked up each time the stream is opened.
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

func ytdlpHLS(ctx context.Context, link string) (string, error) {
	path, err := exec.LookPath(YTDLP)
	if err != nil {
		return "", errors.New("yt-dlp isn't installed (the Docker image has it)")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	// Live streams come as HLS with sound and picture together; take the
	// best of those, or anything with both.
	cmd := exec.CommandContext(ctx, path, "--no-warnings", "--no-playlist",
		"-f", "best[protocol^=m3u8][vcodec!=none][acodec!=none]/best[vcodec!=none][acodec!=none]", "-g", link)
	media.HideWindow(cmd)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "ERROR:"); i >= 0 {
			msg = strings.TrimSpace(msg[i+len("ERROR:"):])
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("yt-dlp: %s", msg)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "http") {
			return line, nil
		}
	}
	return "", errors.New("yt-dlp found no stream; the channel may not be live")
}

var (
	reResolution = regexp.MustCompile(`RESOLUTION=\d+x(\d+)`)
	reBandwidth  = regexp.MustCompile(`(?:^|,)BANDWIDTH=(\d+)`)
)

var reHLSManifest = regexp.MustCompile(`"hlsManifestUrl"\s*:\s*"(https?:[^"]+)"`)

// pageHLS reads the live page's hlsManifestUrl (a master playlist) and picks
// its best variant, as streamlink does.
func pageHLS(ctx context.Context, link string) (string, error) {
	body, err := youtubeGet(ctx, link)
	if err != nil {
		return "", err
	}
	m := reHLSManifest.FindStringSubmatch(body)
	if m == nil {
		return "", errors.New("no live stream on the page")
	}
	master := strings.NewReplacer(`&`, "&", `\/`, "/").Replace(m[1])
	playlist, err := youtubeGet(ctx, master)
	if err != nil {
		return "", err
	}
	if v := bestVariant(master, playlist); v != "" {
		return v, nil
	}
	return master, nil
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
