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

// Offline downloads: refused while downloads are off, the original is a
// direct link, a converted copy is prepared in the background and served
// with ranges, and the check drops titles a profile can no longer see.
func TestOfflineDownloads(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mkv")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=1280x720:rate=25:duration=6",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=6", "-c:v", "mpeg4", "-q:v", "5", "-c:a", "ac3", video).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	st.CreateUser("admin", string(hash), true)
	viewer, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("sam-token", viewer.ID, "Android", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	itemID, err := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Clip", Year: 2026, MatchKey: "movie:clip:2026"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertFile(&store.MediaFile{ItemID: itemID, Path: video, Size: 1000, Container: "mkv", Duration: 6,
		VideoCodec: "mpeg4", Width: 1280, Height: 720, Streams: []store.Stream{{Index: 0, Type: "video", Codec: "mpeg4"}, {Index: 1, Type: "audio", Codec: "ac3", Channels: 1, Default: true}}}, 1); err != nil {
		t.Fatal(err)
	}
	mf, err := st.FileByPath(video)
	if err != nil {
		t.Fatal(err)
	}
	st.SetUserLibraries(viewer.ID, false, []int64{lib.ID})

	h := (&Server{Store: st, DataDir: dir, FFmpeg: ffmpeg}).Router(context.Background())
	call := func(method, path, body string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer sam-token")
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	offline := "/api/files/" + itoa(mf.ID) + "/offline"

	if rec := call("POST", offline, `{"height":0}`); rec.Code != http.StatusForbidden {
		t.Fatalf("downloads off: got %d, want 403", rec.Code)
	}
	if err := st.SetSettings(map[string]string{store.SettingAllowDownloads: "true"}); err != nil {
		t.Fatal(err)
	}

	var res offlineResp
	rec := call("POST", offline, `{"height":0}`)
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &res) != nil || res.Status != "ready" || !strings.Contains(res.URL, "/direct?download=1") {
		t.Fatalf("original: %d %s", rec.Code, rec.Body)
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		rec = call("POST", offline, `{"height":480}`)
		res = offlineResp{}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &res) != nil {
			t.Fatalf("prepare: %d %s", rec.Code, rec.Body)
		}
		if res.Status == "ready" || res.Status == "failed" || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if res.Status != "ready" || res.Size <= 0 || res.Height != 480 || !strings.HasPrefix(res.URL, "/api/offline/") {
		t.Fatalf("copy not ready: %+v", res)
	}

	rec = call("GET", res.URL, "", "Range", "bytes=0-99")
	if rec.Code != http.StatusPartialContent || rec.Body.Len() != 100 || rec.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("ranged copy: %d len %d %s", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
	}
	if rec := call("GET", "/api/offline/"+itoa(mf.ID)+"-1-2-3-4", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown copy: %d", rec.Code)
	}

	// The copy is 480p H.264 with stereo AAC.
	key := strings.TrimPrefix(res.URL, "/api/offline/")
	if out, err := exec.Command(strings.TrimSuffix(ffmpeg, "ffmpeg")+"ffprobe", "-v", "error", "-show_entries", "stream=codec_name,height,channels",
		"-of", "csv=p=0", filepath.Join(dir, "cache", "downloads", key+".mp4")).Output(); err == nil {
		got := strings.Fields(strings.ReplaceAll(string(out), ",", " "))
		if strings.Join(got, " ") != "h264 480 aac 2" {
			t.Fatalf("copy streams: %q", out)
		}
	}

	rec = call("POST", "/api/offline/check", `{"items":[`+itoa(itemID)+`, 999999]}`)
	var chk struct {
		CanDownload bool    `json:"canDownload"`
		Allowed     []int64 `json:"allowed"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &chk) != nil || !chk.CanDownload || len(chk.Allowed) != 1 || chk.Allowed[0] != itemID {
		t.Fatalf("check: %d %s", rec.Code, rec.Body)
	}

	// Library taken away: the check drops it and the copy is refused.
	if err := st.SetUserLibraries(viewer.ID, false, []int64{}); err != nil {
		t.Fatal(err)
	}
	rec = call("POST", "/api/offline/check", `{"items":[`+itoa(itemID)+`]}`)
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &chk) != nil || len(chk.Allowed) != 0 {
		t.Fatalf("check after removal: %d %s", rec.Code, rec.Body)
	}
	if rec := call("GET", res.URL, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("copy after removal: %d", rec.Code)
	}
}

func TestDownloadKey(t *testing.T) {
	f := &store.MediaFile{ID: 42, Size: 7}
	k := stream.DownloadKey(f, stream.DownloadOptions{Height: 720, AudioStream: -1, BurnSubtitle: -1})
	if id, ok := stream.KeyFileID(k); !ok || id != 42 {
		t.Fatalf("key %q → %d %v", k, id, ok)
	}
	if _, ok := stream.KeyFileID("../etc"); ok {
		t.Fatal("bad key accepted")
	}
}
