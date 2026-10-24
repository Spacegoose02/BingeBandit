package stream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/store"
)

// Runs real ffmpeg: an adaptive session writes a master playlist with one
// variant per rung, each with video and audio, and a single-quality session
// is unchanged when the admin hasn't turned adaptive streaming on.
func TestAdaptiveWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mkv")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=1280x720:rate=25:duration=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=10", "-c:v", "mpeg4", "-q:v", "5", "-c:a", "ac3", clip).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := NewManager(st, ffmpeg, dir)
	defer m.Shutdown()
	f := &store.MediaFile{ID: 1, Path: clip, VideoCodec: "mpeg4", BitDepth: 8, Width: 1280, Height: 720, Duration: 10,
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "mpeg4"}, {Index: 1, Type: "audio", Codec: "ac3", Channels: 1, Default: true}}}
	opts := Options{AudioStream: -1, BurnSubtitle: -1, Adaptive: true}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Off by default: one quality.
	s, err := m.Start(f, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	if s.Playlist() != "index.m3u8" || len(s.Variants) != 0 {
		t.Fatalf("adaptive while the setting is off: %s %v", s.Playlist(), s.Variants)
	}
	m.Stop(s.ID)

	st.SetSettings(map[string]string{store.SettingAdaptiveStreaming: "true"})
	s, err = m.Start(f, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	if s.Playlist() != "master.m3u8" || len(s.Variants) != 2 || s.Variants[0] != 720 || s.Variants[1] != 480 {
		t.Fatalf("variants: %s %v", s.Playlist(), s.Variants)
	}
	master, err := s.WaitFile(ctx, "master.m3u8")
	if err != nil {
		t.Fatalf("no master playlist: %v (%s)", err, s.stderr)
	}
	b, _ := os.ReadFile(master)
	for _, want := range []string{"v0_index.m3u8", "v1_index.m3u8", "RESOLUTION=1280x720", "RESOLUTION=854x480"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("master playlist missing %q:\n%s", want, b)
		}
	}
	if _, err := s.WaitFile(ctx, "v1_seg00000.ts"); err != nil {
		t.Fatalf("no 480p segment: %v (%s)", err, s.stderr)
	}
	if _, err := s.WaitFile(ctx, "../t.db"); err == nil {
		t.Fatal("path outside the session was served")
	}
}
