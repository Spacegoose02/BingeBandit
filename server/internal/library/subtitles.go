package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bingebandit/internal/store"
)

var subtitleExts = map[string]string{".srt": "subrip", ".ass": "ass", ".ssa": "ssa", ".vtt": "webvtt"}

func isSubtitleFile(name string) bool {
	_, ok := subtitleExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

// languages maps names and ISO 639-1/639-2 codes to the 639-2 codes ffprobe uses.
var languages = func() map[string]string {
	table := [][]string{
		{"eng", "en", "english"}, {"spa", "es", "spanish", "español", "espanol"}, {"fre", "fr", "fra", "french", "français"},
		{"ger", "de", "deu", "german", "deutsch"}, {"ita", "it", "italian"}, {"por", "pt", "portuguese", "pob", "ptbr"},
		{"dut", "nl", "nld", "dutch"}, {"swe", "sv", "swedish"}, {"nor", "no", "nb", "nob", "norwegian"},
		{"dan", "da", "danish"}, {"fin", "fi", "finnish"}, {"pol", "pl", "polish"}, {"rus", "ru", "russian"},
		{"ukr", "uk", "ukrainian"}, {"cze", "cs", "ces", "czech"}, {"gre", "el", "ell", "greek"},
		{"tur", "tr", "turkish"}, {"ara", "ar", "arabic"}, {"heb", "he", "hebrew"}, {"hin", "hi", "hindi"},
		{"jpn", "ja", "japanese"}, {"kor", "ko", "korean"}, {"chi", "zh", "zho", "chinese", "chs", "cht"},
		{"tha", "th", "thai"}, {"vie", "vi", "vietnamese"}, {"ind", "id", "indonesian"}, {"hun", "hu", "hungarian"},
		{"rum", "ro", "ron", "romanian"}, {"bul", "bg", "bulgarian"}, {"hrv", "hr", "croatian"}, {"srp", "sr", "serbian"},
	}
	m := map[string]string{}
	for _, row := range table {
		for _, alias := range row {
			m[alias] = row[0]
		}
	}
	return m
}()

// subsDirs are the folder names release downloads keep subtitles in.
var subsDirs = map[string]bool{"subs": true, "sub": true, "subtitles": true}

func isSubsDir(name string) bool { return subsDirs[strings.ToLower(name)] }

// sidecars finds subtitle files for a video:
//   - next to it, named after it: Movie.srt, Movie.en.srt, Movie.eng.forced.srt, Movie.English.SDH.srt
//   - in a Subs/ folder beside it, named after it the same way
//   - in Subs/, any name (2_English.srt), when the video is the only one in its folder
//   - in Subs/<video name>/, any name (TV packs: Subs/Show.S01E01/2_English.srt)
func sidecars(videoPath string) []store.Stream {
	dir := filepath.Dir(videoPath)
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath)))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []store.Stream
	add := func(rel, rest string) {
		codec := subtitleExts[strings.ToLower(filepath.Ext(rel))]
		st := store.Stream{Index: store.ExternalIndexBase + len(out), Type: "subtitle", Codec: codec, External: rel}
		var title []string
		for _, tok := range strings.FieldsFunc(rest, func(r rune) bool { return strings.ContainsRune("._- ", r) }) {
			switch {
			case tok == "forced":
				st.Forced = true
			case tok == "sdh" || tok == "cc" || tok == "hi":
				st.HearingImpaired = true
			case tok == "default":
				st.Default = true
			case languages[tok] != "" && st.Language == "":
				st.Language = languages[tok]
			case isNumber(tok):
				// track numbers like "2_English.srt"
			default:
				title = append(title, tok)
			}
		}
		st.Title = strings.Join(title, " ")
		out = append(out, st)
	}
	// named reports the tags after the video's name in a subtitle file name,
	// and whether the file is named after the video at all. Like Plex/Jellyfin,
	// only "Movie.<tags>.srt" counts, so the sequel's "Movie 2.srt" never
	// attaches to "Movie.mkv".
	named := func(name string) (string, bool) {
		stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		if !strings.HasPrefix(stem, base) {
			return "", false
		}
		rest := stem[len(base):]
		return rest, rest == "" || rest[0] == '.'
	}

	var subs []string
	videos := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if isSubsDir(name) {
				subs = append(subs, name)
			}
			continue
		}
		if isVideo(name) && !isSample(name) && !isExtraFile(name) {
			videos++
		}
		if !isSubtitleFile(name) {
			continue
		}
		if rest, ok := named(name); ok {
			add(name, rest)
		}
	}
	for _, sd := range subs {
		files, err := os.ReadDir(filepath.Join(dir, sd))
		if err != nil {
			continue
		}
		for _, e := range files {
			name := e.Name()
			if e.IsDir() {
				if strings.ToLower(name) == base {
					inner, _ := os.ReadDir(filepath.Join(dir, sd, name))
					for _, f := range inner {
						if !f.IsDir() && isSubtitleFile(f.Name()) {
							rest, ok := named(f.Name())
							if !ok {
								rest = strings.ToLower(strings.TrimSuffix(f.Name(), filepath.Ext(f.Name())))
							}
							add(sd+"/"+name+"/"+f.Name(), rest)
						}
					}
				}
				continue
			}
			if !isSubtitleFile(name) {
				continue
			}
			if rest, ok := named(name); ok {
				add(sd+"/"+name, rest)
			} else if videos == 1 {
				add(sd+"/"+name, strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name))))
			}
		}
	}
	return out
}

func isNumber(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// withSidecars replaces the external streams in streams with side.
func withSidecars(streams, side []store.Stream) []store.Stream {
	out := make([]store.Stream, 0, len(streams)+len(side))
	for _, s := range streams {
		if s.External == "" {
			out = append(out, s)
		}
	}
	return append(out, side...)
}

func sameSidecars(streams, side []store.Stream) bool {
	var cur []store.Stream
	for _, s := range streams {
		if s.External != "" {
			cur = append(cur, s)
		}
	}
	a, _ := json.Marshal(cur)
	b, _ := json.Marshal(side)
	return string(a) == string(b)
}

// LanguageCode turns a language name or ISO 639-1/639-2 code into the 639-2
// code ffprobe uses ("" when unknown).
func LanguageCode(s string) string { return languages[strings.ToLower(strings.TrimSpace(s))] }

// SaveSubtitle writes a downloaded subtitle beside the video as
// "<video>.<lang>.srt" (numbered when that name is taken, so nothing is
// overwritten), then refreshes the file's subtitle list. It returns the
// file's streams and the index of the new one.
func (s *Scanner) SaveSubtitle(mf *store.MediaFile, lang string, hearingImpaired bool, data []byte) ([]store.Stream, int, error) {
	base := strings.TrimSuffix(filepath.Base(mf.Path), filepath.Ext(mf.Path))
	tags := lang
	if hearingImpaired {
		tags += ".sdh"
	}
	name := base + "." + tags + ".srt"
	for n := 2; ; n++ {
		if _, err := os.Stat(filepath.Join(filepath.Dir(mf.Path), name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s.%s.%d.srt", base, tags, n)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(mf.Path), name), data, 0o644); err != nil {
		return nil, 0, fmt.Errorf("could not save the subtitles beside the video (is the media folder read-only?): %w", err)
	}
	streams := withSidecars(mf.Streams, sidecars(mf.Path))
	if err := s.st.UpdateStreams(mf.ID, streams); err != nil {
		return nil, 0, err
	}
	for _, st := range streams {
		if st.External == name {
			return streams, st.Index, nil
		}
	}
	return streams, -1, nil
}
