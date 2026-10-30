package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/livetv"
	"bingebandit/internal/store"
)

// A Discover video plays through the server: its HLS playlist comes back
// with numbered parts, and each part is passed on from YouTube.
func TestDiscoverVideoPlays(t *testing.T) {
	yt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hls/96/index.m3u8":
			w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:5\n#EXTINF:5,\nseg/0.ts\n#EXTINF:5,\nhttps://other.example/seg/1.ts\n#EXT-X-ENDLIST\n"))
		case "/hls/96/seg/0.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			w.Write([]byte("part zero"))
		case "/file.mp4":
			if r.Header.Get("Range") != "bytes=2-" {
				t.Errorf("range %q", r.Header.Get("Range"))
			}
			w.Header().Set("Content-Range", "bytes 2-4/5")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("abc"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer yt.Close()

	const hlsID, mp4ID = "aaaaaaaaaaa", "bbbbbbbbbbb"
	resolved := 0
	oldResolve, oldPlaylist, oldOpen := resolveYouTubeVideo, youtubePlaylist, openYouTube
	resolveYouTubeVideo = func(_ context.Context, id string) (livetv.YouTubeVideo, error) {
		resolved++
		if id == mp4ID {
			return livetv.YouTubeVideo{URL: yt.URL + "/file.mp4"}, nil
		}
		return livetv.YouTubeVideo{URL: yt.URL + "/hls/96/index.m3u8", HLS: true}, nil
	}
	youtubePlaylist = func(_ context.Context, link string) (string, error) {
		resp, err := http.Get(link)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return string(b), err
	}
	var opened []string
	openYouTube = func(ctx context.Context, link, rng string) (*http.Response, error) {
		opened = append(opened, link)
		return oldOpen(ctx, link, rng)
	}
	defer func() { resolveYouTubeVideo, youtubePlaylist, openYouTube = oldResolve, oldPlaylist, oldOpen }()

	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	sam, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("sam", sam.ID, "TV", "")
	src := &store.DiscoverSource{Name: "Trailers", Kind: "playlist", YouTubeID: "PLx"}
	if err := st.AddDiscoverSource(src); err != nil {
		t.Fatal(err)
	}
	st.SaveDiscoverVideos(src.ID, []store.DiscoverVideo{{ID: hlsID, Title: "A", PublishedAt: 2}, {ID: mp4ID, Title: "B", PublishedAt: 1}}, "")
	h := (&Server{Store: st}).Router(context.Background())
	get := func(path, rng string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer sam")
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get("/api/discover/videos/"+hlsID, "")
	var out struct{ URL, Type string }
	json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusOK || out.URL != "/api/discover/videos/"+hlsID+"/index.m3u8" || out.Type != "m3u8" {
		t.Fatalf("resolve: %d %s", rec.Code, rec.Body)
	}
	rec = get(out.URL, "")
	want := "#EXTM3U\n#EXT-X-TARGETDURATION:5\n#EXTINF:5,\ns/0\n#EXTINF:5,\ns/1\n#EXT-X-ENDLIST\n"
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("playlist: %d %q", rec.Code, rec.Body)
	}
	rec = get("/api/discover/videos/"+hlsID+"/s/0", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "part zero" || rec.Header().Get("Content-Type") != "video/mp2t" {
		t.Fatalf("part: %d %q", rec.Code, rec.Body)
	}
	if opened[0] != yt.URL+"/hls/96/seg/0.ts" {
		t.Fatalf("opened %v", opened)
	}
	if rec := get("/api/discover/videos/"+hlsID+"/s/2", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("past the end: %d", rec.Code)
	}
	if resolved != 1 {
		t.Fatalf("resolved %d times", resolved)
	}

	// With ?token= (players that send no headers), each part carries it too.
	rec = get("/api/discover/videos/"+hlsID+"/index.m3u8?token=sam", "")
	if !strings.Contains(rec.Body.String(), "s/0?token=sam") {
		t.Fatalf("token playlist: %q", rec.Body)
	}

	// An MP4 file is passed on with the player's range.
	rec = get("/api/discover/videos/"+mp4ID, "")
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Type != "mp4" {
		t.Fatalf("mp4: %s", rec.Body)
	}
	rec = get(out.URL, "bytes=2-")
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "abc" || rec.Header().Get("Content-Range") != "bytes 2-4/5" {
		t.Fatalf("file: %d %q", rec.Code, rec.Body)
	}

	// Only videos on Discover are passed on.
	if rec := get("/api/discover/videos/ccccccccccc", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("other video: %d", rec.Code)
	}
}
