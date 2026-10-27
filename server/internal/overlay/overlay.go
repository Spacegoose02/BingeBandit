// Package overlay draws Kometa-style badges on posters: resolution, Dolby
// Vision / HDR, the best audio track, IMDb / Rotten Tomatoes / TMDB scores,
// and images an admin uploaded. Posters are drawn when served; the stored
// artwork is never changed.
package overlay

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"bingebandit/internal/store"
)

// Facts are what a title's overlays depend on.
type Facts struct {
	Kind       string // movie | show
	LibraryID  int64
	Labels     []string
	AddedAt    int64
	Resolution string // 4k | 1080 | 720 | sd | "" (no file)
	Range      string // dv | hdr10 | hlg | hdr (HDR, kind unknown) | sdr | ""
	Audio      string // atmos | dtsx | truehd | dtshd | 7.1 | 5.1 | ""
	IMDb       float64
	RT         int
	TMDB       float64
}

// Badges says which built-in overlays are on.
type Badges struct {
	Resolution, HDR, Audio, Ratings bool
}

// FactsFrom works out a title's facts from its files and scores. For shows
// the best copy of any episode counts, as Kometa does.
func FactsFrom(f *store.OverlayFacts) Facts {
	out := Facts{
		Kind: f.Item.Kind, LibraryID: f.Item.LibraryID, Labels: f.Item.Labels, AddedAt: f.Item.AddedAt,
		IMDb: f.Ratings.IMDb, RT: f.Ratings.RT, TMDB: f.Item.Rating,
	}
	bestW, bestH, rangeRank, audioRank := 0, 0, 0, 0
	for _, mf := range f.Files {
		if mf.Width > bestW || (mf.Width == bestW && mf.Height > bestH) {
			bestW, bestH = mf.Width, mf.Height
		}
		r := ""
		for _, st := range mf.Streams {
			if st.Type == "video" && r == "" {
				r = st.Range
			}
			if st.Type == "audio" {
				if a := audioOf(st); audioRanks[a] > audioRank {
					out.Audio, audioRank = a, audioRanks[a]
				}
			}
		}
		if r == "" && mf.HDR {
			r = "hdr" // probed before Dolby Vision was looked for
		}
		if rangeRanks[r] > rangeRank {
			out.Range, rangeRank = r, rangeRanks[r]
		}
	}
	if len(f.Files) > 0 {
		out.Resolution = resolutionOf(bestW, bestH)
	}
	return out
}

var rangeRanks = map[string]int{"sdr": 1, "hlg": 2, "hdr": 3, "hdr10": 4, "dv": 5}
var audioRanks = map[string]int{"5.1": 1, "7.1": 2, "dtshd": 3, "truehd": 4, "dtsx": 5, "atmos": 6}

// NeedsProbe reports whether a file was probed before BingeBandit looked
// for Dolby Vision and Atmos, so its badges may be missing something.
func NeedsProbe(mf *store.MediaFile) bool {
	for _, st := range mf.Streams {
		if st.Type == "video" {
			return st.Range == ""
		}
	}
	return false
}

func resolutionOf(w, h int) string {
	// Width first: a 2.39:1 1080p film is 1920×800.
	switch {
	case w >= 3200 || h >= 1800:
		return "4k"
	case w >= 1700 || h >= 1000:
		return "1080"
	case w >= 1200 || h >= 700:
		return "720"
	case w > 0:
		return "sd"
	}
	return ""
}

func audioOf(st store.Stream) string {
	desc := strings.ToLower(st.Profile + " " + st.Title)
	codec := strings.ToLower(st.Codec)
	switch {
	case strings.Contains(desc, "atmos"):
		return "atmos"
	case codec == "dts" && (strings.Contains(desc, "dts:x") || strings.Contains(desc, "dts-x")):
		return "dtsx"
	case codec == "truehd":
		return "truehd"
	case codec == "dts" && (strings.Contains(desc, "dts-hd ma") || strings.Contains(desc, "dts-hd master")):
		return "dtshd"
	case st.Channels >= 8:
		return "7.1"
	case st.Channels >= 6:
		return "5.1"
	}
	return ""
}

// Labels the built-in badges print.
var (
	resolutionText = map[string]string{"4k": "4K", "1080": "1080P", "720": "720P", "sd": "SD"}
	rangeText      = map[string]string{"dv": "DOLBY VISION", "hdr10": "HDR10", "hdr": "HDR", "hlg": "HLG"}
	audioText      = map[string]string{"atmos": "ATMOS", "dtsx": "DTS:X", "truehd": "TRUEHD", "dtshd": "DTS-HD MA", "7.1": "7.1", "5.1": "5.1"}
)

// Condition is one choice for when an uploaded overlay is drawn. Some take
// a value after a colon (label:Kids, imdb:8, new:14).
type Condition struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Value string `json:"value,omitempty"` // what the value is, when it takes one
}

var Conditions = []Condition{
	{ID: "always", Label: "Every poster"},
	{ID: "res:4k", Label: "4K"},
	{ID: "res:1080", Label: "1080p"},
	{ID: "res:720", Label: "720p"},
	{ID: "res:sd", Label: "SD (below 720p)"},
	{ID: "hdr", Label: "Any HDR (including Dolby Vision)"},
	{ID: "dv", Label: "Dolby Vision"},
	{ID: "hdr10", Label: "HDR without Dolby Vision"},
	{ID: "atmos", Label: "Dolby Atmos"},
	{ID: "dtsx", Label: "DTS:X"},
	{ID: "truehd", Label: "Dolby TrueHD (not Atmos)"},
	{ID: "dtshd", Label: "DTS-HD MA"},
	{ID: "surround", Label: "5.1 or more channels"},
	{ID: "label", Label: "Has the label", Value: "label"},
	{ID: "imdb", Label: "IMDb score at least", Value: "score out of 10"},
	{ID: "rt", Label: "Rotten Tomatoes at least", Value: "percent"},
	{ID: "new", Label: "Added in the last", Value: "days"},
}

// ValidCondition checks an overlay's condition, with its value if it needs one.
func ValidCondition(c string) error {
	id, val, hasVal := strings.Cut(c, ":")
	for _, k := range Conditions {
		if k.Value == "" && k.ID == c {
			return nil
		}
		if k.Value != "" && k.ID == id {
			if !hasVal || strings.TrimSpace(val) == "" {
				return fmt.Errorf("%q needs a %s", k.Label, k.Value)
			}
			if id != "label" {
				if n, err := strconv.ParseFloat(val, 64); err != nil || n < 0 {
					return fmt.Errorf("%q needs a number", k.Label)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("unknown condition %q", c)
}

// Matches reports whether an uploaded overlay applies to a title.
func Matches(o *store.PosterOverlay, f Facts) bool {
	if !o.Enabled || (o.Kind != "" && o.Kind != f.Kind) || (o.LibraryID != 0 && o.LibraryID != f.LibraryID) {
		return false
	}
	id, val, _ := strings.Cut(o.Condition, ":")
	n, _ := strconv.ParseFloat(val, 64)
	switch id {
	case "always":
		return true
	case "res":
		return f.Resolution == val
	case "hdr":
		return rangeRanks[f.Range] >= rangeRanks["hlg"]
	case "dv":
		return f.Range == "dv"
	case "hdr10":
		return f.Range == "hdr10" || f.Range == "hdr" || f.Range == "hlg"
	case "atmos", "dtsx", "truehd", "dtshd":
		return f.Audio == id
	case "surround":
		return f.Audio != ""
	case "label":
		for _, l := range f.Labels {
			if strings.EqualFold(l, val) {
				return true
			}
		}
	case "imdb":
		return f.IMDb > 0 && f.IMDb >= n
	case "rt":
		return f.RT > 0 && float64(f.RT) >= n
	case "new":
		return f.AddedAt > 0 && time.Since(time.Unix(f.AddedAt, 0)) <= time.Duration(n*24)*time.Hour
	}
	return false
}

// Positions an uploaded overlay can sit at; full stretches it over the
// whole poster (frames).
var Positions = []string{"top-left", "top", "top-right", "left", "center", "right", "bottom-left", "bottom", "bottom-right", "full"}

func ValidPosition(p string) bool {
	for _, x := range Positions {
		if x == p {
			return true
		}
	}
	return false
}

// Plan is what will be drawn on one poster; its Key names the result.
type Plan struct {
	Resolution, Range, Audio string
	Ratings                  []Rating
	Custom                   []*store.PosterOverlay
}

type Rating struct {
	Source string // imdb | rt | tmdb
	Text   string
}

// PlanFor picks the badges and uploaded overlays for a title.
func PlanFor(f Facts, b Badges, custom []*store.PosterOverlay) Plan {
	var p Plan
	if b.Resolution {
		p.Resolution = resolutionText[f.Resolution]
	}
	if b.HDR {
		p.Range = rangeText[f.Range]
	}
	if b.Audio {
		p.Audio = audioText[f.Audio]
	}
	if b.Ratings {
		if f.IMDb > 0 {
			p.Ratings = append(p.Ratings, Rating{"imdb", strconv.FormatFloat(f.IMDb, 'f', 1, 64)})
		}
		if f.RT > 0 {
			p.Ratings = append(p.Ratings, Rating{"rt", strconv.Itoa(f.RT) + "%"})
		}
		if f.TMDB > 0 {
			p.Ratings = append(p.Ratings, Rating{"tmdb", strconv.Itoa(int(f.TMDB*10+0.5)) + "%"})
		}
	}
	for _, o := range custom {
		if Matches(o, f) {
			p.Custom = append(p.Custom, o)
		}
	}
	return p
}

// Empty reports whether nothing would be drawn.
func (p Plan) Empty() bool {
	return p.Resolution == "" && p.Range == "" && p.Audio == "" && len(p.Ratings) == 0 && len(p.Custom) == 0
}

// Key is a short name for this plan drawn on poster ref at width w, for the
// render cache. Bump the version when drawing changes.
func (p Plan) Key(ref string, w int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "v3|%s|%d|%s|%s|%s|", ref, w, p.Resolution, p.Range, p.Audio)
	for _, r := range p.Ratings {
		fmt.Fprintf(&b, "%s=%s,", r.Source, r.Text)
	}
	for _, o := range p.Custom {
		fmt.Fprintf(&b, "|%d@%d", o.ID, o.UpdatedAt)
	}
	return hash(b.String())
}
