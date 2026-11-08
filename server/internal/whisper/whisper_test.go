package whisper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A stand-in for whisper-cli: it checks it was given 16 kHz WAV, prints
// progress and the language the way whisper.cpp does, and writes the SRT.
const fakeWhisper = `#!/bin/sh
out=""; wav=""; tr=0
while [ $# -gt 0 ]; do
  case "$1" in
    -of) out="$2"; shift ;;
    -f) wav="$2"; shift ;;
    -tr) tr=1 ;;
  esac
  shift
done
head -c 4 "$wav" | grep -q RIFF || { echo "not a wav" >&2; exit 1; }
echo "whisper_full_with_state: auto-detected language: fr (p = 0.97)" >&2
echo "whisper_print_progress_callback: progress =  50%" >&2
echo "whisper_print_progress_callback: progress = 100%" >&2
printf '1\n00:00:00,000 --> 00:00:01,500\nBonjour (%s)\n' "$tr" > "$out.srt"
`

func TestRun(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "whisper-cli")
	os.WriteFile(bin, []byte(fakeWhisper), 0o755)
	t.Setenv("BB_WHISPER", bin)
	video := filepath.Join(dir, "film.mkv")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:a", "aac", video).CombinedOutput(); err != nil {
		t.Fatalf("making a test file: %v %s", err, out)
	}

	// The model is downloaded once.
	downloads := 0
	hf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ggml-tiny.bin" {
			http.NotFound(w, r)
			return
		}
		downloads++
		w.Write([]byte("model"))
	}))
	defer hf.Close()
	defer func(old string) { ModelBase = old }(ModelBase)
	ModelBase = hf.URL

	r := &Runner{FFmpeg: ffmpeg, DataDir: dir, Model: func() string { return "tiny" }}
	var steps []string
	res, err := r.Run(context.Background(), Request{Path: video}, func(step string, p float64) { steps = append(steps, step) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Language != "fr" || !strings.Contains(string(res.SRT), "Bonjour (0)") {
		t.Fatalf("result: %s %q", res.Language, res.SRT)
	}
	if !strings.Contains(strings.Join(steps, "|"), "Downloading the tiny model") {
		t.Fatalf("steps: %v", steps)
	}
	// Translated: English, and no second download.
	res, err = r.Run(context.Background(), Request{Path: video, Translate: true}, func(string, float64) {})
	if err != nil || res.Language != "en" || !strings.Contains(string(res.SRT), "(1)") {
		t.Fatalf("translate: %v %+v", err, res)
	}
	if downloads != 1 {
		t.Fatalf("model downloaded %d times", downloads)
	}

	// A model name that isn't one, and no whisper at all.
	r.Model = func() string { return "../../etc" }
	if _, err := r.Run(context.Background(), Request{Path: video}, func(string, float64) {}); err == nil {
		t.Fatal("bad model name accepted")
	}
	t.Setenv("BB_WHISPER", "")
	t.Setenv("PATH", "")
	if _, err := r.Run(context.Background(), Request{Path: video}, func(string, float64) {}); err != ErrNoWhisper {
		t.Fatalf("no whisper: %v", err)
	}
}
