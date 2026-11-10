//go:build !windows

package stream

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

// When the GPU can't decode a codec, the session starts again decoding on
// the CPU, and later conversions of that codec skip the GPU decoder.
func TestHWDecodeFallsBackToCPU(t *testing.T) {
	dir := t.TempDir()
	// A stand-in ffmpeg: it fails like ffmpeg does when asked to decode on
	// the GPU, and otherwise writes a playlist with one segment.
	fake := filepath.Join(dir, "ffmpeg")
	script := `#!/bin/sh
for a in "$@"; do last="$a"; case "$a" in -hwaccel) hw=1;; esac; done
[ "$last" = full ] && exit 0
echo "$@" >> "` + filepath.Join(dir, "calls") + `"
if [ -n "$hw" ]; then
  echo "[av1 @ 0x1] Your platform doesn't support hardware accelerated AV1 decoding." >&2
  echo "[av1 @ 0x1] Failed setup for format vaapi: hwaccel initialisation returned error." >&2
  exit 1
fi
out=$(dirname "$last")
: > "$out/seg00000.ts"
printf '#EXTM3U\n#EXTINF:4.0,\nseg00000.ts\n' > "$last"
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSettings(map[string]string{store.SettingHWEncoder: "vaapi", store.SettingHWDecode: "true", store.SettingTranscodeDir: filepath.Join(dir, "tc")})
	m := NewManager(st, fake, dir)
	defer m.Shutdown()
	f := &store.MediaFile{ID: 1, Path: "clip.mkv", VideoCodec: "av1", BitDepth: 8, Height: 1080,
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "av1"}, {Index: 1, Type: "audio", Codec: "opus", Channels: 2}}}

	s, err := m.Start(f, 1, Options{AudioStream: -1, BurnSubtitle: -1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WaitFile(context.Background(), "index.m3u8"); err != nil {
		t.Fatalf("no playlist after the fallback: %v", err)
	}
	if s.HWDecode {
		t.Error("session still says it decodes on the GPU")
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "-hwaccel vaapi") || strings.Contains(lines[1], "-hwaccel") ||
		!strings.Contains(lines[1], "-vaapi_device") || !strings.Contains(lines[1], "h264_vaapi") {
		t.Fatalf("want a GPU-decode try, then a CPU decode still encoding with VAAPI:\n%s", calls)
	}

	// The next conversion of the same codec goes straight to the CPU.
	s2 := &Session{dir: "out"}
	if args := strings.Join(m.buildArgs(f, s2, Options{AudioStream: -1, BurnSubtitle: -1}), " "); strings.Contains(args, "-hwaccel") || s2.HWDecode {
		t.Errorf("av1 should skip the GPU decoder now:\n%s", args)
	}
	h := *f
	h.VideoCodec = "hevc"
	if args := strings.Join(m.buildArgs(&h, &Session{dir: "out"}, Options{AudioStream: -1, BurnSubtitle: -1}), " "); !strings.Contains(args, "-hwaccel vaapi") {
		t.Errorf("other codecs should still decode on the GPU:\n%s", args)
	}
}
