package livetv

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func TestSyncPluginChannels(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// An existing channel keeps its number.
	st.CreateChannel(&store.Channel{Number: 5, Name: "News", Enabled: true}, nil)
	a := PluginChannel{Key: "UCa", Name: "Alice", URL: "https://www.youtube.com/channel/UCa/live", Live: true, Title: "Speedrun"}
	b := PluginChannel{Key: "UCb", Name: "Bob", URL: "https://www.youtube.com/channel/UCb/live"}
	if err := SyncPluginChannels(st, "youtube-live", "YouTube Live", []PluginChannel{a, b}); err != nil {
		t.Fatal(err)
	}
	byName := func() map[string]*store.Channel {
		chans, _ := st.Channels(0, true)
		out := map[string]*store.Channel{}
		for _, c := range chans {
			out[c.Name] = c
		}
		return out
	}
	ch := byName()
	if len(ch) != 3 || !ch["Alice"].Enabled || ch["Bob"].Enabled || ch["Alice"].Group != "YouTube Live" || ch["Alice"].Number <= 5 {
		t.Fatalf("channels: %+v %+v", ch["Alice"], ch["Bob"])
	}
	now := time.Now().Unix()
	progs, _ := st.Programs([]string{ch["Alice"].EPGID, ch["Bob"].EPGID}, now, now+60)
	if len(progs) != 1 || progs[0].Title != "Speedrun" {
		t.Fatalf("guide: %+v", progs)
	}
	number := ch["Alice"].Number

	// Alice stops, Bob goes live; then Bob is taken off the list.
	a.Live, b.Live = false, true
	SyncPluginChannels(st, "youtube-live", "YouTube Live", []PluginChannel{a, b})
	if ch = byName(); ch["Alice"].Enabled || !ch["Bob"].Enabled || ch["Alice"].Number != number {
		t.Fatalf("after switching: %+v %+v", ch["Alice"], ch["Bob"])
	}
	SyncPluginChannels(st, "youtube-live", "YouTube Live", []PluginChannel{a})
	if ch = byName(); len(ch) != 2 || ch["Bob"] != nil || ch["News"] == nil {
		t.Fatalf("after removing Bob: %v", ch)
	}
	srcs, _ := st.LiveSources("m3u")
	if len(srcs) != 1 {
		t.Fatalf("sources: %+v", srcs)
	}
	if n, err := refresh(t.Context(), st, srcs[0]); err != nil || n != 1 {
		t.Fatalf("refreshing a plugin source fetches nothing: %d %v", n, err)
	}
}

func TestIsYouTube(t *testing.T) {
	for u, want := range map[string]bool{
		"https://www.youtube.com/channel/UCa/live": true, "https://youtu.be/abc": true,
		"http://example.com/youtube.com/live.m3u8": false, "https://notyoutube.com/x": false,
	} {
		if isYouTube(u) != want {
			t.Errorf("%s: %v", u, !want)
		}
	}
}

func TestResolveYouTubeFromPage(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/channel/UCa/live":
			if c, _ := r.Cookie("SOCS"); c == nil {
				t.Error("no consent cookie")
			}
			fmt.Fprintf(w, `<script>var ytInitialPlayerResponse = {"streamingData":{"hlsManifestUrl":"%s/api/manifest/hls_variant/id/1?a=1&b=2"}}</script>`, srv.URL)
		case "/api/manifest/hls_variant/id/1":
			if r.URL.RawQuery != "a=1&b=2" {
				t.Errorf("query %q", r.URL.RawQuery)
			}
			w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=854x480\n/v/480.m3u8\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=4500000,RESOLUTION=1920x1080\n/v/1080.m3u8\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=20000000,RESOLUTION=3840x2160\n/v/2160.m3u8\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldBin, oldClient := YTDLP, youtubeClient
	YTDLP, youtubeClient = "no-such-yt-dlp", srv.Client()
	defer func() { YTDLP, youtubeClient = oldBin, oldClient }()

	got, err := resolveYouTube(t.Context(), srv.URL+"/channel/UCa/live")
	if err != nil || got != srv.URL+"/v/1080.m3u8" {
		t.Fatalf("resolved %q %v", got, err)
	}
	if _, err := resolveYouTube(t.Context(), srv.URL+"/channel/UCb/live"); err == nil || !strings.Contains(err.Error(), "yt-dlp isn't installed") {
		t.Fatalf("both failing: %v", err)
	}
}

// YouTube's web page often has no hlsManifestUrl now; the player API, asked
// as one of its apps, still gives one (here the second app asked, after a
// TV playlist with a JavaScript challenge would be no use).
func TestResolveYouTubeFromPlayerAPI(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/channel/UCa/live":
			w.Write([]byte(`<link rel="canonical" href="https://www.youtube.com/watch?v=OQpgz8u7YCA"><script>{"isLiveNow":true}</script>`))
		case "/youtubei/v1/player":
			var body struct {
				VideoID string `json:"videoId"`
				Context struct {
					Client struct {
						ClientName string `json:"clientName"`
					} `json:"client"`
				} `json:"context"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.VideoID != "OQpgz8u7YCA" {
				t.Errorf("video %q", body.VideoID)
			}
			switch body.Context.Client.ClientName {
			case "VISIONOS":
				w.Write([]byte(`{"playabilityStatus":{"status":"OK"},"streamingData":{}}`))
				return
			case "TVHTML5":
				t.Error("asked the TV app after iOS answered")
			}
			fmt.Fprintf(w, `{"playabilityStatus":{"status":"OK"},"streamingData":{"hlsManifestUrl":"%s/api/manifest/hls_variant/id/1"}}`, srv.URL)
		case "/api/manifest/hls_variant/id/1":
			w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=4500000,RESOLUTION=1280x720\n/v/720.m3u8\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldBin, oldClient := YTDLP, youtubeClient
	YTDLP, youtubeClient = "no-such-yt-dlp", srv.Client()
	defer func() { YTDLP, youtubeClient = oldBin, oldClient }()

	got, err := resolveYouTube(t.Context(), srv.URL+"/channel/UCa/live")
	if err != nil || got != srv.URL+"/v/720.m3u8" {
		t.Fatalf("resolved %q %v", got, err)
	}
}

func TestPickYouTubeHLS(t *testing.T) {
	formats := []ytdlpFormat{
		{URL: "dash-video", Protocol: "http_dash_segments", VCodec: "avc1", ACodec: "none", Height: 1080},
		{URL: "480", Protocol: "m3u8_native", VCodec: "avc1", ACodec: "mp4a", Height: 480},
		{URL: "1080", Protocol: "m3u8_native", VCodec: "avc1", ACodec: "mp4a", Height: 1080},
		{URL: "2160", Protocol: "m3u8_native", VCodec: "vp9", ACodec: "mp4a", Height: 2160},
	}
	if got := pickHLS(formats); got != "1080" {
		t.Fatalf("picked %q", got)
	}
	if got := pickHLS(formats[:1]); got != "" {
		t.Fatalf("picked %q from DASH only", got)
	}
}

func TestYouTubeVideoID(t *testing.T) {
	for link, want := range map[string]string{
		"https://www.youtube.com/watch?v=OQpgz8u7YCA": "OQpgz8u7YCA",
		"https://youtu.be/OQpgz8u7YCA":                "OQpgz8u7YCA",
		"https://www.youtube.com/live/OQpgz8u7YCA":    "OQpgz8u7YCA",
		"https://www.youtube.com/channel/UCa/live":    "",
	} {
		if got := videoID(link); got != want {
			t.Errorf("%s: %q", link, got)
		}
	}
}

func TestYTDLPMessage(t *testing.T) {
	stderr := "WARNING: [youtube] No supported JavaScript runtime could be found\nERROR: [youtube] x: Requested format is not available\n"
	if got := ytdlpMessage(stderr, nil); got != "[youtube] x: Requested format is not available ([youtube] No supported JavaScript runtime could be found)" {
		t.Fatalf("%q", got)
	}
}

// yt-dlp's JSON is read instead of failing on a format request: here
// there's DASH it can't use and an HLS stream with sound and picture.
func TestYTDLPHLSFromJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as yt-dlp")
	}
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	script := `#!/bin/sh
echo 'WARNING: [youtube] something is missing' >&2
echo '{"id":"OQpgz8u7YCA","formats":[{"url":"dash","protocol":"http_dash_segments","vcodec":"avc1","acodec":"none","height":1080},{"url":"hls720","protocol":"m3u8_native","vcodec":"avc1","acodec":"mp4a","height":720}]}'
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := YTDLP
	YTDLP = bin
	defer func() { YTDLP = old }()
	if got, err := ytdlpHLS(t.Context(), "https://www.youtube.com/channel/UCa/live"); err != nil || got != "hls720" {
		t.Fatalf("got %q %v", got, err)
	}
}
