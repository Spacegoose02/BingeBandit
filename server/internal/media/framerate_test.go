package media

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFrameRate(t *testing.T) {
	cases := []struct {
		avg, base string
		want      float64
	}{
		{"24000/1001", "24000/1001", 23.976},
		{"25/1", "50/1", 25},
		{"0/0", "60000/1001", 59.94},
		{"0/0", "0/0", 0},
		{"", "", 0},
	}
	for _, c := range cases {
		if got := frameRate(c.avg, c.base); got != c.want {
			t.Errorf("frameRate(%q, %q) = %v, want %v", c.avg, c.base, got, c.want)
		}
	}
}

func TestVideoFrameRateWithFFprobe(t *testing.T) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg not installed")
	}
	clip := filepath.Join(t.TempDir(), "clip.mkv")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=24000/1001:duration=2",
		"-c:v", "libx264", clip).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}
	fps, err := VideoFrameRate(context.Background(), ffprobe, clip)
	if err != nil || fps != 23.976 {
		t.Fatalf("got %v %v, want 23.976", fps, err)
	}
}
