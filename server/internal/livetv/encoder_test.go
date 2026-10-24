package livetv

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

func TestHWFallback(t *testing.T) {
	var h hwHealth
	if got := h.pick("qsv"); got != "qsv" {
		t.Fatalf("pick = %q before any failure", got)
	}
	h.markFailed("qsv", "Error creating a MFX session")
	if got := h.pick("qsv"); got != "none" {
		t.Errorf("pick = %q after a failure, want none (CPU)", got)
	}
	if h.lastFailure("qsv") == "" {
		t.Error("the failure reason should be kept for the activity view")
	}
	if got := h.pick("nvenc"); got != "nvenc" {
		t.Errorf("another encoder shouldn't be affected, got %q", got)
	}
	h.clear("qsv")
	if got := h.pick("qsv"); got != "qsv" {
		t.Errorf("pick = %q after clearing", got)
	}
}

func TestEncodeArgs(t *testing.T) {
	in, out := liveEncoder("qsv")
	if !slices.Contains(out, "h264_qsv") {
		t.Errorf("qsv output args = %v", out)
	}
	if runtime.GOOS == "linux" && (!slices.Contains(in, "-init_hw_device") || !strings.Contains(strings.Join(out, " "), "hwupload")) {
		t.Errorf("qsv on Linux should set up the device and upload frames: in=%v out=%v", in, out)
	}
	in, out = liveEncoder("vaapi")
	if !slices.Contains(in, "-vaapi_device") || !slices.Contains(out, "h264_vaapi") {
		t.Errorf("vaapi args: in=%v out=%v", in, out)
	}
	if in, out = liveEncoder("none"); len(in) != 0 || !slices.Contains(out, "libx264") {
		t.Errorf("software args: in=%v out=%v", in, out)
	}
}

func TestEncoderSelfTest(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := &Manager{st: st, ffmpeg: ff}
	if err := m.TestEncoder(context.Background(), "none"); err != nil {
		t.Errorf("software encoder test failed: %v", err)
	}
	if err := m.TestEncoder(context.Background(), "nvenc"); err == nil {
		t.Log("nvenc works here")
	} else if strings.TrimSpace(err.Error()) == "" {
		t.Error("a failed test should say why")
	}
}
