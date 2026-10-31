package trickplay

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

// Runs real ffmpeg on a 2 minute 30 second test picture with a keyframe every
// 2 seconds: 15 thumbnails (0 s to 140 s) on one 10×10 sheet.
func TestMake(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "movie.mp4")
	// 640×360 stored, shown 16:9 → thumbnails 320×180.
	if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=10:duration=150",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "20", video).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g := New(st, "ffmpeg", dir)
	f := &store.MediaFile{ID: 7, Path: video, Size: 1234, Duration: 150, VideoCodec: "h264"}
	if _, ok := g.Info(f); ok {
		t.Fatal("info before making")
	}
	if err := g.Make(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	in, ok := g.Info(f)
	if !ok {
		t.Fatal("no info after making")
	}
	want := Info{Interval: 10, Width: 320, Height: 180, Columns: 10, Rows: 10, Count: 15, Sheets: 1}
	if *in != want {
		t.Fatalf("info %+v, want %+v", *in, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "cache", "trickplay", "7-1234.tmp")); !os.IsNotExist(err) {
		t.Error("temporary folder left behind")
	}

	// The BIF: one frame per thumbnail, 10 s apart, each a 320×180 JPEG.
	path, err := g.BIF(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	frames := readBIF(t, path)
	if len(frames) != 15 {
		t.Fatalf("%d BIF frames, want 15", len(frames))
	}
	img, err := jpeg.Decode(bytes.NewReader(frames[3]))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 320 || b.Dy() != 180 {
		t.Errorf("frame is %v", b)
	}
	// From 95 s into the file (a conversion starting there): frames from 90 s on.
	path, _ = g.BIF(f, 95)
	if n := len(readBIF(t, path)); n != 6 {
		t.Errorf("%d frames from 95 s, want 6", n)
	}

	// A replaced file (new size) is cleaned up; the current one is kept.
	if n, _ := Clean(dir, map[int64]int64{7: 1234}); n != 0 {
		t.Errorf("cleaned %d current folders", n)
	}
	if n, _ := Clean(dir, map[int64]int64{7: 999}); n != 1 {
		t.Errorf("cleaned %d stale folders, want 1", n)
	}
}

func readBIF(t *testing.T, path string) [][]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b[:8], bifMagic) {
		t.Fatal("bad magic")
	}
	n := int(binary.LittleEndian.Uint32(b[12:]))
	if every := binary.LittleEndian.Uint32(b[16:]); every != 10000 {
		t.Errorf("every %d ms", every)
	}
	var out [][]byte
	for i := 0; i < n; i++ {
		at := 64 + i*8
		if ts := binary.LittleEndian.Uint32(b[at:]); ts != uint32(i) {
			t.Errorf("frame %d timestamp %d", i, ts)
		}
		start, end := binary.LittleEndian.Uint32(b[at+4:]), binary.LittleEndian.Uint32(b[at+12:])
		out = append(out, b[start:end])
	}
	if end := binary.LittleEndian.Uint32(b[64+n*8:]); end != 0xffffffff {
		t.Error("index not terminated")
	}
	return out
}
