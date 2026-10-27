package main

import (
	"context"
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
		case "/channel/" + bob + "/live":
			// An upcoming stream sits on /live too, but isn't on yet.
			w.Write([]byte(`<link rel="canonical" href="https://www.youtube.com/watch?v=zzzzzzzzzzz">"liveBroadcastDetails":{"isLiveNow":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer yt.Close()
	baseURL, discover.BaseURL = yt.URL, yt.URL

	a := newApp()
	a.p = &bbplugin.Plugin{Manifest: manifest}
	a.settings = map[string]string{"youtubers": "@alice, " + bob + ", https://example.com/nope", "group": "YouTube Live"}
	var sent []channel
	var group string
	a.send = func(_ context.Context, g string, c []channel) error { group, sent = g, c; return nil }
	a.look(context.Background())
	if group != "YouTube Live" || len(sent) != 2 {
		t.Fatalf("sent %q %+v", group, sent)
	}
	if s := sent[0]; s.Key != alice || s.Name != "Alice & Co" || !s.Live || s.Title != "Speedrun night" || s.StartedAt != 1793124000 ||
		s.URL != "https://www.youtube.com/channel/"+alice+"/live" || s.Logo != "https://yt3.example/a.jpg" {
		t.Errorf("alice: %+v", s)
	}
	if s := sent[1]; s.Key != bob || s.Live {
		t.Errorf("bob (upcoming only): %+v", s)
	}
	if y := a.people["https://example.com/nope"]; y == nil || y.Error == "" {
		t.Errorf("a bad link should show its error: %+v", y)
	}

	mu.Lock()
	aliceLive = false
	mu.Unlock()
	a.look(context.Background())
	if sent[0].Live {
		t.Errorf("alice stopped: %+v", sent[0])
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
