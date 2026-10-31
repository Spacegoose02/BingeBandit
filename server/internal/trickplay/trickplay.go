// Package trickplay makes the thumbnails players show while scrubbing the seek
// bar: sheets of small pictures, one every Interval seconds, taken from the
// video's keyframes (so making them only decodes a fraction of the file).
package trickplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

const (
	Interval = 10  // seconds between thumbnails
	Width    = 320 // thumbnail width; the height follows the picture's shape
	Columns  = 10
	Rows     = 10
)

// Info describes a file's thumbnails: thumbnail n (at n×Interval seconds) is
// in sheet n/(Columns×Rows), at column n%Columns and row (n/Columns)%Rows.
type Info struct {
	Interval int `json:"interval"`
	Width    int `json:"width"`
	Height   int `json:"height"`
	Columns  int `json:"columns"`
	Rows     int `json:"rows"`
	Count    int `json:"count"`
	Sheets   int `json:"sheets"`
}

type Generator struct {
	st     *store.Store
	ffmpeg string
	dir    string // <data>/cache/trickplay

	mu      sync.Mutex // one file at a time; decoding is CPU-heavy
	pending sync.Map   // file id → queued for Queue
}

func New(st *store.Store, ffmpeg, dataDir string) *Generator {
	return &Generator{st: st, ffmpeg: ffmpeg, dir: filepath.Join(dataDir, "cache", "trickplay")}
}

// Enabled reports whether the admin has scrub previews on.
func (g *Generator) Enabled() bool { return g.st.Setting(store.SettingTrickplay) != "false" }

// Dir is where a file's thumbnails live. The size in the name makes a
// replaced file (an upgrade) get new ones.
func (g *Generator) Dir(f *store.MediaFile) string {
	return filepath.Join(g.dir, fmt.Sprintf("%d-%d", f.ID, f.Size))
}

// Info returns the file's thumbnails, if they have been made.
func (g *Generator) Info(f *store.MediaFile) (*Info, bool) {
	b, err := os.ReadFile(filepath.Join(g.Dir(f), "info.json"))
	if err != nil {
		return nil, false
	}
	var in Info
	if json.Unmarshal(b, &in) != nil || in.Count == 0 {
		return nil, false
	}
	return &in, true
}

// Sheet is the path of sheet n (0-based).
func (g *Generator) Sheet(f *store.MediaFile, n int) string {
	return filepath.Join(g.Dir(f), fmt.Sprintf("%03d.jpg", n+1))
}

// Queue makes the file's thumbnails in the background, if they're missing,
// so the next time it plays they're there.
func (g *Generator) Queue(f *store.MediaFile) {
	if !g.Enabled() || f.Duration < Interval*2 || f.VideoCodec == "" {
		return
	}
	if _, loaded := g.pending.LoadOrStore(f.ID, true); loaded {
		return
	}
	go func() {
		defer g.pending.Delete(f.ID)
		if err := g.Make(context.Background(), f); err != nil {
			log.Printf("trickplay %d: %v", f.ID, err)
		}
	}()
}

// Make creates the file's thumbnails unless they exist.
func (g *Generator) Make(ctx context.Context, f *store.MediaFile) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.Info(f); ok {
		return nil
	}
	if f.Duration < Interval*2 || f.VideoCodec == "" {
		return errors.New("too short or no video")
	}
	dir := g.Dir(f)
	tmp := dir + ".tmp"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	// Keyframes only (-skip_frame nokey): rounding up, fps shows the last
	// keyframe at or before each slot's time. dar keeps anamorphic video the right shape.
	vf := fmt.Sprintf("fps=1/%d:round=up,scale=%d:trunc(%d/dar/2)*2,setsar=1,tile=%dx%d", Interval, Width, Width, Columns, Rows)
	cmd := exec.CommandContext(ctx, g.ffmpeg, "-nostdin", "-loglevel", "error", "-y",
		"-skip_frame", "nokey", "-i", f.Path, "-map", "0:v:0", "-an", "-sn", "-dn",
		"-vf", vf, "-q:v", "5", "-f", "image2", filepath.Join(tmp, "%03d.jpg"))
	media.HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %v %s", err, strings.TrimSpace(string(out)))
	}
	sheets := 0
	for ; ; sheets++ {
		if _, err := os.Stat(filepath.Join(tmp, fmt.Sprintf("%03d.jpg", sheets+1))); err != nil {
			break
		}
	}
	if sheets == 0 {
		return errors.New("ffmpeg made no pictures")
	}
	first, err := os.Open(filepath.Join(tmp, "001.jpg"))
	if err != nil {
		return err
	}
	cfg, _, err := image.DecodeConfig(first)
	first.Close()
	if err != nil {
		return err
	}
	per := Columns * Rows
	in := Info{
		Interval: Interval, Width: cfg.Width / Columns, Height: cfg.Height / Rows, Columns: Columns, Rows: Rows,
		Count:  min(int(math.Ceil(f.Duration/Interval)), sheets*per),
		Sheets: sheets,
	}
	b, _ := json.Marshal(in)
	if err := os.WriteFile(filepath.Join(tmp, "info.json"), b, 0o644); err != nil {
		return err
	}
	os.RemoveAll(dir)
	return os.Rename(tmp, dir)
}

// Pending makes thumbnails for every file that has none, newest first (the
// scheduled task). It stops at the end of the maintenance window via ctx.
func (g *Generator) Pending(ctx context.Context) (string, error) {
	if !g.Enabled() {
		return "scrub previews are turned off in Settings", nil
	}
	files, err := g.st.VideoFiles()
	if err != nil {
		return "", err
	}
	made, failed := 0, 0
	for _, f := range files {
		if ctx.Err() != nil {
			break
		}
		if _, ok := g.Info(f); ok || f.Duration < Interval*2 || f.VideoCodec == "" {
			continue
		}
		if err := g.Make(ctx, f); err != nil {
			if ctx.Err() == nil {
				failed++
				log.Printf("trickplay %d: %v", f.ID, err)
			}
			continue
		}
		made++
	}
	msg := fmt.Sprintf("made scrub previews for %d files", made)
	if failed > 0 {
		msg += fmt.Sprintf(" (%d failed; see the log)", failed)
	}
	return msg, nil
}

// Clean deletes thumbnails of files that are gone or were replaced, and any
// half-made ones. It returns how many folders and bytes it removed.
func Clean(dataDir string, sizes map[int64]int64) (removed int, freed int64) {
	root := filepath.Join(dataDir, "cache", "trickplay")
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		id, size, ok := strings.Cut(e.Name(), "-")
		fid, err1 := strconv.ParseInt(id, 10, 64)
		fsize, err2 := strconv.ParseInt(size, 10, 64)
		if ok && err1 == nil && err2 == nil && e.IsDir() {
			if s, exists := sizes[fid]; exists && s == fsize {
				continue
			}
		}
		p := filepath.Join(root, e.Name())
		freed += dirSize(p)
		if os.RemoveAll(p) == nil {
			removed++
		}
	}
	return removed, freed
}

func dirSize(p string) int64 {
	var n int64
	filepath.Walk(p, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n += info.Size()
		}
		return nil
	})
	return n
}
