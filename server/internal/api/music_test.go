package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/library"
	"bingebandit/internal/store"
)

func TestMusicLibraryAPI(t *testing.T) {
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "Music")
	ffmpeg := func(out string, args ...string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(out), 0o755)
		if msg, err := exec.Command("ffmpeg", append(append([]string{"-v", "error", "-y"}, args...), out)...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg: %v %s", err, msg)
		}
	}
	tone := []string{"-f", "lavfi", "-i", "sine=frequency=440:duration=2"}
	ffmpeg(filepath.Join(root, "Daft Punk", "Discovery", "01 One More Time.mp3"),
		append(tone, "-metadata", "title=One More Time", "-metadata", "artist=Daft Punk", "-metadata", "album=Discovery", "-metadata", "track=1")...)
	ffmpeg(filepath.Join(root, "Daft Punk", "Discovery", "02 Aerodynamic.mp3"),
		append(tone, "-metadata", "title=Aerodynamic", "-metadata", "artist=Daft Punk", "-metadata", "album=Discovery", "-metadata", "track=2")...)
	ffmpeg(filepath.Join(root, "Daft Punk", "Discovery", "cover.jpg"), "-f", "lavfi", "-i", "color=c=blue:s=800x800", "-frames:v", "1")

	// An album whose cover is inside its second song only.
	ffmpeg(filepath.Join(dir, "art.png"), "-f", "lavfi", "-i", "color=c=red:s=300x300", "-frames:v", "1")
	ffmpeg(filepath.Join(root, "Daft Punk", "Homework", "01.mp3"), append(tone, "-metadata", "album=Homework", "-metadata", "artist=Daft Punk", "-metadata", "track=1")...)
	ffmpeg(filepath.Join(root, "Daft Punk", "Homework", "02.mp3"), append(tone, "-i", filepath.Join(dir, "art.png"), "-map", "0:a", "-map", "1:v",
		"-c:v", "mjpeg", "-disposition:v", "attached_pic", "-metadata", "album=Homework", "-metadata", "artist=Daft Punk", "-metadata", "track=2")...)

	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	admin, _ := st.CreateUser("admin", string(hash), true)
	st.CreateSession("admin", admin.ID, "Web", "")
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(movies)
	sc := library.NewScanner(st, nil, "ffprobe")
	h := (&Server{Store: st, Scanner: sc, DataDir: dir, FFmpeg: "ffmpeg"}).Router(context.Background())
	call := func(method, path, body string, web bool, out any) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer admin")
		req.Header.Set("Content-Type", "application/json")
		if web {
			req.Header.Set("X-BB-Media", "music,audiobooks,photos")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec
	}

	var music store.Library
	if rec := call("POST", "/api/admin/libraries", `{"name":"Music","kind":"music","paths":["`+filepath.ToSlash(root)+`"]}`, true, &music); rec.Code != http.StatusCreated {
		t.Fatalf("create music library: %d %s", rec.Code, rec.Body)
	}
	if !music.SkipMarkers {
		t.Error("a music library should skip intro detection")
	}
	if rec := call("POST", "/api/admin/libraries", `{"name":"X","kind":"comics","paths":[]}`, true, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown kind: %d", rec.Code)
	}
	// Creating it started a scan; wait for it with one of our own.
	for {
		err := sc.ScanWait(music.ID)
		if err == nil {
			break
		}
		if err != library.ErrRunning {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The TV and Roku apps (no header) don't get the music library at all.
	var libs []store.Library
	call("GET", "/api/libraries", "", false, &libs)
	if len(libs) != 1 || libs[0].Kind != "movies" {
		t.Fatalf("libraries for an app that doesn't know music: %+v", libs)
	}
	if rec := call("GET", "/api/libraries/"+itoa(music.ID)+"/items", "", false, nil); rec.Code != http.StatusNotFound {
		t.Errorf("music grid for the TV app: %d, want 404", rec.Code)
	}
	call("GET", "/api/libraries", "", true, &libs)
	if len(libs) != 2 {
		t.Fatalf("libraries for the web: %+v", libs)
	}

	var page struct {
		Items []store.Item `json:"items"`
		Total int          `json:"total"`
	}
	call("GET", "/api/libraries/"+itoa(music.ID)+"/items", "", true, &page)
	if page.Total != 2 || page.Items[0].Kind != "album" || page.Items[0].Title != "Discovery" {
		t.Fatalf("music grid: %+v", page)
	}
	album, homework := page.Items[0], page.Items[1]
	call("GET", "/api/libraries/"+itoa(music.ID)+"/items?view=artists", "", true, &page)
	if page.Total != 1 || page.Items[0].Kind != "artist" || page.Items[0].Title != "Daft Punk" {
		t.Fatalf("artists view: %+v", page)
	}

	var detail struct {
		Item   store.Item    `json:"item"`
		Artist *store.Item   `json:"artist"`
		Tracks []store.Track `json:"tracks"`
	}
	call("GET", "/api/items/"+itoa(album.ID), "", true, &detail)
	if detail.Artist == nil || detail.Artist.Title != "Daft Punk" || len(detail.Tracks) != 2 || detail.Tracks[1].Item.Title != "Aerodynamic" {
		t.Fatalf("album page: %+v", detail)
	}
	track := detail.Tracks[0]

	// The folder's cover.jpg, scaled to the asked width.
	rec := call("GET", album.Poster+"?w=small", "", true, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" || rec.Body.Len() == 0 {
		t.Fatalf("cover: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	// No cover.jpg: the picture inside the album's second song, for the
	// album and for its first song too.
	var hw struct {
		Tracks []store.Track `json:"tracks"`
	}
	call("GET", "/api/items/"+itoa(homework.ID), "", true, &hw)
	for _, u := range []string{homework.Poster, hw.Tracks[0].Item.Poster} {
		if rec := call("GET", u, "", true, nil); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
			t.Errorf("embedded cover %s: %d", u, rec.Code)
		}
	}
	if hw.Tracks[0].Album != "Homework" {
		t.Errorf("track album = %q", hw.Tracks[0].Album)
	}

	// A file played as it is, and converted for browsers that can't.
	if rec := call("GET", "/api/files/"+itoa(track.File.FileID)+"/direct", "", true, nil); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "audio/mpeg" {
		t.Errorf("direct play: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = call("GET", "/api/files/"+itoa(track.File.FileID)+"/audio?start=1", "", true, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "audio/mpeg" || rec.Body.Len() < 1000 {
		t.Errorf("converted audio: %d %q %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}

	// Search finds artists and albums for the web only.
	var found []store.Item
	call("GET", "/api/search?q=daft", "", false, &found)
	if len(found) != 0 {
		t.Errorf("TV app search found music: %+v", found)
	}
	call("GET", "/api/search?q=daft", "", true, &found)
	if len(found) != 3 {
		t.Errorf("web search for the artist: %d results, want the artist and both albums", len(found))
	}
	// Home gets a music row on the web only.
	var home struct {
		Rows []homeRow `json:"rows"`
	}
	call("GET", "/api/home", "", true, &home)
	if len(home.Rows) != 1 || home.Rows[0].ID != "music-recent" {
		t.Errorf("web home rows: %+v", home.Rows)
	}
	call("GET", "/api/home", "", false, &home)
	if len(home.Rows) != 0 {
		t.Errorf("TV home rows: %+v", home.Rows)
	}
}
