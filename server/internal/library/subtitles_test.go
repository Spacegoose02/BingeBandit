package library

import (
	"os"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
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

func TestSidecarsSubsFolder(t *testing.T) {
	touch := func(p string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}

	// A movie folder: any subtitle in Subs/ belongs to the one video.
	movie := t.TempDir()
	for _, name := range []string{"Movie.2020.1080p.mkv", "Subs/2_English.srt", "Subs/3_Spanish.srt", "Subs/English.forced.srt", "Subs/notes.txt"} {
		touch(filepath.Join(movie, name))
	}
	got := sidecars(filepath.Join(movie, "Movie.2020.1080p.mkv"))
	type want struct {
		file, lang, title string
		forced            bool
	}
	check := func(got []store.Stream, wants []want) {
		t.Helper()
		if len(got) != len(wants) {
			t.Fatalf("got %d sidecars: %+v", len(got), got)
		}
		for i, w := range wants {
			g := got[i]
			if g.External != w.file || g.Language != w.lang || g.Title != w.title || g.Forced != w.forced || g.Index != 1000+i {
				t.Errorf("sidecar %d = %+v, want %+v", i, g, w)
			}
		}
	}
	check(got, []want{
		{"Subs/2_English.srt", "eng", "", false},
		{"Subs/3_Spanish.srt", "spa", "", false},
		{"Subs/English.forced.srt", "eng", "", true},
	})

	// A season folder: Subs/<episode>/ and Subs/<episode>.lang.srt match one
	// episode each; loose names are ambiguous and skipped.
	season := t.TempDir()
	for _, name := range []string{
		"Show.S01E01.mkv", "Show.S01E02.mkv", "Show.S01E01.en.srt",
		"Subs/Show.S01E01/2_English.srt", "Subs/Show.S01E02/2_English.srt",
		"Subs/Show.S01E01.fr.srt", "Subs/English.srt",
	} {
		touch(filepath.Join(season, name))
	}
	check(sidecars(filepath.Join(season, "Show.S01E01.mkv")), []want{
		{"Show.S01E01.en.srt", "eng", "", false},
		{"Subs/Show.S01E01/2_English.srt", "eng", "", false},
		{"Subs/Show.S01E01.fr.srt", "fre", "", false},
	})
	check(sidecars(filepath.Join(season, "Show.S01E02.mkv")), []want{
		{"Subs/Show.S01E02/2_English.srt", "eng", "", false},
	})
}

func TestVideoDir(t *testing.T) {
	for in, want := range map[string]string{
		"/m/Movie/Subs":        "/m/Movie",
		"/m/Show/S1/subs/Ep1":  "/m/Show/S1",
		"/m/Movie":             "/m/Movie",
		"/m/Show/S1/Subtitles": "/m/Show/S1",
	} {
		if got := videoDir(filepath.FromSlash(in)); got != filepath.FromSlash(want) {
			t.Errorf("videoDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// A downloaded subtitle lands beside the video without replacing one already
// there, and shows up in the file's streams.
func TestSaveSubtitle(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	video := filepath.Join(dir, "Movie.mkv")
	os.WriteFile(video, nil, 0o644)
	os.WriteFile(filepath.Join(dir, "Movie.en.srt"), []byte("mine"), 0o644)
	id, err := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Movie", MatchKey: "movie:movie"})
	if err != nil {
		t.Fatal(err)
	}
	mf := &store.MediaFile{ItemID: id, Path: video, Size: 1, Streams: []store.Stream{{Index: 0, Type: "video", Codec: "h264"}}}
	if err := st.UpsertFile(mf, 1); err != nil {
		t.Fatal(err)
	}
	mf, _ = st.FileByPath(video)
	sc := NewScanner(st, nil, "ffprobe-missing")
	streams, index, err := sc.SaveSubtitle(mf, "en", true, []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "Movie.en.srt")); string(b) != "mine" {
		t.Error("existing subtitle overwritten")
	}
	var got *store.Stream
	for i := range streams {
		if streams[i].Index == index {
			got = &streams[i]
		}
	}
	if got == nil || got.External != "Movie.en.sdh.srt" || got.Language != "eng" || !got.HearingImpaired {
		t.Fatalf("new stream %+v", got)
	}
	if again, _ := st.File(mf.ID); len(again.Streams) != 3 {
		t.Errorf("stored %d streams, want video and two subtitles", len(again.Streams))
	}
	// Same language again: numbered, still English.
	_, index, _ = sc.SaveSubtitle(mf, "en", true, []byte("again"))
	if _, err := os.Stat(filepath.Join(dir, "Movie.en.sdh.2.srt")); err != nil || index < 0 {
		t.Errorf("second download: %v, index %d", err, index)
	}
}
