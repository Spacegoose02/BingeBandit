package stream

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

// Conversions that encode video stop at the limit; copies don't count and
// aren't refused, and replacing a session (seeking) frees its place.
func TestConversionLimit(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mkv")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=25:duration=120",
		"-c:v", "mpeg4", "-q:v", "5", clip).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSettings(map[string]string{store.SettingMaxConversions: "2", store.SettingTranscodeThrottle: "true"})
	m := NewManager(st, ffmpeg, dir)
	defer m.Shutdown()
	encode := &store.MediaFile{ID: 1, Path: clip, VideoCodec: "mpeg4", BitDepth: 8, Width: 640, Height: 360, Duration: 120,
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "mpeg4"}}}
	copyable := &store.MediaFile{ID: 2, Path: clip, VideoCodec: "h264", BitDepth: 8, Width: 640, Height: 360, Duration: 120,
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "h264"}}}
	opts := Options{AudioStream: -1, BurnSubtitle: -1}

	a, err := m.Start(encode, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(encode, 2, opts); err != nil {
		t.Fatal(err)
	}
	var busy *BusyError
	if _, err := m.Start(encode, 3, opts); !errors.As(err, &busy) || busy.Running != 2 {
		t.Fatalf("third conversion: %v, want busy with 2 running", err)
	}
	if _, err := m.Start(copyable, 3, opts); err != nil {
		t.Fatalf("a copy was refused: %v", err)
	}
	if _, err := m.Start(encode, 1, Options{AudioStream: -1, BurnSubtitle: -1, Start: 30, Replace: a.ID}); err != nil {
		t.Fatalf("seeking was refused: %v", err)
	}
}
