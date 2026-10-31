package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
	"bingebandit/internal/stream"
)

// A paused player stops fetching segments once its buffer is full, but keeps
// reporting progress. Those reports keep its conversion from being stopped
// as idle; someone else's reports don't.
func TestProgressKeepsPausedTranscodeAlive(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=10",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", video).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	sam, _ := st.CreateUser("sam", string(hash), true)
	ana, _ := st.CreateUser("ana", string(hash), true)
	st.CreateSession("sam-token", sam.ID, "TV", "")
	st.CreateSession("ana-token", ana.ID, "TV", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	itemID, err := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Clip", Year: 2026, MatchKey: "movie:clip:2026"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertFile(&store.MediaFile{ItemID: itemID, Path: video, Size: 1, Container: "mp4", Duration: 10,
		VideoCodec: "h264", Width: 320, Height: 240}, 1); err != nil {
		t.Fatal(err)
	}
	mf, _ := st.FileByPath(video)

	tc := stream.NewManager(st, ffmpeg, dir)
	defer tc.Shutdown()
	srv := &Server{Store: st, Transcoder: tc, DataDir: dir, Activity: stream.NewActivity()}
	h := srv.Router(context.Background())
	call := func(token, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := call("sam-token", "POST", "/api/files/"+itoa(mf.ID)+"/transcode", `{}`)
	var started struct {
		Session stream.Session `json:"session"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &started) != nil {
		t.Fatalf("start transcode: %d %s", rec.Code, rec.Body)
	}
	sess := tc.Get(started.Session.ID)
	pl := call("sam-token", "GET", "/api/transcode/"+sess.ID+"/index.m3u8", "")
	if pl.Code != http.StatusOK || !strings.Contains(pl.Body.String(), "#EXT-X-START:TIME-OFFSET=0") {
		t.Fatalf("playlist for a header-signed-in app has no EXT-X-START: %d\n%s", pl.Code, pl.Body)
	}
	report := func(token, session string) {
		t.Helper()
		body := fmt.Sprintf(`{"playSessionId":"p-%s","itemId":%d,"fileId":%d,"position":5,"duration":10,"paused":true,"mode":"transcode","transcodeId":%q}`,
			token, itemID, mf.ID, session)
		if rec := call(token, "POST", "/api/progress", body); rec.Code != http.StatusOK {
			t.Fatalf("progress: %d %s", rec.Code, rec.Body)
		}
	}

	before := sess.LastUsed()
	time.Sleep(1100 * time.Millisecond)
	report("ana-token", sess.ID)
	if !sess.LastUsed().Equal(before) {
		t.Fatal("another user's report kept the session alive")
	}
	report("sam-token", sess.ID)
	if !sess.LastUsed().After(before) {
		t.Fatal("a paused player's report didn't keep its conversion alive")
	}
}
