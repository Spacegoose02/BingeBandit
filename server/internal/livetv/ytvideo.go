package livetv

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// YouTubeVideo is where a YouTube video (a trailer, not a live stream) can
// be read from: an HLS media playlist with sound and picture together, or
// else one MP4 file. YouTube ties both to the address that asked for them,
// so the server passes them on rather than handing them to the player.
type YouTubeVideo struct {
	URL string
	HLS bool
}

// ResolveYouTubeVideo asks yt-dlp for a video's best stream up to 1080p
// that has its sound in it: HLS when YouTube offers it (up to 1080p), else
// the MP4 file it still serves whole (usually 360p).
func ResolveYouTubeVideo(ctx context.Context, id string) (YouTubeVideo, error) {
	info, stderr, err := ytdlpJSON(ctx, "https://www.youtube.com/watch?v="+id)
	if err != nil {
		return YouTubeVideo{}, err
	}
	if hls := pickHLS(info.Formats); hls != "" {
		return YouTubeVideo{URL: hls, HLS: true}, nil
	}
	if mp4 := pickMP4(info.Formats); mp4 != "" {
		return YouTubeVideo{URL: mp4}, nil
	}
	msg := "yt-dlp found no stream with sound and picture"
	if w := ytdlpMessage(stderr, nil); w != "" {
		msg += " (" + w + ")"
	}
	return YouTubeVideo{}, errors.New(msg)
}

// pickMP4 takes the tallest whole MP4 file up to 1080p with sound and picture.
func pickMP4(formats []ytdlpFormat) string {
	var best *ytdlpFormat
	for i := range formats {
		f := &formats[i]
		if f.Protocol != "https" || f.Ext != "mp4" || f.URL == "" || f.VCodec == "none" || f.ACodec == "none" || f.Height > maxVariantHeight {
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

// YouTubeGet reads a YouTube playlist (at most 8 MB).
func YouTubeGet(ctx context.Context, link string) (string, error) {
	return youtubeGet(ctx, link)
}

// OpenYouTube starts reading a video file or segment from YouTube, passing
// on the player's Range so it can seek. The caller closes the body.
func OpenYouTube(ctx context.Context, link, rangeHeader string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	if rangeHeader = strings.TrimSpace(rangeHeader); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	return youtubeClient.Do(req)
}
