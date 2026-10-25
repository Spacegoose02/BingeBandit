package markers

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

// frames builds samples every step seconds from a pattern of 'c' (credits)
// and '.' (normal picture), one character per sample.
func frames(step float64, pattern string) []frameSample {
	out := make([]frameSample, len(pattern))
	for i, c := range pattern {
		out[i] = frameSample{t: float64(i) * step, credits: c == 'c'}
	}
	return out
}

func TestFindCreditsRun(t *testing.T) {
	cases := []struct {
		name       string
		samples    []frameSample
		length     float64
		start, end float64
		ok         bool
	}{
		{"credits to the end", frames(10, "....cccccccc"), 120, 40, 120, true},
		{"one flash of colour is bridged", frames(10, "...ccc.cccc."), 120, 30, 110, true},
		{"post-credits scene stops the run", frames(5, "....cccccccccccccc....cc"), 120, 20, 90, true},
		{"too short", frames(10, "......ccccc."), 120, 0, 0, false},
		{"no credits", frames(10, "............"), 120, 0, 0, false},
		// Two dark stretches split by real footage: the longer one is the credits.
		{"longest run wins", frames(5, "ccccccccccccc....cccccccccccccccccc"), 175, 85, 175, true},
	}
	for _, c := range cases {
		s, e, ok := FindCreditsRun(c.samples, c.length)
		if ok != c.ok || s != c.start || e != c.end {
			t.Errorf("%s: got %v %v-%v, want %v %v-%v", c.name, ok, s, e, c.ok, c.start, c.end)
		}
	}
}

func TestParseSignalstats(t *testing.T) {
	out := `frame:0    pts:60     pts_time:2.5
lavfi.signalstats.YMIN=16
lavfi.signalstats.YAVG=125.04
lavfi.signalstats.SATAVG=107.451
frame:1    pts:960    pts_time:40
lavfi.signalstats.YAVG=18.5142
lavfi.signalstats.SATAVG=0.0222222
frame:2    pts:1200   pts_time:50
lavfi.signalstats.YAVG=30
lavfi.signalstats.SATAVG=40
`
	got := ParseSignalstats(strings.NewReader(out))
	want := []frameSample{{2.5, false}, {40, true}, {50, false}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("frame %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// makeMovie encodes a small test film: colourful footage, then credits
// (white lines rolling up a black screen) from creditsAt for creditsLen
// seconds, then more footage. gop sets the keyframe interval in frames.
func makeMovie(t *testing.T, path string, creditsAt, creditsLen, after float64, gop int) {
	t.Helper()
	footage := func(d float64) string { return "testsrc2=s=128x72:r=4:d=" + ftoa(d) }
	roll := "color=black:s=128x72:r=4:d=" + ftoa(creditsLen) +
		",drawbox=x=30:y='mod(72-t*6,80)':w=68:h=2:color=white:t=fill" +
		",drawbox=x=44:y='mod(100-t*6,80)':w=40:h=2:color=white:t=fill"
	args := []string{"-loglevel", "error", "-y",
		"-f", "lavfi", "-i", footage(creditsAt), "-f", "lavfi", "-i", roll, "-f", "lavfi", "-i", footage(after),
		"-filter_complex", "[0][1][2]concat=n=3:v=1:a=0",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", itoa(gop), "-sc_threshold", "0", path}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
func itoa(i int) string     { return strconv.Itoa(i) }

func TestMovieCredits(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	d := NewDetector(st, "ffmpeg", filepath.Join(dir, "cache"))

	add := func(title string, duration float64, path string) int64 {
		id, err := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, MatchKey: "movie:" + title})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertFile(&store.MediaFile{ItemID: id, Path: path, Size: 1, Duration: duration}, 1); err != nil {
			t.Fatal(err)
		}
		return id
	}
	credits := func(id int64) *store.Marker {
		ms, _ := st.Markers(id)
		for _, m := range ms {
			if m.Kind == "credits" {
				return &m
			}
		}
		return nil
	}
	near := func(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

	// Credits with a post-credits scene, keyframes every 10 seconds.
	stinger := filepath.Join(dir, "stinger.mp4")
	makeMovie(t, stinger, 1180, 120, 60, 40)
	// Credits that run to the end, and a file with almost no keyframes, so
	// the slower every-two-seconds pass is used.
	sparse := filepath.Join(dir, "sparse.mp4")
	makeMovie(t, sparse, 1220, 100, 10, 100000)
	// A dark scene that is too short to be credits.
	night := filepath.Join(dir, "night.mp4")
	makeMovie(t, night, 1200, 40, 80, 40)

	ids := map[string]int64{
		"stinger": add("stinger", 1360, stinger),
		"sparse":  add("sparse", 1330, sparse),
		"night":   add("night", 1320, night),
		"short":   add("short", 600, filepath.Join(dir, "short.mp4")), // too short for a movie
	}
	msg, err := d.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Log(msg)

	if m := credits(ids["stinger"]); m == nil || !near(m.Start, 1180, 1) || !near(m.End, 1300, 10) {
		t.Errorf("stinger: credits %+v, want 1180-1300", m)
	}
	if m := credits(ids["sparse"]); m == nil || !near(m.Start, 1220, 2) || m.End != 1330 {
		t.Errorf("sparse: credits %+v, want 1220-1330", m)
	}
	for _, name := range []string{"night", "short"} {
		if m := credits(ids[name]); m != nil {
			t.Errorf("%s: unexpected credits %+v", name, m)
		}
	}
	left, _ := st.MoviesNeedingMarkers()
	if len(left) != 0 {
		t.Errorf("movies still pending: %v", left)
	}
}
