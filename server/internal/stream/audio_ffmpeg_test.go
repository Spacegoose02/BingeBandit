package stream

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/store"
)

// Runs real ffmpeg: a 5.1 AC-3 track is copied for a surround client (even
// when starting mid-file) and mixed to AAC stereo for an old one.
func TestSurroundAudioWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mkv")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=12", "-filter_complex", "[1:a]pan=5.1|c0=c0|c1=c0|c2=c0|c3=c0|c4=c0|c5=c0[a]",
		"-map", "0:v", "-map", "[a]", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "ac3", clip).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := NewManager(st, ffmpeg, dir)
	defer m.Shutdown()
	f := &store.MediaFile{ID: 1, Path: clip, VideoCodec: "h264", BitDepth: 8, Width: 320, Height: 240, Duration: 12,
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "h264"}, {Index: 1, Type: "audio", Codec: "ac3", Channels: 6, Default: true}}}

	probe := func(o Options) string {
		t.Helper()
		s, err := m.Start(f, 1, o)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		seg, err := s.WaitFile(ctx, "seg00000.ts")
		if err != nil {
			t.Fatalf("no segment: %v (%s)", err, s.stderr)
		}
		out, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,channels",
			"-of", "csv=p=0", seg).Output()
		if err != nil {
			t.Fatal(err)
		}
		// MPEG-TS lists the stream once per program too; the first line is enough.
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		return strings.TrimSpace(line)
	}
	if got := probe(Options{AudioStream: -1, BurnSubtitle: -1, Start: 3, AudioCodecs: []string{"aac", "ac3"}, MaxAudioChannels: 6}); got != "ac3,6" {
		t.Errorf("surround client: %s, want ac3,6", got)
	}
	if got := probe(Options{AudioStream: -1, BurnSubtitle: -1}); got != "aac,2" {
		t.Errorf("old client: %s, want aac,2", got)
	}
}
