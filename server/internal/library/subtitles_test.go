package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSidecars(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Movie (2020).mkv", "Movie (2020).srt", "Movie (2020).en.srt", "Movie (2020).eng.forced.srt",
		"Movie (2020).English.SDH.srt", "Movie (2020).es.ass", "Movie (2020) 2.srt", "Other.srt", "Movie (2020).nfo",
	} {
		os.WriteFile(filepath.Join(dir, name), nil, 0o644)
	}
	got := sidecars(filepath.Join(dir, "Movie (2020).mkv"))
	type want struct {
		file, lang, codec string
		forced, sdh       bool
	}
	wants := []want{
		{"Movie (2020).English.SDH.srt", "eng", "subrip", false, true},
		{"Movie (2020).en.srt", "eng", "subrip", false, false},
		{"Movie (2020).eng.forced.srt", "eng", "subrip", true, false},
		{"Movie (2020).es.ass", "spa", "ass", false, false},
		{"Movie (2020).srt", "", "subrip", false, false},
	}
	if len(got) != len(wants) {
		t.Fatalf("got %d sidecars: %+v", len(got), got)
	}
	for i, w := range wants {
		g := got[i]
		if g.External != w.file || g.Language != w.lang || g.Codec != w.codec || g.Forced != w.forced || g.HearingImpaired != w.sdh {
			t.Errorf("sidecar %d = %+v, want %+v", i, g, w)
		}
		if g.Index != 1000+i {
			t.Errorf("sidecar %d index = %d", i, g.Index)
		}
	}
}
