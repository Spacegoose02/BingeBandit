package library

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

func tinyJPEG(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A channel folder the way Cinemask downloads it becomes a show with its
// seasons by year, episodes numbered by upload date, and the details from
// the .nfo files and images next to the videos, with no TMDB key.
func TestYouTubeChannelFromCinemaskFolder(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "YouTube")
	ch := filepath.Join(root, "Tech Tips")
	write := func(p string, data []byte) {
		t.Helper()
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(ch, "tvshow.nfo"), []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<tvshow>
  <title>Tech Tips</title>
  <plot>Weekly gadget reviews.</plot>
  <studio>YouTube</studio>
  <uniqueid type="youtube" default="true">UCabcdefghijklmnopqrstuv</uniqueid>
</tvshow>
`))
	write(filepath.Join(ch, "poster.jpg"), tinyJPEG(t, 10))
	video := func(date, title, id, plot string, withNFO bool) {
		stem := filepath.Join(ch, "Season "+date[:4], "Tech Tips - "+date+" - "+title+" ["+id+"]")
		write(stem+".mkv", []byte("x"))
		write(stem+".jpg", tinyJPEG(t, uint8(len(id)+len(title))))
		if withNFO {
			write(stem+".nfo", []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<episodedetails>
  <title>`+title+` (from nfo)</title>
  <showtitle>Tech Tips</showtitle>
  <season>`+date[:4]+`</season>
  <episode>0101</episode>
  <aired>`+date+`</aired>
  <plot>`+plot+`</plot>
  <runtime>12</runtime>
  <uniqueid type="youtube" default="true">`+id+`</uniqueid>
</episodedetails>
`))
		}
	}
	// Two on one day (they used to share an episode number), one titled
	// like an S01E02 marker, and one with no .nfo.
	video("2024-05-03", "Phone review", "aaaaaaaaaa1", "First of the day.", true)
	video("2024-05-03", "Laptop review", "bbbbbbbbbb2", "Second of the day.", true)
	video("2024-01-20", "Watching S01E02 of a show", "ccccccccc_3", "January.", true)
	video("2023-12-31", "Year end", "ddddddddd-4", "", false)
	// A normal show in the same library is left alone.
	write(filepath.Join(root, "Bluey", "Season 1", "Bluey - S01E01.mkv"), []byte("x"))

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "YouTube", Kind: "shows", Paths: []string{root}}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser("admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	meta := metadata.NewService(st)
	meta.Artwork = metadata.NewArtwork(filepath.Join(dir, "artwork"))
	sc := NewScanner(st, meta, "ffprobe-missing")
	if err := sc.ScanWait(lib.ID); err != nil {
		t.Fatal(err)
	}

	items, _, err := st.LibraryItems(u.ID, lib.ID, "title", 0, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	var show, bluey *store.Item
	for _, it := range items {
		switch it.Title {
		case "Tech Tips":
			show = it
		case "Bluey":
			bluey = it
		}
	}
	if show == nil || bluey == nil || len(items) != 2 {
		t.Fatalf("items = %v", items)
	}
	if bluey.MetadataSource != "" {
		t.Errorf("Bluey's source = %q, want TMDB's", bluey.MetadataSource)
	}
	show, err = st.Item(u.ID, show.ID)
	if err != nil {
		t.Fatal(err)
	}
	if show.MetadataSource != "youtube" || show.Unmatched {
		t.Errorf("source %q unmatched %v", show.MetadataSource, show.Unmatched)
	}
	if show.Overview != "Weekly gadget reviews." || show.Studio != "YouTube" || show.Year != 2023 {
		t.Errorf("show = %+v", show)
	}
	if !strings.HasPrefix(show.Poster, "/api/images/local/") || !strings.HasPrefix(show.Backdrop, "/api/images/local/") {
		t.Errorf("poster %q backdrop %q, want the avatar and the newest thumbnail", show.Poster, show.Backdrop)
	}

	seasons, err := st.Children(u.ID, show.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 2 || *seasons[0].SeasonNumber != 2023 || *seasons[1].SeasonNumber != 2024 {
		t.Fatalf("seasons = %v", seasons)
	}
	type ep struct {
		n                int
		title, plot, day string
	}
	got := func(season *store.Item) []ep {
		eps, err := st.Children(u.ID, season.ID)
		if err != nil {
			t.Fatal(err)
		}
		var out []ep
		for _, e := range eps {
			if !strings.HasPrefix(e.Backdrop, "/api/images/local/") {
				t.Errorf("%q has no thumbnail: %q", e.Title, e.Backdrop)
			}
			out = append(out, ep{*e.EpisodeNumber, e.Title, e.Overview, e.ReleaseDate})
		}
		return out
	}
	want2023 := []ep{{1, "Year end", "", "2023-12-31"}}
	want2024 := []ep{
		{1, "Watching S01E02 of a show (from nfo)", "January.", "2024-01-20"},
		{2, "Laptop review (from nfo)", "Second of the day.", "2024-05-03"},
		{3, "Phone review (from nfo)", "First of the day.", "2024-05-03"},
	}
	check := func(season *store.Item, want []ep) {
		t.Helper()
		g := got(season)
		if len(g) != len(want) {
			t.Fatalf("season %s = %+v, want %+v", season.Title, g, want)
		}
		for i := range want {
			if g[i] != want[i] {
				t.Errorf("season %s episode %d = %+v, want %+v", season.Title, i, g[i], want[i])
			}
		}
	}
	check(seasons[0], want2023)
	check(seasons[1], want2024)

	// A video downloaded later from earlier in the year moves the others along.
	video("2024-02-14", "Valentine", "eeeeeeeeee5", "February.", true)
	if err := sc.ScanWait(lib.ID); err != nil {
		t.Fatal(err)
	}
	want2024 = append([]ep{want2024[0], {2, "Valentine (from nfo)", "February.", "2024-02-14"}},
		ep{3, want2024[1].title, want2024[1].plot, want2024[1].day}, ep{4, want2024[2].title, want2024[2].plot, want2024[2].day})
	check(seasons[1], want2024)
}
