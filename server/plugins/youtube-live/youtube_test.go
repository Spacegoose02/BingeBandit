package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"bingebandit/internal/discover"
	"bingebandit/pkg/bbplugin"
)

const (
	alice = "UCaaaaaaaaaaaaaaaaaaaaaa"
	bob   = "UCbbbbbbbbbbbbbbbbbbbbbb"
)

func TestLook(t *testing.T) {
	var mu sync.Mutex
	aliceLive := true
	yt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/@alice":
			w.Write([]byte(`<link rel="canonical" href="https://www.youtube.com/channel/` + alice + `">`))
		case "/channel/" + alice:
			w.Write([]byte(`<meta property="og:title" content="Alice &amp; Co"><meta property="og:image" content="https://yt3.example/a.jpg">`))
		case "/channel/" + bob:
			w.Write([]byte(`<meta property="og:title" content="Bob"><meta property="og:image" content="https://yt3.example/b.jpg">`))
		case "/channel/" + alice + "/live":
			if aliceLive {
				w.Write([]byte(`<link rel="canonical" href="https://www.youtube.com/watch?v=abcdefghijk"><meta name="title" content="Speedrun night">
					"liveBroadcastDetails":{"isLiveNow":true,"startTimestamp":"2026-10-27T18:00:00+00:00"}`))
			} else {
				w.Write([]byte(`<link rel="canonical" href="https://www.youtube.com/channel/` + alice + `">`))
			}
		case "/channel/" + alice + "/streams":
			// Alice streams twice at once: the one /live shows, and another.
			if aliceLive {
				w.Write([]byte(`{"videoRenderer":{"videoId":"abcdefghijk","title":{"runs":[{"text":"Speedrun night"}]},"style":"LIVE"},
					{"videoRenderer":{"videoId":"second00001","title":{"runs":[{"text":"Cam 2 \u0026 chat"}]},"badges":[{"style":"BADGE_STYLE_TYPE_LIVE_NOW"}]},
					{"videoRenderer":{"videoId":"oldvideo001","title":{"runs":[{"text":"Last week"}]},"style":"DEFAULT"}`))
			}
		case "/channel/" + bob + "/live":
			// An upcoming stream sits on /live too, but isn't on yet.
			w.Write([]byte(`<link rel="canonical" href="https://www.youtube.com/watch?v=zzzzzzzzzzz">"liveBroadcastDetails":{"isLiveNow":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer yt.Close()
	baseURL, discover.BaseURL = yt.URL, yt.URL
	// Without yt-dlp the /live page is read.
	old := ytdlpLive
	ytdlpLive = func(context.Context, string) (liveState, bool) { return liveState{}, false }
	oldStreams := ytdlpStreams
	ytdlpStreams = func(context.Context, string) ([]liveState, bool) { return nil, false }
	defer func() { ytdlpLive, ytdlpStreams = old, oldStreams }()

	a := newApp()
	a.p = &bbplugin.Plugin{Manifest: manifest}
	a.settings = map[string]string{"youtubers": "@alice, " + bob + ", https://example.com/nope", "group": "YouTube Live"}
	var sent []channel
	var group string
	a.send = func(_ context.Context, g string, c []channel) error { group, sent = g, c; return nil }
	a.look(context.Background())
	if group != "YouTube Live" || len(sent) != 3 {
		t.Fatalf("sent %q %+v", group, sent)
	}
	if s := sent[0]; s.Key != alice || s.Name != "Alice & Co" || !s.Live || s.Title != "Speedrun night" || s.StartedAt != 1793124000 ||
		s.URL != "https://www.youtube.com/channel/"+alice+"/live" || s.Logo != "https://yt3.example/a.jpg" {
		t.Errorf("alice: %+v", s)
	}
	if s := sent[1]; s.Key != alice+".second00001" || s.Name != "Alice & Co (2)" || !s.Live || s.Title != "Cam 2 & chat" ||
		s.URL != "https://www.youtube.com/watch?v=second00001" {
		t.Errorf("alice's second stream: %+v", s)
	}
	if s := sent[2]; s.Key != bob || s.Live {
		t.Errorf("bob (upcoming only): %+v", s)
	}
	if y := a.people["https://example.com/nope"]; y == nil || y.Error == "" {
		t.Errorf("a bad link should show its error: %+v", y)
	}

	mu.Lock()
	aliceLive = false
	mu.Unlock()
	a.look(context.Background())
	if sent[0].Live || len(sent) != 2 {
		t.Errorf("alice stopped, and her second channel should go: %+v", sent)
	}

	// The status page lists them.
	h := a.routes()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-BingeBandit-User-Id", "1")
	req.Header.Set("X-BingeBandit-Admin", "true")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Alice &amp; Co") || !strings.Contains(w.Body.String(), "Not live") {
		t.Errorf("page: %d %s", w.Code, w.Body.String())
	}
}

func TestParseYTDLP(t *testing.T) {
	s, ok := parseYTDLP("is_live\tabcdefghijk\t1793124000\tNews tonight: live\n", "", nil)
	if !ok || !s.Live || s.VideoID != "abcdefghijk" || s.StartedAt != 1793124000 || s.Title != "News tonight: live" {
		t.Errorf("live: %+v %v", s, ok)
	}
	if s, ok := parseYTDLP("is_upcoming\tabcdefghijk\tNA\tSoon\n", "", nil); !ok || s.Live {
		t.Errorf("upcoming: %+v %v", s, ok)
	}
	if s, ok := parseYTDLP("", "ERROR: [youtube:tab] UCx: The channel is not currently live\n", errors.New("exit 1")); !ok || s.Live {
		t.Errorf("not live: %+v %v", s, ok)
	}
	if _, ok := parseYTDLP("", "ERROR: Unable to download API page\n", errors.New("exit 1")); ok {
		t.Error("a network error should fall back to reading the page")
	}
}

func TestParseStreams(t *testing.T) {
	got := parseStreams("is_live\tabcdefghijk\tMain cam\nwas_live\tolder000001\tYesterday\nis_live\tsecond00001\tCam 2\tchat\n")
	if len(got) != 2 || got[0].VideoID != "abcdefghijk" || got[1].Title != "Cam 2\tchat" {
		t.Errorf("%+v", got)
	}
}
