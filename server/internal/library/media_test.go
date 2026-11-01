package library

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

// audio makes a short tagged audio file with ffmpeg.
func audio(t *testing.T, path string, tags ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1"}
	for i := 0; i+1 < len(tags); i += 2 {
		args = append(args, "-metadata", tags[i]+"="+tags[i+1])
	}
	if filepath.Ext(path) == ".m4b" {
		args = append(args, "-c:a", "aac", "-f", "mp4")
	}
	if out, err := exec.Command("ffmpeg", append(args, path)...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %s: %v\n%s", path, err, out)
	}
}

func needFFmpeg(t *testing.T) {
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
}

func mediaLibrary(t *testing.T, kind string) (*store.Store, *store.Library, *store.User, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, kind)
	os.MkdirAll(root, 0o755)
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	lib := &store.Library{Name: "My " + kind, Kind: kind, Paths: []string{root}}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser("admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	return st, lib, u, root
}

func titlesOf(items []*store.Item) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func TestScanMusicLibrary(t *testing.T) {
	needFFmpeg(t)
	st, lib, u, root := mediaLibrary(t, store.KindMusic)
	// Tagged: the tags win over the folder names.
	audio(t, filepath.Join(root, "x", "Kid A", "02 track.mp3"),
		"title", "The National Anthem", "artist", "Radiohead", "album_artist", "Radiohead", "album", "Kid A",
		"track", "2/10", "date", "2000-10-02", "genre", "Alternative; Electronic")
	audio(t, filepath.Join(root, "x", "Kid A", "01 track.mp3"),
		"title", "Everything in Its Right Place", "artist", "Radiohead", "album", "Kid A", "album_artist", "Radiohead", "track", "1")
	// Untagged: Artist/Album (Year)/CD2/03 - Title.
	audio(t, filepath.Join(root, "Pink Floyd", "The Wall (1979)", "CD2", "03 - Comfortably Numb.flac"))
	// A compilation folder: one album though each track names its artist.
	audio(t, filepath.Join(root, "Now 1", "01.mp3"), "title", "A", "artist", "Band A", "album", "Now 1", "compilation", "1")
	audio(t, filepath.Join(root, "Now 1", "02.mp3"), "title", "B", "artist", "Band B", "album", "Now 1", "compilation", "1")

	sc := NewScanner(st, nil, "ffprobe")
	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	albums, total, err := st.FilteredLibraryItems(u.ID, lib.ID, "title", 0, 50, nil, store.LibraryFilter{Kind: "album"})
	if err != nil {
		t.Fatal(err)
	}
	if got := titlesOf(albums); total != 3 || got[0] != "Kid A" || got[1] != "Now 1" || got[2] != "The Wall (1979)" {
		t.Fatalf("albums = %v (%d)", got, total)
	}
	kidA := albums[0]
	if kidA.Year != 2000 || kidA.ReleaseDate != "2000-10-02" || kidA.Studio != "Radiohead" || len(kidA.Genres) != 2 {
		t.Errorf("Kid A details: year %d date %q artist %q genres %v", kidA.Year, kidA.ReleaseDate, kidA.Studio, kidA.Genres)
	}
	if kidA.Poster != store.CoverURL(kidA.ID) {
		t.Errorf("album poster = %q", kidA.Poster)
	}
	tracks, err := st.Tracks(u.ID, kidA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 || tracks[0].Item.Title != "Everything in Its Right Place" || tracks[1].Item.Title != "The National Anthem" {
		t.Fatalf("Kid A tracks = %+v", tracks)
	}
	if tracks[0].File.FileID == 0 || tracks[0].File.Duration < 0.5 || tracks[0].File.AudioCodec != "mp3" {
		t.Errorf("track file = %+v", tracks[0].File)
	}
	if now := albums[1]; now.Studio != "Various Artists" {
		t.Errorf("compilation artist = %q", now.Studio)
	}
	wall := albums[2]
	wt, _ := st.Tracks(u.ID, wall.ID)
	if len(wt) != 1 || wt[0].Item.Title != "Comfortably Numb" || *wt[0].Item.SeasonNumber != 2 || *wt[0].Item.EpisodeNumber != 3 || wall.Year != 1979 {
		t.Fatalf("The Wall: %+v year %d", wt, wall.Year)
	}
	artists, _, _ := st.FilteredLibraryItems(u.ID, lib.ID, "title", 0, 50, nil, store.LibraryFilter{Kind: "artist"})
	if got := titlesOf(artists); len(got) != 3 || got[0] != "Pink Floyd" || got[1] != "Radiohead" || got[2] != "Various Artists" {
		t.Fatalf("artists = %v", got)
	}
	if libs, _ := st.Libraries(); libs[0].ItemCount != 3 {
		t.Errorf("library count = %d, want 3 albums", libs[0].ItemCount)
	}
	// Videos never show up in a music library's grid, nor music in search.
	if res, _ := st.Search(u.ID, "Kid", 10); len(res) != 0 {
		t.Errorf("video search found %v", titlesOf(res))
	}
	if res, _ := st.SearchKinds(u.ID, "radiohead", []string{"artist", "album"}, 10); len(res) != 2 {
		t.Errorf("music search found %v", titlesOf(res))
	}

	// A rescan keeps everything; a deleted album takes its artist with it.
	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(root, "Pink Floyd"))
	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	artists, _, _ = st.FilteredLibraryItems(u.ID, lib.ID, "title", 0, 50, nil, store.LibraryFilter{Kind: "artist"})
	if got := titlesOf(artists); len(got) != 2 || got[0] != "Radiohead" {
		t.Fatalf("after deleting Pink Floyd: artists = %v", got)
	}
}

func TestScanAudiobookLibrary(t *testing.T) {
	needFFmpeg(t)
	st, lib, u, root := mediaLibrary(t, store.KindAudiobooks)
	audio(t, filepath.Join(root, "Frank Herbert", "Dune", "02 Part Two.mp3"))
	audio(t, filepath.Join(root, "Frank Herbert", "Dune", "01 Part One.mp3"))
	audio(t, filepath.Join(root, "Project Hail Mary.m4b"), "title", "Project Hail Mary", "artist", "Andy Weir", "date", "2021")

	sc := NewScanner(st, nil, "ffprobe")
	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	books, _, err := st.FilteredLibraryItems(u.ID, lib.ID, "title", 0, 50, nil, store.LibraryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if got := titlesOf(books); len(got) != 2 || got[0] != "Dune" || got[1] != "Project Hail Mary" {
		t.Fatalf("books = %v", got)
	}
	if books[0].Studio != "Frank Herbert" || books[1].Studio != "Andy Weir" || books[1].Year != 2021 {
		t.Errorf("authors %q %q, year %d", books[0].Studio, books[1].Studio, books[1].Year)
	}
	ch, _ := st.Tracks(u.ID, books[0].ID)
	if len(ch) != 2 || ch[0].Item.Title != "Part One" || ch[1].Item.Title != "Part Two" {
		t.Fatalf("Dune chapters = %v", ch)
	}
	// Listening to part of a chapter puts the book on Continue Listening.
	if _, err := st.SaveProgress(u.ID, ch[0].Item.ID, 0.2, 1); err != nil {
		t.Fatal(err)
	}
	if cl, _ := st.ContinueListening(u.ID, 10); len(cl) != 0 {
		t.Errorf("under 15 s counted: %v", titlesOf(cl))
	}
	st.SetWatched(u.ID, []int64{ch[0].Item.ID}, true)
	cl, err := st.ContinueListening(u.ID, 10)
	if err != nil || len(cl) != 1 || cl[0].ID != books[0].ID {
		t.Fatalf("continue listening = %v %v", titlesOf(cl), err)
	}
	st.SetWatched(u.ID, []int64{ch[1].Item.ID}, true)
	if cl, _ := st.ContinueListening(u.ID, 10); len(cl) != 0 {
		t.Errorf("a finished book stays on: %v", titlesOf(cl))
	}
}

func TestScanPhotoLibrary(t *testing.T) {
	needFFmpeg(t)
	st, lib, u, root := mediaLibrary(t, store.KindPhotos)
	img := func(p string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=red:s=64x48", "-frames:v", "1", p).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	img(filepath.Join(root, "Beach 2024", "IMG_0002.jpg"))
	img(filepath.Join(root, "Beach 2024", "IMG_0001.jpg"))
	img(filepath.Join(root, "loose.png"))
	writeFile(t, filepath.Join(root, "Beach 2024", "notes.txt"))
	writeFile(t, filepath.Join(root, ".thumbs", "x.jpg"))

	sc := NewScanner(st, nil, "ffprobe")
	if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
		t.Fatal(err)
	}
	albums, _, _ := st.FilteredLibraryItems(u.ID, lib.ID, "title", 0, 50, nil, store.LibraryFilter{})
	if got := titlesOf(albums); len(got) != 2 || got[0] != "Beach 2024" || got[1] != "My photos" {
		t.Fatalf("photo albums = %v", got)
	}
	photos, _ := st.Tracks(u.ID, albums[0].ID)
	if len(photos) != 2 || photos[0].Item.Title != "IMG_0001" || photos[0].File.Width != 64 {
		t.Fatalf("photos = %+v", photos)
	}
	if paths, isPhoto, err := st.CoverSources(albums[0].ID); err != nil || !isPhoto || filepath.Base(paths[0]) != "IMG_0001.jpg" {
		t.Errorf("cover source = %q %v %v", paths, isPhoto, err)
	}
	// A photo's own cover is itself.
	if paths, _, _ := st.CoverSources(photos[1].Item.ID); filepath.Base(paths[0]) != "IMG_0002.jpg" {
		t.Errorf("photo cover source = %q", paths)
	}
}
