package markers

import (
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// cut is a black frame with silence at t.
func cut(t float64) ([2]float64, [2]float64) {
	return [2]float64{t - 0.2, t + 0.2}, [2]float64{t - 0.25, t + 0.3}
}

func TestFindAdBreaks(t *testing.T) {
	var blacks, silences [][2]float64
	add := func(ts ...float64) {
		for _, x := range ts {
			b, s := cut(x)
			blacks, silences = append(blacks, b), append(silences, s)
		}
	}
	// A scene change at 300 (black, but no silence) isn't a cut.
	blacks = append(blacks, [2]float64{299.9, 300.2})
	// First break at 600: 30 + 15 + 30.5 + 60 s of ads.
	add(600, 630, 645, 675.5, 735.5)
	// A lone fade at 1000 and another at 1400 (400 s apart: show, not ads).
	add(1000, 1400)
	// Second break at 1800: two 30 s ads.
	add(1800, 1830, 1860)
	// A last "break" running into the end of the file is left alone.
	add(2600, 2630, 2660)
	got := FindAdBreaks(blacks, silences, 2662)
	want := [][2]float64{{600, 735.5}, {1800, 1860}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("breaks: %v, want %v", got, want)
	}
	if got := FindAdBreaks(nil, nil, 100); len(got) != 0 {
		t.Fatalf("nothing in, %v out", got)
	}
}

func TestParseEDL(t *testing.T) {
	got := ParseEDL("12.50\t95.20\t0\n\n400.0 520.5 0\nbad line\n")
	want := [][2]float64{{12.5, 95.2}, {400, 520.5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("edl: %v", got)
	}
}

// A real file through ffmpeg: grey picture with a tone, with two black and
// silent ad breaks' worth of cuts. Skipped without ffmpeg.
func TestCommercialsWithFFmpeg(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	t.Setenv("BB_COMSKIP", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "rec.ts")
	// Black and silent for 0.4 s at 60, 75, 90 and 120 s; 150 s long.
	black := "between(t,59.8,60.2)+between(t,74.8,75.2)+between(t,89.8,90.2)+between(t,119.8,120.2)"
	cmd := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "color=c=gray:s=160x90:r=10:d=150",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=22050:duration=150",
		"-vf", "drawbox=c=black:t=fill:enable='"+black+"'",
		"-af", "volume=enable='"+black+"':volume=0",
		"-c:v", "mpeg2video", "-c:a", "mp2", "-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make test file: %v %s", err, out)
	}
	d := NewDetector(nil, ff, dir)
	got, err := d.Commercials(context.Background(), path, 150)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0][0] < 59 || got[0][0] > 61 || got[0][1] < 119 || got[0][1] > 121 {
		t.Fatalf("breaks: %v", got)
	}
}
