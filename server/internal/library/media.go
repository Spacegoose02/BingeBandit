package library

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

// Music, audiobook and photo libraries. Their items come from each file's
// tags (ffprobe reads ID3, Vorbis comments and MP4 atoms alike), with the
// folders as the fallback: Artist/Album/01 - Track.flac, Author/Book/01.mp3
// or Book.m4b, and any folder of photos.

var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".m4b": true, ".aac": true, ".ogg": true, ".oga": true, ".opus": true,
	".wav": true, ".wma": true, ".aif": true, ".aiff": true, ".alac": true, ".ape": true, ".wv": true, ".mka": true,
	".dsf": true, ".mp2": true,
}

// imageExts are the photo formats browsers can show (HEIC and RAW can't).
var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true, ".avif": true,
}

func isAudio(name string) bool { return audioExts[strings.ToLower(filepath.Ext(name))] }
func isImage(name string) bool { return imageExts[strings.ToLower(filepath.Ext(name))] }

// wanted says whether a file belongs in a library of a non-video kind.
// Folder artwork (cover.jpg) is a photo only in a photo library.
func wanted(kind, name string) bool {
	switch kind {
	case store.KindMusic, store.KindAudiobooks:
		return isAudio(name)
	case store.KindPhotos:
		return isImage(name) && !strings.HasPrefix(name, ".")
	}
	return false
}

// mediaSkipDirs are NAS and system folders. Video's extras folders ("Other",
// "Extras", "Shorts") are real albums in a music library, so only these go.
var mediaSkipDirs = map[string]bool{
	"@eadir": true, "#recycle": true, "$recycle.bin": true, "system volume information": true, ".trash": true,
}

func skipMediaDir(name string) bool {
	return strings.HasPrefix(name, ".") || mediaSkipDirs[strings.ToLower(name)]
}

var (
	reDiscDir  = regexp.MustCompile(`(?i)^(?:cd|disc|disk|part)[\s._\-]*(\d{1,2})$`)
	reTrackNum = regexp.MustCompile(`^\s*(\d{1,4})`)
	reYear     = regexp.MustCompile(`(1[89]\d\d|20\d\d)`)
	reTrackPfx = regexp.MustCompile(`^\s*(?:\d{1,2}[\s._\-]+)?\d{1,3}(?:\s*[-._]\s*|\s+)`)
)

// tag returns the first of the named tags the file has.
func tag(tags map[string]string, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(tags[n]); v != "" {
			return v
		}
	}
	return ""
}

// tagNumber reads "3" or "3/12".
func tagNumber(v string) int {
	if m := reTrackNum.FindStringSubmatch(v); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// tagDate reads a year and, when the tag has one, a full date.
func tagDate(v string) (year int, date string) {
	if len(v) >= 10 {
		if t, err := time.Parse("2006-01-02", v[:10]); err == nil {
			return t.Year(), v[:10]
		}
	}
	if m := reYear.FindString(v); m != "" {
		year, _ = strconv.Atoi(m)
	}
	return year, ""
}

// tagGenres splits "Rock; Indie" or "Rock/Indie".
func tagGenres(v string) []string {
	var out []string
	for _, g := range strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == '/' || r == ',' || r == '\x00' }) {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// trackTitle is a file's name without its track number ("01 - Intro" is
// "Intro"); a name that is only a number stays as it is.
func trackTitle(name string) string {
	if t := strings.TrimSpace(reTrackPfx.ReplaceAllString(name, "")); t != "" {
		return t
	}
	return name
}

func runtimeMinutes(seconds float64) int { return int(math.Ceil(seconds / 60)) }

// relKey is a path below the library folder, for match keys.
func relKey(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return strings.ToLower(filepath.ToSlash(rel))
}

// processTagged adds one file of a music, audiobook or photo library. An
// unchanged file keeps its item; a new or changed one is probed and its tags
// decide where it goes.
func (s *Scanner) processTagged(ctx context.Context, lib *store.Library, f found, gen int64) error {
	info, err := os.Stat(f.path)
	if err != nil {
		return err
	}
	existing, err := s.st.FileByPath(f.path)
	if err == nil && existing.Size == info.Size() && existing.MTime == info.ModTime().Unix() && existing.ProbedAt > 0 && existing.ItemID != 0 {
		return s.st.TouchFile(existing.ID, existing.ItemID, gen)
	}
	mf := &store.MediaFile{Path: f.path, Size: info.Size(), MTime: info.ModTime().Unix()}
	if err := media.Probe(ctx, s.ffprobe, mf); err != nil {
		log.Printf("probe: %v", err)
		mf.Container = strings.TrimPrefix(strings.ToLower(filepath.Ext(f.path)), ".")
		mf.Tags = map[string]string{}
	}
	switch lib.Kind {
	case store.KindMusic:
		mf.ItemID, err = s.trackItem(lib, f, mf)
	case store.KindAudiobooks:
		mf.ItemID, err = s.chapterItem(lib, f, mf)
	case store.KindPhotos:
		mf.ItemID, err = s.photoItem(lib, f, mf, info.ModTime())
	default:
		err = fmt.Errorf("unknown library kind %q", lib.Kind)
	}
	if err != nil {
		return err
	}
	return s.st.UpsertFile(mf, gen)
}

// discFolder splits Album/CD2 into (Album, 2); other folders are (dir, 0).
func discFolder(root, dir string) (string, int) {
	if dir != root {
		if m := reDiscDir.FindStringSubmatch(filepath.Base(dir)); m != nil {
			n, _ := strconv.Atoi(m[1])
			return filepath.Dir(dir), n
		}
	}
	return dir, 0
}

// trackItem files a song under its artist and album: the album artist tag
// (else the artist), else the folder above the album's.
func (s *Scanner) trackItem(lib *store.Library, f found, mf *store.MediaFile) (int64, error) {
	t := mf.Tags
	name := strings.TrimSuffix(filepath.Base(f.path), filepath.Ext(f.path))
	albumDir, discNo := discFolder(f.root, filepath.Dir(f.path))
	inFolder := albumDir != f.root

	album := tag(t, "album")
	if album == "" && inFolder {
		album = filepath.Base(albumDir)
	}
	if album == "" {
		album = "Unknown Album"
	}
	trackArtist := tag(t, "artist")
	artist := tag(t, "album_artist", "albumartist", "album artist")
	if artist == "" && tag(t, "compilation") == "1" {
		artist = "Various Artists"
	}
	if artist == "" {
		if parent := filepath.Dir(albumDir); inFolder && parent != f.root && strings.HasPrefix(parent, f.root) {
			artist = filepath.Base(parent)
		}
	}
	if artist == "" {
		artist = trackArtist
	}
	if artist == "" {
		artist = "Unknown Artist"
	}
	if trackArtist == "" {
		trackArtist = artist
	}
	year, date := tagDate(tag(t, "date", "year", "originaldate", "tdor"))
	if year == 0 && inFolder {
		_, year, _ = parseTitleYear(filepath.Base(albumDir))
	}

	artistID, err := s.st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "artist", Title: artist, MatchKey: "artist:" + normKey(artist)})
	if err != nil {
		return 0, err
	}
	if err := s.st.SetTaggedDetails(artistID, store.TaggedDetails{Title: artist}); err != nil {
		return 0, err
	}
	// An album in a folder is that folder, so a compilation whose tracks
	// name different artists stays one album; loose files group by name.
	albumKey := "album:" + normKey(artist) + ":" + normKey(album)
	if inFolder {
		albumKey = "album:" + relKey(f.root, albumDir)
	}
	albumID, err := s.st.UpsertItem(&store.Item{
		LibraryID: lib.ID, Kind: "album", ParentID: artistID, ShowID: artistID, Title: album, Year: year, MatchKey: albumKey,
	})
	if err != nil {
		return 0, err
	}
	if err := s.st.SetTaggedDetails(albumID, store.TaggedDetails{
		Title: album, Year: year, ReleaseDate: date, Genres: tagGenres(tag(t, "genre")), Artist: artist,
	}); err != nil {
		return 0, err
	}

	disc := tagNumber(tag(t, "disc", "discnumber", "disk", "tpos"))
	if disc == 0 {
		disc = max(discNo, 1)
	}
	number := tagNumber(tag(t, "track", "tracknumber", "trck"))
	if number == 0 {
		number, _ = parseLeadingNumber(name)
	}
	title := tag(t, "title")
	if title == "" {
		title = trackTitle(name)
	}
	id, err := s.st.UpsertItem(&store.Item{
		LibraryID: lib.ID, Kind: "track", ParentID: albumID, ShowID: artistID, Title: title, Year: year,
		SeasonNumber: &disc, EpisodeNumber: &number, MatchKey: "track:" + relKey(f.root, f.path),
	})
	if err != nil {
		return 0, err
	}
	return id, s.st.SetTaggedDetails(id, store.TaggedDetails{
		Title: title, Year: year, Artist: trackArtist, Runtime: runtimeMinutes(mf.Duration),
	})
}

// chapterItem files part of an audiobook under its book: the folder it's in
// (Author/Book/01.mp3), or the file itself when it sits in the library
// folder (Book.m4b).
func (s *Scanner) chapterItem(lib *store.Library, f found, mf *store.MediaFile) (int64, error) {
	t := mf.Tags
	name := strings.TrimSuffix(filepath.Base(f.path), filepath.Ext(f.path))
	bookDir, discNo := discFolder(f.root, filepath.Dir(f.path))
	single := bookDir == f.root

	book := tag(t, "album")
	if book == "" && !single {
		book = filepath.Base(bookDir)
	}
	if book == "" {
		book = tag(t, "title")
	}
	if book == "" {
		book = name
	}
	author := tag(t, "album_artist", "albumartist", "artist", "author", "composer")
	if author == "" && !single {
		if parent := filepath.Dir(bookDir); parent != f.root && strings.HasPrefix(parent, f.root) {
			author = filepath.Base(parent)
		}
	}
	year, date := tagDate(tag(t, "date", "year"))
	if year == 0 {
		_, year, _ = parseTitleYear(book)
	}
	bookKey := "book:" + relKey(f.root, bookDir)
	if single {
		bookKey = "book:" + relKey(f.root, strings.TrimSuffix(f.path, filepath.Ext(f.path)))
	}
	bookID, err := s.st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "audiobook", Title: book, Year: year, MatchKey: bookKey})
	if err != nil {
		return 0, err
	}
	if err := s.st.SetTaggedDetails(bookID, store.TaggedDetails{
		Title: book, Year: year, ReleaseDate: date, Genres: tagGenres(tag(t, "genre")), Artist: author,
	}); err != nil {
		return 0, err
	}
	disc := max(tagNumber(tag(t, "disc", "discnumber")), discNo, 1)
	number := tagNumber(tag(t, "track", "tracknumber"))
	if number == 0 {
		number, _ = parseLeadingNumber(name)
	}
	title := tag(t, "title")
	if title == "" || (single && title == book) {
		title = trackTitle(name)
	}
	if single && (title == "" || title == book) {
		title = book
	}
	id, err := s.st.UpsertItem(&store.Item{
		LibraryID: lib.ID, Kind: "chapter", ParentID: bookID, ShowID: bookID, Title: title, Year: year,
		SeasonNumber: &disc, EpisodeNumber: &number, MatchKey: "chapter:" + relKey(f.root, f.path),
	})
	if err != nil {
		return 0, err
	}
	return id, s.st.SetTaggedDetails(id, store.TaggedDetails{Title: title, Artist: author, Runtime: runtimeMinutes(mf.Duration)})
}

// photoItem files a photo under its folder. Photos in the library folder
// itself go in an album named after the library.
func (s *Scanner) photoItem(lib *store.Library, f found, mf *store.MediaFile, mtime time.Time) (int64, error) {
	dir := filepath.Dir(f.path)
	albumTitle := lib.Name
	if dir != f.root {
		albumTitle = filepath.Base(dir)
	}
	taken := mtime
	if v := tag(mf.Tags, "creation_time", "date", "datetimeoriginal"); v != "" {
		for _, layout := range []string{time.RFC3339Nano, "2006:01:02 15:04:05", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, v); err == nil {
				taken = t
				break
			}
		}
	}
	albumID, err := s.st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "photoalbum", Title: albumTitle, MatchKey: "photos:" + relKey(f.root, dir)})
	if err != nil {
		return 0, err
	}
	// An album's year is its newest photo's (set as photos come in).
	if err := s.st.SetPhotoAlbumYear(albumID, taken.Year()); err != nil {
		return 0, err
	}
	title := strings.TrimSuffix(filepath.Base(f.path), filepath.Ext(f.path))
	id, err := s.st.UpsertItem(&store.Item{
		LibraryID: lib.ID, Kind: "photo", ParentID: albumID, ShowID: albumID, Title: title, Year: taken.Year(),
		MatchKey: "photo:" + relKey(f.root, f.path),
	})
	if err != nil {
		return 0, err
	}
	return id, s.st.SetTaggedDetails(id, store.TaggedDetails{Title: title, Year: taken.Year(), ReleaseDate: taken.Format("2006-01-02")})
}
