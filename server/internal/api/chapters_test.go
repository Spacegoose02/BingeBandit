package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

// A file's chapters are read with ffprobe, cached, and read again once the
// file is replaced. Someone who can't see the title gets a 404.
func TestFileChapters(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	meta := filepath.Join(dir, "chapters.txt")
	os.WriteFile(meta, []byte(";FFMETADATA1\n"+
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=4000\ntitle=Opening\n"+
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=4000\nEND=10000\ntitle=The Heist\n"), 0o644)
	video := filepath.Join(dir, "clip.mkv")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration=10",
		"-i", meta, "-map_metadata", "1", "-map_chapters", "1", "-c:v", "mpeg4", video).CombinedOutput(); err != nil {
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
	other := &store.Library{Name: "Other", Kind: "movies", Paths: []string{t.TempDir()}}
	st.CreateLibrary(other)
	itemID, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Clip", Year: 2026, MatchKey: "movie:clip:2026"})
	if err := st.UpsertFile(&store.MediaFile{ItemID: itemID, Path: video, Size: 1, MTime: 1, Container: "mkv", Duration: 10}, 1); err != nil {
		t.Fatal(err)
	}
	mf, _ := st.FileByPath(video)
	st.SetUserLibraries(viewer.ID, false, []int64{other.ID})

	h := (&Server{Store: st, FFprobe: ffprobe, DataDir: dir}).Router(context.Background())
	get := func(token string) (int, []media.Chapter) {
		req := httptest.NewRequest("GET", "/api/files/"+itoa(mf.ID)+"/chapters", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var body struct {
			Chapters []media.Chapter `json:"chapters"`
		}
		json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body.Chapters
	}

	code, ch := get("admin-token")
	if code != http.StatusOK || len(ch) != 2 || ch[0].Title != "Opening" || ch[1].Title != "The Heist" || ch[1].Start != 4 {
		t.Fatalf("chapters: %d %+v", code, ch)
	}
	if code, _ := get("sam-token"); code != http.StatusNotFound {
		t.Errorf("someone without the library got %d", code)
	}

	// Replace the file with one without chapters: the cached list is
	// dropped once the scan records the new size.
	if out, err := exec.Command(ffmpeg, "-y", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration=10",
		"-map_chapters", "-1", "-c:v", "mpeg4", video).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if code, ch := get("admin-token"); code != http.StatusOK || len(ch) != 2 {
		t.Fatalf("before the rescan the cached chapters stay: %d %+v", code, ch)
	}
	st.UpsertFile(&store.MediaFile{ItemID: itemID, Path: video, Size: 2, MTime: 2, Container: "mkv", Duration: 10}, 2)
	if code, ch := get("admin-token"); code != http.StatusOK || ch == nil || len(ch) != 0 {
		t.Fatalf("after replacing: %d %#v", code, ch)
	}
}
