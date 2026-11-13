package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

// Embedded subtitles are pulled out apart from the request: two players
// asking at once both get them, and a player that gives up early still
// leaves them cached for its next try.
func TestSubtitleExtraction(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	srt := filepath.Join(dir, "subs.srt")
	os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:03,000\nHello there\n\n2\n00:00:04,000 --> 00:00:06,000\nGeneral\n"), 0o644)
	video := filepath.Join(dir, "clip.mkv")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration=8",
		"-i", srt, "-map", "0", "-map", "1", "-c:v", "libx264", "-c:s", "srt", video).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	sam, _ := st.CreateUser("sam", string(hash), true)
	st.CreateSession("sam-token", sam.ID, "TV", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	itemID, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Clip", Year: 2026, MatchKey: "movie:clip:2026"})
	if err := st.UpsertFile(&store.MediaFile{ItemID: itemID, Path: video, Size: 1, Container: "mkv", Duration: 8, VideoCodec: "h264",
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "h264"}, {Index: 1, Type: "subtitle", Codec: "subrip"}}}, 1); err != nil {
		t.Fatal(err)
	}
	mf, _ := st.FileByPath(video)
	srv := &Server{Store: st, DataDir: dir, FFmpeg: ffmpeg}
	h := srv.Router(context.Background())
	path := "/api/files/" + itoa(mf.ID) + "/subtitles/1.vtt"
	get := func(ctx context.Context) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer sam-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// A player that gives up at once.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	get(ctx)
	cache := filepath.Join(dir, "cache", "subtitles", itoa(mf.ID)+"-1-1.vtt")
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(cache); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("an abandoned request didn't leave the subtitles cached")
		}
		time.Sleep(50 * time.Millisecond)
	}
	os.Remove(cache)

	// Two at once.
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 2)
	for i := range recs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recs[i] = get(context.Background())
		}()
	}
	wg.Wait()
	for i, rec := range recs {
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Hello there") {
			t.Errorf("request %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	left, _ := filepath.Glob(filepath.Join(dir, "cache", "subtitles", "extract-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}
