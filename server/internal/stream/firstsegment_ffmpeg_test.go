package stream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"bingebandit/internal/store"
)

// Runs real ffmpeg: a conversion that encodes video starts with one-second
// segments, so the player gets its first picture sooner, then settles on
// segmentSeconds.
func TestFirstSegmentsAreShort(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mkv")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=25:duration=16",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=16", "-c:v", "mpeg4", "-q:v", "5", "-c:a", "ac3", clip).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := NewManager(st, ffmpeg, dir)
	defer m.Shutdown()
	f := &store.MediaFile{ID: 1, Path: clip, VideoCodec: "mpeg4", BitDepth: 8, Width: 640, Height: 360, Duration: 16,
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "mpeg4"}, {Index: 1, Type: "audio", Codec: "ac3", Channels: 1, Default: true}}}
	s, err := m.Start(f, 1, Options{AudioStream: -1, BurnSubtitle: -1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var b []byte
	for ctx.Err() == nil {
		b, _ = os.ReadFile(filepath.Join(s.dir, "index.m3u8"))
		if regexp.MustCompile(`EXT-X-ENDLIST`).Match(b) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	got := regexp.MustCompile(`#EXTINF:([0-9.]+)`).FindAllStringSubmatch(string(b), -1)
	var lens []string
	for _, g := range got {
		lens = append(lens, g[1][:1])
	}
	want := []string{"1", "1", "1", "1", "4", "4", "4"}
	if len(lens) < len(want) {
		t.Fatalf("segments %v, want at least %v:\n%s", lens, want, b)
	}
	for i := range want {
		if lens[i] != want[i] {
			t.Fatalf("segment lengths %v, want %v first:\n%s", lens, want, b)
		}
	}
}
