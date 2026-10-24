// Command fakeiptv is a development tool that pretends to be an IPTV
// provider: an M3U playlist, an XMLTV guide and endless MPEG-TS test
// channels made by ffmpeg. Point BingeBandit's Live TV at it to try the
// guide, the shared relay and stream fallback without a real provider.
//
//	go run ./cmd/fakeiptv -addr :8585
//	M3U:   http://localhost:8585/playlist.m3u
//	XMLTV: http://localhost:8585/epg.xml
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type channel struct {
	n      int
	name   string
	group  string
	source string // ffmpeg lavfi video source
	codec  string // h264 | mpeg2 (to exercise transcoding)
	tone   int    // Hz, so channels sound different
	flaky  bool   // stops after 20s, to exercise fallback
}

var channels = []channel{
	{1, "Bandit News", "News", "testsrc2=size=1280x720:rate=30", "h264", 440, false},
	{2, "Bandit Sports", "Sports", "smptehdbars=size=1280x720:rate=30", "h264", 523, false},
	{3, "Flaky Sports", "Sports", "testsrc=size=1280x720:rate=30", "h264", 587, true},
	{4, "Retro Broadcast", "Entertainment", "smptebars=size=720x480:rate=30000/1001", "mpeg2", 659, false},
	{5, "Kids Zone", "Kids", "mandelbrot=size=960x540:rate=25", "h264", 698, false},
	{6, "Nature HD", "Entertainment", "life=size=1280x720:rate=30:mold=10:ratio=0.1", "h264", 784, false},
}

var shows = []string{"Morning Report", "The Big Game", "Cartoon Hour", "Nature Walk", "Late Night Talk", "Cooking Live", "Sports Center", "Movie Classics"}

func main() {
	addr := flag.String("addr", ":8585", "listen address")
	ffmpeg := flag.String("ffmpeg", "ffmpeg", "ffmpeg binary")
	flag.Parse()
	host := "http://localhost" + *addr
	if !strings.HasPrefix(*addr, ":") {
		host = "http://" + *addr
	}

	http.HandleFunc("/playlist.m3u", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		for _, c := range channels {
			fmt.Fprintf(&b, "#EXTINF:-1 tvg-id=\"fake.%d\" tvg-chno=\"%d\" tvg-logo=\"%s/logo/%d.svg\" group-title=\"%s\",%s\n%s/live/%d.ts\n",
				c.n, c.n, host, c.n, c.group, c.name, host, c.n)
		}
		// A backup feed for the flaky channel, as a second provider would offer.
		fmt.Fprintf(&b, "#EXTINF:-1 tvg-id=\"fake.3\" tvg-logo=\"%s/logo/3.svg\" group-title=\"Backup\",Flaky Sports (backup)\n%s/live/3b.ts\n", host, host)
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		w.Write([]byte(b.String()))
	})

	http.HandleFunc("/epg.xml", func(w http.ResponseWriter, r *http.Request) {
		const layout = "20060102150405 -0700"
		start := time.Now().Truncate(30 * time.Minute).Add(-2 * time.Hour)
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n<tv generator-info-name=\"fakeiptv\">\n")
		for _, c := range channels {
			fmt.Fprintf(&b, "<channel id=\"fake.%d\"><display-name>%s</display-name></channel>\n", c.n, c.name)
		}
		for _, c := range channels {
			t := start
			for i := 0; t.Before(start.Add(26 * time.Hour)); i++ {
				dur := time.Duration(30*(1+(i+c.n)%3)) * time.Minute // 30, 60 or 90 minutes
				title := shows[(i+c.n)%len(shows)]
				fmt.Fprintf(&b, "<programme start=\"%s\" stop=\"%s\" channel=\"fake.%d\"><title>%s</title><desc>%s on %s, part %d.</desc><category>%s</category></programme>\n",
					t.UTC().Format(layout), t.Add(dur).UTC().Format(layout), c.n, title, title, c.name, i+1, c.group)
				t = t.Add(dur)
			}
		}
		b.WriteString("</tv>\n")
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(b.String()))
	})

	http.HandleFunc("/logo/", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/logo/"), ".svg"))
		hue := n * 57 % 360
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 60"><rect width="100" height="60" rx="10" fill="hsl(%d,60%%,45%%)"/><text x="50" y="40" font-family="sans-serif" font-size="30" font-weight="bold" fill="#fff" text-anchor="middle">%d</text></svg>`, hue, n)
	})

	http.HandleFunc("/live/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/live/"), ".ts")
		backup := strings.HasSuffix(id, "b")
		n, _ := strconv.Atoi(strings.TrimSuffix(id, "b"))
		var c *channel
		for i := range channels {
			if channels[i].n == n {
				c = &channels[i]
			}
		}
		if c == nil {
			http.NotFound(w, r)
			return
		}
		args := []string{"-hide_banner", "-loglevel", "error", "-re",
			"-f", "lavfi", "-i", c.source, "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:sample_rate=48000", c.tone)}
		if c.flaky && !backup {
			args = append(args, "-t", "20")
		}
		if c.codec == "mpeg2" {
			args = append(args, "-c:v", "mpeg2video", "-b:v", "4M", "-c:a", "mp2")
		} else {
			args = append(args, "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-g", "60", "-pix_fmt", "yuv420p", "-c:a", "aac")
		}
		args = append(args, "-f", "mpegts", "pipe:1")
		cmd := exec.CommandContext(r.Context(), *ffmpeg, args...)
		cmd.Stdout = flushWriter{w}
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		log.Printf("client %s ← channel %s", r.RemoteAddr, id)
		cmd.Run()
		log.Printf("client %s ✕ channel %s", r.RemoteAddr, id)
	})

	log.Printf("fake IPTV provider on %s: %s/playlist.m3u and %s/epg.xml", *addr, host, host)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}
