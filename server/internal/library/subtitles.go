package library

import (
	"encoding/json"
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

// sidecars finds subtitle files next to a video whose names start with the
// video's name: Movie.srt, Movie.en.srt, Movie.eng.forced.srt, Movie.English.SDH.srt.
func sidecars(videoPath string) []store.Stream {
	dir := filepath.Dir(videoPath)
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath)))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []store.Stream
	for _, e := range entries {
		name := e.Name()
		codec, ok := subtitleExts[strings.ToLower(filepath.Ext(name))]
		if e.IsDir() || !ok {
			continue
		}
		stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		if !strings.HasPrefix(stem, base) {
			continue
		}
		rest := stem[len(base):]
		// Like Plex/Jellyfin, only "Movie.<tags>.srt" counts, so the
		// sequel's "Movie 2.srt" never attaches to "Movie.mkv".
		if rest != "" && rest[0] != '.' {
			continue
		}
		st := store.Stream{Index: store.ExternalIndexBase + len(out), Type: "subtitle", Codec: codec, External: name}
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
			default:
				title = append(title, tok)
			}
		}
		st.Title = strings.Join(title, " ")
		out = append(out, st)
	}
	return out
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
