package livetv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"bingebandit/internal/media"
)

// YTDLP is the yt-dlp program that finds a YouTube live stream's HLS
// address; the Docker image has it on the PATH.
var YTDLP = "yt-dlp"

func isYouTube(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	return h == "youtube.com" || h == "m.youtube.com" || h == "youtu.be"
}

// resolveYouTube asks yt-dlp for the HLS playlist of a live stream (a
// /watch link or a channel's /live page). The address it gives expires after
// some hours, so it's looked up each time the stream is opened.
func resolveYouTube(ctx context.Context, link string) (string, error) {
	path, err := exec.LookPath(YTDLP)
	if err != nil {
		return "", errors.New("YouTube streams need yt-dlp, which isn't installed (the Docker image has it)")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--no-warnings", "--no-playlist", "-f", "best[protocol^=m3u8]/best", "-g", link)
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
