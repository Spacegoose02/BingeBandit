package library

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true, ".wmv": true, ".ts": true, ".m2ts": true,
	".webm": true, ".mpg": true, ".mpeg": true, ".flv": true, ".ogm": true, ".divx": true, ".3gp": true,
}

// Folders that hold extras, NAS metadata or system files rather than the main video.
var skipDirs = map[string]bool{
	"extras": true, "featurettes": true, "behind the scenes": true, "deleted scenes": true, "trailers": true,
	"interviews": true, "scenes": true, "shorts": true, "other": true, "samples": true, "sample": true,
	"@eadir": true, "#recycle": true, "$recycle.bin": true, "system volume information": true, ".trash": true,
}

var (
	reSample   = regexp.MustCompile(`(?i)(^|[\W_])sample([\W_]|$)`)
	reTMDBHint = regexp.MustCompile(`(?i)[\[{(]\s*tmdb(?:id)?[-=: ](\d+)\s*[\]})]`)
	reTVDBHint = regexp.MustCompile(`(?i)[\[{(]\s*tvdb(?:id)?[-=: ](\d+)\s*[\]})]`)
	reIMDbHint = regexp.MustCompile(`(?i)[\[{(]\s*imdb(?:id)?[-=: ](tt\d+)\s*[\]})]`)
	reBrackets = regexp.MustCompile(`[\[{][^\]}]*[\]}]`)
	reJunk     = regexp.MustCompile(`(?i)[\s._\-(\[](2160p|1080p|720p|576p|480p|4k|uhd|bluray|blu-ray|bdrip|brrip|web-?dl|webrip|web|hdtv|dvdrip|remux|x264|x265|h\.?264|h\.?265|hevc|avc|hdr10?|dv|atmos|truehd|dts|ddp?5\.1|aac|proper|repack|extended|unrated|imax|criterion)([\s._\-)\]]|$).*$`)
	reDigits   = regexp.MustCompile(`\d+`)
	reEpisode  = regexp.MustCompile(`(?i)s(\d{1,3})[ ._-]?e(\d{1,4})`)
	reEpisodeX = regexp.MustCompile(`(?i)(?:^|[\s._\-])(\d{1,2})x(\d{2,3})(?:[\s._\-]|$)`)
	reSeason   = regexp.MustCompile(`(?i)^(?:season|series|staffel|saison|s)[ ._-]*(\d{1,3})$`)
	reLeadNum  = regexp.MustCompile(`^(\d{1,3})(?:[\s._\-]|$)`)
	reSpaces   = regexp.MustCompile(`\s{2,}`)
)

func isVideo(name string) bool { return videoExts[strings.ToLower(filepath.Ext(name))] }

func isSample(name string) bool {
	return reSample.MatchString(strings.TrimSuffix(name, filepath.Ext(name)))
}

func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || skipDirs[strings.ToLower(name)]
}

// parseTitleYear splits "Blade.Runner.2049.2017.1080p.BluRay" into
// ("Blade Runner 2049", 2017). The last plausible year wins, as long as it
// is not the first token (so "1917 (2019)" and "2012" both work).
func parseTitleYear(name string) (title string, year int, tmdbID int64) {
	if m := reTMDBHint.FindStringSubmatch(name); m != nil {
		tmdbID, _ = strconv.ParseInt(m[1], 10, 64)
		name = strings.Replace(name, m[0], " ", 1)
	}
	name = reTVDBHint.ReplaceAllString(name, " ")
	name = reIMDbHint.ReplaceAllString(name, " ")
	cut := -1
	for _, loc := range reDigits.FindAllStringIndex(name, -1) {
		tok := name[loc[0]:loc[1]]
		if len(tok) != 4 || loc[0] == 0 {
			continue
		}
		n, _ := strconv.Atoi(tok)
		if n < 1900 || n > 2100 {
			continue
		}
		if !isSep(rune(name[loc[0]-1])) || (loc[1] < len(name) && !isSep(rune(name[loc[1]]))) {
			continue
		}
		year, cut = n, loc[0]
	}
	if cut > 0 {
		name = name[:cut]
	}
	return cleanTitle(name), year, tmdbID
}

// idHints reads TheTVDB and IMDb ids from a folder or file name, written the
// way Plex, Sonarr and Radarr do: "Show (2019) {tvdb-12345}",
// "Movie (2010) [imdbid-tt1375666]".
func idHints(name string) (tvdbID int64, imdbID string) {
	if m := reTVDBHint.FindStringSubmatch(name); m != nil {
		tvdbID, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := reIMDbHint.FindStringSubmatch(name); m != nil {
		imdbID = strings.ToLower(m[1])
	}
	return tvdbID, imdbID
}

func isSep(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("._-()[]{}", r)
}

func cleanTitle(s string) string {
	s = reBrackets.ReplaceAllString(s, " ")
	s = reJunk.ReplaceAllString(s, "")
	// Dots/underscores are word separators only in scene-style names without spaces.
	if !strings.Contains(s, " ") {
		s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	} else {
		s = strings.ReplaceAll(s, "_", " ")
	}
	s = strings.Trim(s, " -.([")
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

// reAirDate matches date-named episodes ("Show - 2026-10-23" or, from the
// DVR, "Show - 2026-10-23 19.30").
var reAirDate = regexp.MustCompile(`(?:^|[\s._\-])((?:19|20)\d{2})[-.](\d{2})[-.](\d{2})(?:[ ._T](\d{2})[.:](\d{2}))?`)

// parseEpisode finds S01E02 / 1x02 markers. prefix is the text before the
// marker (often the show name), rest is what follows (often the episode title).
// Date-named episodes (daily shows, DVR recordings) use the year as the season
// and MMDDhhmm as the episode number, so they sort by air date.
func parseEpisode(name string) (season, episode int, prefix, rest string, ok bool) {
	for _, re := range []*regexp.Regexp{reEpisode, reEpisodeX} {
		if loc := re.FindStringSubmatchIndex(name); loc != nil {
			season, _ = strconv.Atoi(name[loc[2]:loc[3]])
			episode, _ = strconv.Atoi(name[loc[4]:loc[5]])
			return season, episode, name[:loc[0]], name[loc[1]:], true
		}
	}
	if m := reAirDate.FindStringSubmatchIndex(name); m != nil {
		num := func(i int) int {
			if m[2*i] < 0 {
				return 0
			}
			n, _ := strconv.Atoi(name[m[2*i]:m[2*i+1]])
			return n
		}
		season = num(1)
		episode = num(2)*1000000 + num(3)*10000 + num(4)*100 + num(5)
		return season, episode, name[:m[0]], name[m[1]:], true
	}
	return 0, 0, "", "", false
}

// parseSeasonDir reads "Season 2", "S02" or "Specials".
func parseSeasonDir(name string) (int, bool) {
	if strings.EqualFold(name, "specials") {
		return 0, true
	}
	if m := reSeason.FindStringSubmatch(strings.TrimSpace(name)); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

func parseLeadingNumber(name string) (int, bool) {
	if m := reLeadNum.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// normKey reduces a title to letters and digits for stable match keys.
func normKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var reExtraEpisode = regexp.MustCompile(`(?i)^[\s._\-]*e\d{1,4}`)

func episodeTitle(rest string) string {
	rest = reExtraEpisode.ReplaceAllString(rest, "") // multi-episode files: S01E01E02
	return cleanTitle(rest)
}
