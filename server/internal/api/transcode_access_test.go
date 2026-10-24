package api

import (
	"context"
	"encoding/json"
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

// Taking a library away from a user ends a transcode they already started.
func TestTranscodeStopsWhenAccessRemoved(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=20",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", video).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}

	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	admin, _ := st.CreateUser("admin", string(hash), true)
	viewer, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("admin-token", admin.ID, "Web", "")
	st.CreateSession("sam-token", viewer.ID, "Web", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	itemID, err := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Clip", Year: 2026, MatchKey: "movie:clip:2026"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertFile(&store.MediaFile{ItemID: itemID, Path: video, Size: 1, Container: "mp4", Duration: 20,
		VideoCodec: "h264", Width: 320, Height: 240}, 1); err != nil {
		t.Fatal(err)
	}
	mf, err := st.FileByPath(video)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserLibraries(viewer.ID, false, []int64{lib.ID}); err != nil {
		t.Fatal(err)
	}

	tc := stream.NewManager(st, ffmpeg, dir)
	defer tc.Shutdown()
	h := (&Server{Store: st, Transcoder: tc, DataDir: dir}).Router(context.Background())
	call := func(token, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := call("sam-token", "POST", "/api/files/"+itoa(mf.ID)+"/transcode", `{}`)
	var started struct {
		Session  stream.Session `json:"session"`
		Playlist string         `json:"playlist"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &started) != nil {
		t.Fatalf("start transcode: %d %s", rec.Code, rec.Body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for call("sam-token", "GET", started.Playlist, "").Code != http.StatusOK {
		if ctx.Err() != nil {
			t.Fatal("transcode never produced a playlist")
		}
		time.Sleep(200 * time.Millisecond)
	}

	if c := call("admin-token", "PUT", "/api/admin/users/"+itoa(viewer.ID)+"/libraries", `{"all":false,"libraryIds":[]}`).Code; c != http.StatusNoContent {
		t.Fatalf("remove access: %d", c)
	}
	if tc.Get(started.Session.ID) != nil {
		t.Fatal("transcode still running after access was removed")
	}
	if c := call("sam-token", "GET", started.Playlist, "").Code; c != http.StatusNotFound {
		t.Fatalf("playlist after access removed: %d", c)
	}
}
