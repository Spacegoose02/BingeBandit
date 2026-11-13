package markers

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

// makeTVMovie encodes a movie-length "episode": footage over noise of its
// own, with the channel's promo (noise both files share) promoAt seconds in
// for promoLen seconds. withCredits ends it on a black credits roll.
func makeTVMovie(t *testing.T, path string, seed int, dur, promoAt, promoLen float64, withCredits bool) {
	t.Helper()
	const creditsLen = 100
	video := "[0]"
	inputs := []string{"-f", "lavfi", "-i", "testsrc2=s=64x36:r=1:d=" + ftoa(dur)}
	if withCredits {
		inputs = []string{"-f", "lavfi", "-i", "testsrc2=s=64x36:r=1:d=" + ftoa(dur-creditsLen),
			"-f", "lavfi", "-i", "color=black:s=64x36:r=1:d=" + ftoa(creditsLen) + ",drawbox=x=16:y='mod(36-t*6,40)':w=32:h=2:color=white:t=fill"}
		video = "[0][1]concat=n=2:v=1:a=0[v];[v]"
	}
	n := len(inputs) / 4
	noise := func(seed int, d float64) []string {
		return []string{"-f", "lavfi", "-i", "anoisesrc=r=8000:c=pink:seed=" + itoa(seed) + ":d=" + ftoa(d)}
	}
	inputs = append(inputs, noise(seed, promoAt)...)
	inputs = append(inputs, noise(42, promoLen)...)
	inputs = append(inputs, noise(seed+1, dur-promoAt-promoLen)...)
	filter := video + "null[vo];" + "[" + itoa(n) + "][" + itoa(n+1) + "][" + itoa(n+2) + "]concat=n=3:v=0:a=1[ao]"
	args := append([]string{"-loglevel", "error", "-y"}, inputs...)
	args = append(args, "-filter_complex", filter, "-map", "[vo]", "-map", "[ao]",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "10", "-c:a", "aac", "-b:a", "24k", path)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
}

// TV movies filed as episodes of a show share the channel's promos near the
// end, not their credits. That match isn't taken as the credits (Next
// episode came up 10 to 15 minutes early); the picture decides instead.
func TestLongEpisodeCredits(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "TV", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(lib)
	one := 1
	show, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "show", Title: "Channel Movies", MatchKey: "show:cm"})
	season, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "season", ParentID: show, ShowID: show, Title: "Season 1", SeasonNumber: &one, MatchKey: "show:cm:s1"})

	const dur = 4600
	add := func(n int, path string) int64 {
		ep := n
		id, err := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: season, ShowID: show, Title: "Movie " + itoa(n),
			SeasonNumber: &one, EpisodeNumber: &ep, MatchKey: "show:cm:s1:e" + itoa(n)})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertFile(&store.MediaFile{ItemID: id, Path: path, Size: 1, Duration: dur}, 1); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// The promo plays 6:40 before the end of both, and lasts a minute.
	a := filepath.Join(dir, "a.mp4")
	makeTVMovie(t, a, 1, dur, dur-400, 60, true)
	b := filepath.Join(dir, "b.mp4")
	makeTVMovie(t, b, 7, dur, dur-400, 60, false)
	idA, idB := add(1, a), add(2, b)
	// B still has the early credits an older version found.
	if err := st.SetDetectedMarker(idB, "credits", dur-400, dur-340); err != nil {
		t.Fatal(err)
	}

	if _, err := newTestDetector(st, dir).Pending(context.Background()); err != nil {
		t.Fatal(err)
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
	if m := credits(idA); m == nil || m.Start < dur-110 || m.End != dur {
		t.Errorf("a: credits %+v, want the roll at the end (%d-%d)", m, dur-100, dur)
	}
	if m := credits(idB); m != nil {
		t.Errorf("b: credits %+v, want none (the shared sound is a promo)", m)
	}
}

func newTestDetector(st *store.Store, dir string) *Detector {
	return NewDetector(st, "ffmpeg", filepath.Join(dir, "cache"))
}
