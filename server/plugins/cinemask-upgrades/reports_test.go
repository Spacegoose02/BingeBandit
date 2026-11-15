package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func intp(n int) *int { return &n }

func TestIssueBody(t *testing.T) {
	// A movie: no season, and the note with who reported it and where.
	b, err := issueBody(problem{Kind: "video", ItemKind: "movie", TMDBID: 949, User: "Dana", Position: 754, Note: "blocky"})
	if err != nil || b["kind"] != "movie" || b["type"] != "video" || b["season"] != nil || b["message"] != "Reported in BingeBandit by Dana at 12:34: blocky" {
		t.Fatalf("movie: %v %v", b, err)
	}
	// An episode: the show, its season and episode.
	b, _ = issueBody(problem{Kind: "playback", ItemKind: "episode", TMDBID: 1396, Season: intp(2), Episode: intp(5), User: "Dana"})
	if b["kind"] != "show" || b["type"] != "playback" || b["season"] != 2 || b["episode"] != 5 {
		t.Fatalf("episode: %v", b)
	}
	// A whole season: the season without the episode.
	b, _ = issueBody(problem{Kind: "audio", ItemKind: "episode", TMDBID: 1396, Season: intp(2), Episode: intp(5), WholeSeason: true})
	if b["season"] != 2 || b["episode"] != nil || !strings.HasPrefix(b["message"].(string), "Reported in BingeBandit by someone") {
		t.Fatalf("season: %v", b)
	}
	if _, err := issueBody(problem{Kind: "video", ItemKind: "movie"}); err == nil {
		t.Fatal("a title without a TMDB id can't be reported")
	}
}

func TestMirror(t *testing.T) {
	fixed := "Downloading a different copy"
	if st, reply := mirror(problem{Status: "open"}, &Issue{Status: "fixing", Resolution: &fixed}); st != "fixing" || reply != fixed {
		t.Fatalf("fixing: %q %q", st, reply)
	}
	if st, reply := mirror(problem{Status: "fixing"}, &Issue{Status: "resolved"}); st != "resolved" || reply == "" {
		t.Fatalf("resolved: %q %q", st, reply)
	}
	if st, _ := mirror(problem{Status: "fixing"}, &Issue{Status: "fixing"}); st != "" {
		t.Fatalf("same state changed: %q", st)
	}
}

// fakeIssues answers like Cinemask's report fixes: the movie isn't
// followed until it's added, and report 7 was already fixed.
type fakeIssues struct {
	added, reopened bool
	fixes           []string
}

func (f *fakeIssues) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "POST" && r.URL.Path == "/api/monitored":
		f.added = true
		json.NewEncoder(w).Encode(map[string]any{"id": 3})
	case r.Method == "POST" && r.URL.Path == "/api/issues/7/reopen":
		f.reopened = true
		w.Write([]byte(`{}`))
	case r.Method == "POST" && r.URL.Path == "/api/issues/7/fix":
		var b struct{ Action string }
		json.NewDecoder(r.Body).Decode(&b)
		f.fixes = append(f.fixes, b.Action)
		switch {
		case !f.reopened:
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"This report is already fixed"}`))
		case !f.added:
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "message": "Heat isn't managed by Cinemask yet. Monitor it first, then try again."})
		case b.Action == "subtitles":
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "Saved 1 subtitle file.", "issue": map[string]any{"id": 7, "status": "resolved"}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "Grabbed a different one.", "issue": map[string]any{"id": 7, "status": "fixing"}})
		}
	default:
		http.NotFound(w, r)
	}
}

func TestFixReport(t *testing.T) {
	f := &fakeIssues{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c, _ := newCinemask(srv.URL, "k")
	a := &app{}
	// Already fixed in Cinemask and not followed there: reopened, followed, then replaced.
	st, reply, err := a.fixReport(context.Background(), c, problem{ID: 1, Kind: "wrong", ItemKind: "movie", TMDBID: 949, RemoteID: "7"})
	if err != nil || st != "fixing" || reply != "Grabbed a different one." || !f.reopened || !f.added {
		t.Fatalf("replace: %q %q %v (%+v)", st, reply, err, f)
	}
	// Subtitles are fetched, which fixes it at once.
	st, _, err = a.fixReport(context.Background(), c, problem{ID: 1, Kind: "subtitles", ItemKind: "movie", TMDBID: 949, RemoteID: "7"})
	if err != nil || st != "resolved" || f.fixes[len(f.fixes)-1] != "subtitles" {
		t.Fatalf("subtitles: %q %v %v", st, err, f.fixes)
	}
	// "Something else" about a whole season has no fix.
	if _, _, err := a.fixReport(context.Background(), c, problem{Kind: "other", ItemKind: "episode", WholeSeason: true, Season: intp(1)}); err == nil {
		t.Fatal("a whole season's 'something else' can't be fixed")
	}
}
