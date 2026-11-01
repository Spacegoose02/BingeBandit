package api

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/store"
)

// Music, audiobook and photo libraries. Only the web app (and the Windows
// app, which shows it) can browse them: it says so with an X-BB-Media header
// listing the library kinds it knows. The TV and Roku apps don't send it, so
// those libraries stay out of their lists instead of opening as broken
// movie grids. Movies and shows are always listed.

const mediaHeader = "X-BB-Media"

// seesLibraryKind says whether the app making r can browse a library kind.
func seesLibraryKind(r *http.Request, kind string) bool {
	if store.IsVideoLibrary(kind) {
		return true
	}
	for _, k := range strings.Split(r.Header.Get(mediaHeader), ",") {
		if strings.TrimSpace(k) == kind {
			return true
		}
	}
	return false
}

// knowsMedia says whether the app making r knows any non-video library.
func knowsMedia(r *http.Request) bool { return strings.TrimSpace(r.Header.Get(mediaHeader)) != "" }

// libraryViewKind is the item kind a library's grid lists: a music library
// lists albums, or artists with view=artists.
func libraryViewKind(lib *store.Library, view string) string {
	if lib.Kind == store.KindMusic {
		if view == "artists" {
			return "artist"
		}
		return "album"
	}
	return ""
}

// mediaHomeRows are Home's rows for music and audiobooks, for apps that know
// them: books the user is partway through, then what's new.
func (s *Server) mediaHomeRows(r *http.Request, uid int64) []homeRow {
	var rows []homeRow
	add := func(id, title string, items []*store.Item, err error) {
		if err == nil && len(items) > 0 {
			rows = append(rows, homeRow{ID: id, Title: title, Items: items})
		}
	}
	if seesLibraryKind(r, store.KindAudiobooks) {
		books, err := s.Store.ContinueListening(uid, rowSize)
		add("continue-listening", "Continue Listening", books, err)
	}
	if seesLibraryKind(r, store.KindMusic) {
		albums, err := s.Store.RecentlyAdded(uid, "album", rowSize)
		add("music-recent", "Recently Added Music", albums, err)
	}
	if seesLibraryKind(r, store.KindAudiobooks) {
		books, err := s.Store.RecentlyAdded(uid, "audiobook", rowSize)
		add("audiobooks-recent", "Recently Added Audiobooks", books, err)
	}
	return rows
}

// itemTracks lists what playing an item plays, with each file: an artist's
// songs album by album, an album's tracks, an audiobook's chapters or a
// photo album's photos. The web's player queues them.
func (s *Server) itemTracks(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	uid := currentUser(r).ID
	if _, err := s.Store.Item(uid, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	tracks, err := s.Store.Tracks(uid, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tracks)
}

// coverWidths are the sizes the cover endpoint draws (?w=), 600 by default.
var coverWidths = map[string]int{"small": 300, "w342": 300, "w500": 600, "medium": 600, "large": 1200, "w780": 1200, "w1280": 1200}

// coverNames are the folder artwork files music and audiobook rippers write.
var coverNames = []string{"cover", "folder", "front", "album", "albumart", "albumartsmall", "poster"}

// folderCover finds cover.jpg (or folder.png, any case) in dir.
func folderCover(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, name := range coverNames {
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !e.IsDir() && (ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp") &&
				strings.EqualFold(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), name) {
				return filepath.Join(dir, e.Name())
			}
		}
	}
	return ""
}

// coverInput picks what a cover is drawn from: a photo itself, else the
// first folder artwork (cover.jpg next to the files, or a folder up for
// Album/CD1), else the first file with a picture inside.
func (s *Server) coverInput(srcs []string, photo bool) (path string, embedded bool) {
	if photo {
		return srcs[0], false
	}
	// Remembered, since finding a picture inside a file runs ffprobe. A new
	// cover.jpg changes the folder's time, which makes a new key.
	key := strings.Join(srcs, "\x00")
	if info, err := os.Stat(filepath.Dir(srcs[0])); err == nil {
		key += "\x00" + info.ModTime().String()
	}
	if c, ok := coverInputs.Load(key); ok {
		c := c.(coverChoice)
		return c.path, c.embedded
	}
	path, embedded = s.findCoverInput(srcs)
	coverInputs.Store(key, coverChoice{path, embedded})
	return path, embedded
}

type coverChoice struct {
	path     string
	embedded bool
}

var coverInputs sync.Map

func (s *Server) findCoverInput(srcs []string) (path string, embedded bool) {
	seen := map[string]bool{}
	for _, src := range srcs {
		dir := filepath.Dir(src)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if c := folderCover(dir); c != "" {
			return c, false
		}
		if c := folderCover(filepath.Dir(dir)); c != "" && reDiscFolder.MatchString(filepath.Base(dir)) {
			return c, false
		}
	}
	for _, src := range srcs {
		if s.hasPicture(src) {
			return src, true
		}
	}
	return "", false
}

var reDiscFolder = regexp.MustCompile(`(?i)^(?:cd|disc|disk|part)[\s._\-]*\d{1,2}$`)

// hasPicture says whether a music file carries cover art. The scan's probe
// leaves picture streams out of what it stores, so this asks ffprobe.
func (s *Server) hasPicture(path string) bool {
	ffprobe := s.FFprobe
	if ffprobe == "" {
		ffprobe = "ffprobe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "v", "-show_entries", "stream=index", "-of", "csv=p=0", path).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// cover draws an album's, artist's, audiobook's or photo's artwork: the
// album folder's cover.jpg, else the picture inside its first file, else
// (for photos) the photo itself, scaled down and cached.
func (s *Server) cover(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, err := s.Store.Item(currentUser(r).ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	srcs, photo, err := s.Store.CoverSources(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	input, embedded := s.coverInput(srcs, photo)
	if input == "" {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(input)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	width, ok := coverWidths[r.URL.Query().Get("w")]
	if !ok {
		width = 600
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", input, info.ModTime().UnixNano(), info.Size())))
	dir := filepath.Join(s.DataDir, "cache", "covers", strconv.Itoa(width))
	out := filepath.Join(dir, hex.EncodeToString(sum[:])+".jpg")
	missing := out + ".none"
	if _, err := os.Stat(missing); err == nil {
		http.NotFound(w, r) // a file with no picture inside; don't run ffmpeg each time
		return
	}
	if _, err := os.Stat(out); err != nil {
		if err := s.drawCover(r.Context(), input, embedded, width, dir, out); err != nil {
			if r.Context().Err() == nil {
				os.WriteFile(missing, nil, 0o644)
			}
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, out)
}

// coverSlots limits how many covers ffmpeg draws at once, so opening a big
// music library doesn't start a hundred ffmpegs.
var coverSlots = make(chan struct{}, 3)

func (s *Server) drawCover(ctx context.Context, input string, embedded bool, width int, dir, out string) error {
	select {
	case coverSlots <- struct{}{}:
		defer func() { <-coverSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if _, err := os.Stat(out); err == nil {
		return nil // drawn while this one waited
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ffmpeg := s.FFmpeg
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tmp := out + ".tmp.jpg"
	args := []string{"-v", "error", "-y", "-i", input}
	if embedded {
		args = append(args, "-an", "-map", "0:v:0")
	}
	args = append(args, "-frames:v", "1", "-vf", fmt.Sprintf("scale='min(%d,iw)':-2", width), "-q:v", "3", tmp)
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	if msg, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		if !embedded {
			log.Printf("cover %s: %v: %s", input, err, strings.TrimSpace(string(msg)))
		}
		return fmt.Errorf("cover: %w", err)
	}
	return os.Rename(tmp, out)
}

// audioStream converts a file's audio to MP3 for browsers that can't play
// it as it is (ALAC in Chrome, WMA, APE...), from ?start= seconds. The
// player seeks by asking again from the new position.
func (s *Server) audioStream(w http.ResponseWriter, r *http.Request) {
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	ffmpeg := s.FFmpeg
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	args := []string{"-v", "error"}
	if start > 0 {
		args = append(args, "-ss", strconv.FormatFloat(start, 'f', 3, 64))
	}
	args = append(args, "-i", mf.Path, "-map", "0:a:0", "-vn", "-c:a", "libmp3lame", "-b:a", "256k", "-f", "mp3", "pipe:1")
	cmd := exec.CommandContext(r.Context(), ffmpeg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot convert")
		return
	}
	if err := cmd.Start(); err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot convert")
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-store")
	io.Copy(w, stdout)
	cmd.Wait()
}

// isPhotoPath says whether a file is a picture (a photo library's), which
// direct play serves without the stream limits meant for video.
func isPhotoPath(path string) bool {
	return strings.HasPrefix(mime.TypeByExtension(strings.ToLower(filepath.Ext(path))), "image/")
}

// mediaSearchKinds are the music and audiobook kinds search finds for apps
// that know them.
func mediaSearchKinds(r *http.Request) []string {
	var kinds []string
	if seesLibraryKind(r, store.KindMusic) {
		kinds = append(kinds, "artist", "album")
	}
	if seesLibraryKind(r, store.KindAudiobooks) {
		kinds = append(kinds, "audiobook")
	}
	return slices.Clip(kinds)
}
