package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// file is one movie or episode file, as the plugin API's /files lists it.
type file struct {
	FileID     int64   `json:"fileId"`
	ItemID     int64   `json:"itemId"`
	Kind       string  `json:"kind"`
	Title      string  `json:"title"`
	Year       int     `json:"year"`
	TMDBID     int64   `json:"tmdbId"`
	ShowID     int64   `json:"showId"`
	ShowTitle  string  `json:"showTitle"`
	ShowTMDBID int64   `json:"showTmdbId"`
	Season     *int    `json:"season"`
	Episode    *int    `json:"episode"`
	Size       int64   `json:"size"`
	Bitrate    int64   `json:"bitrate"`
	VideoCodec string  `json:"videoCodec"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	HDR        bool    `json:"hdr"`
	Duration   float64 `json:"duration"`
}

// Resolutions, lowest first. Widths count too, so a 1920×800 widescreen
// film is 1080p.
var resolutions = []string{"SD", "720p", "1080p", "2160p"}

func resolution(w, h int) string {
	switch {
	case w >= 3200 || h >= 1800:
		return "2160p"
	case w >= 1800 || h >= 1000:
		return "1080p"
	case w >= 1200 || h >= 700:
		return "720p"
	}
	return "SD"
}

func resRank(r string) int { return slices.Index(resolutions, r) }

// Codecs from before H.264, as ffprobe names them.
var oldCodecs = map[string]bool{
	"mpeg1video": true, "mpeg2video": true, "mpeg4": true, "msmpeg4v1": true, "msmpeg4v2": true, "msmpeg4v3": true,
	"wmv1": true, "wmv2": true, "wmv3": true, "vc1": true, "h263": true, "rv30": true, "rv40": true, "theora": true,
}

// rules is what counts as worth upgrading, from the plugin's settings.
type rules struct {
	Target    string  // lowest resolution wanted
	OldCodecs bool    // flag pre-H.264 codecs
	MinMbps   float64 // flag files below this bitrate; 0 is off
	Want4KHDR bool    // flag 4K files without HDR
}

func rulesFrom(s map[string]string) rules {
	r := rules{Target: s["target"], OldCodecs: s["old_codecs"] != "false", Want4KHDR: s["want_hdr"] == "true"}
	if resRank(r.Target) < 0 {
		r.Target = "1080p"
	}
	r.MinMbps, _ = strconv.ParseFloat(strings.TrimSpace(s["min_bitrate"]), 64)
	return r
}

// reasons says why a file is worth upgrading; none means it's fine.
func (r rules) reasons(f file) []string {
	var out []string
	if f.Height == 0 && f.Width == 0 {
		return nil // not probed yet
	}
	res := resolution(f.Width, f.Height)
	if resRank(res) < resRank(r.Target) {
		out = append(out, res+", below "+r.Target)
	}
	if r.OldCodecs && oldCodecs[strings.ToLower(f.VideoCodec)] {
		out = append(out, "old codec ("+f.VideoCodec+")")
	}
	if mbps := float64(f.Bitrate) / 1e6; r.MinMbps > 0 && f.Bitrate > 0 && mbps < r.MinMbps {
		out = append(out, fmt.Sprintf("low bitrate (%.1f Mbps)", mbps))
	}
	if r.Want4KHDR && res == "2160p" && !f.HDR {
		out = append(out, "4K without HDR")
	}
	return out
}

// describe is a file's quality in a few words, like "720p h264 4.2 Mbps".
func describe(f file) string {
	if f.Height == 0 && f.Width == 0 {
		return "not probed yet"
	}
	parts := []string{resolution(f.Width, f.Height)}
	if f.VideoCodec != "" {
		parts = append(parts, f.VideoCodec)
	}
	if f.HDR {
		parts = append(parts, "HDR")
	}
	if f.Bitrate > 0 {
		parts = append(parts, fmt.Sprintf("%.1f Mbps", float64(f.Bitrate)/1e6))
	}
	return strings.Join(parts, " ")
}

// better reports whether a is the better copy of the same thing.
func better(a, b file) bool {
	ra, rb := resRank(resolution(a.Width, a.Height)), resRank(resolution(b.Width, b.Height))
	if ra != rb {
		return ra > rb
	}
	return a.Bitrate > b.Bitrate
}

// movie is a movie worth upgrading.
type movie struct {
	ItemID  int64    `json:"itemId"`
	TMDBID  int64    `json:"tmdbId"`
	Title   string   `json:"title"`
	Year    int      `json:"year,omitempty"`
	Quality string   `json:"quality"`
	Size    int64    `json:"size"`
	Reasons []string `json:"reasons"`
}

type episode struct {
	ItemID  int64    `json:"itemId"`
	Season  int      `json:"season"`
	Episode int      `json:"episode"`
	Title   string   `json:"title"`
	Quality string   `json:"quality"`
	Reasons []string `json:"reasons"`
}

// show is a show with episodes worth upgrading.
type show struct {
	ShowID   int64     `json:"showId"`
	TMDBID   int64     `json:"tmdbId"`
	Title    string    `json:"title"`
	Total    int       `json:"total"` // episodes in the library
	Episodes []episode `json:"episodes"`
}

// report is what a library scan found.
type report struct {
	Rules  rules   `json:"-"`
	Target string  `json:"target"`
	Files  int     `json:"files"`
	Movies []movie `json:"movies"`
	Shows  []show  `json:"shows"`
}

// find picks each movie's and episode's best file and keeps the ones that
// are still worth upgrading. Another copy that's already good enough (a 4K
// version next to a 720p one) means nothing to do.
func find(files []file, r rules) report {
	best := map[int64]file{}
	var order []int64
	for _, f := range files {
		cur, ok := best[f.ItemID]
		if !ok {
			order = append(order, f.ItemID)
		}
		if !ok || better(f, cur) {
			best[f.ItemID] = f
		}
	}
	rep := report{Rules: r, Target: r.Target, Files: len(files), Movies: []movie{}, Shows: []show{}}
	shows := map[int64]*show{}
	var showOrder []int64
	for _, id := range order {
		f := best[id]
		why := r.reasons(f)
		if f.Kind == "episode" {
			s := shows[f.ShowID]
			if s == nil {
				s = &show{ShowID: f.ShowID, TMDBID: f.ShowTMDBID, Title: f.ShowTitle, Episodes: []episode{}}
				shows[f.ShowID] = s
				showOrder = append(showOrder, f.ShowID)
			}
			s.Total++
			if len(why) > 0 {
				e := episode{ItemID: f.ItemID, Title: f.Title, Quality: describe(f), Reasons: why}
				if f.Season != nil {
					e.Season = *f.Season
				}
				if f.Episode != nil {
					e.Episode = *f.Episode
				}
				s.Episodes = append(s.Episodes, e)
			}
			continue
		}
		if len(why) > 0 {
			rep.Movies = append(rep.Movies, movie{ItemID: f.ItemID, TMDBID: f.TMDBID, Title: f.Title, Year: f.Year, Quality: describe(f), Size: f.Size, Reasons: why})
		}
	}
	for _, id := range showOrder {
		s := shows[id]
		if len(s.Episodes) == 0 {
			continue
		}
		slices.SortFunc(s.Episodes, func(a, b episode) int {
			if a.Season != b.Season {
				return a.Season - b.Season
			}
			return a.Episode - b.Episode
		})
		rep.Shows = append(rep.Shows, *s)
	}
	byTitle := func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }
	slices.SortFunc(rep.Movies, func(a, b movie) int { return byTitle(a.Title, b.Title) })
	slices.SortFunc(rep.Shows, func(a, b show) int { return byTitle(a.Title, b.Title) })
	return rep
}
