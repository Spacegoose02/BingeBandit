package livetv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bingebandit/internal/store"
)

// An HDHomeRun network tuner (antenna or cable) as a source, without an
// M3U: a playlist source whose URL is hdhomerun://<address> reads the
// tuner's channel lineup, and a guide source with the same URL reads
// SiliconDust's free guide for it. Both name channels by their number
// (the lineup's GuideNumber, such as "7.1"), so channels match their guide
// by themselves; a Schedules Direct guide for an antenna lineup does too.

const hdhrScheme = "hdhomerun://"

// IsHDHomeRun reports whether a source reads an HDHomeRun tuner.
func IsHDHomeRun(src *store.LiveSource) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(src.URL)), hdhrScheme)
}

// hdhrGuideBase is SiliconDust's guide service; a variable so tests can point it elsewhere.
var hdhrGuideBase = "https://api.hdhomerun.com"

var hdhrClient = &http.Client{Timeout: 30 * time.Second}

// HDHomeRun is what a tuner says about itself (discover.json).
type HDHomeRun struct {
	FriendlyName string `json:"FriendlyName"`
	ModelNumber  string `json:"ModelNumber"`
	DeviceID     string `json:"DeviceID"`
	DeviceAuth   string `json:"DeviceAuth"`
	TunerCount   int    `json:"TunerCount"`
	BaseURL      string `json:"BaseURL"`
	LineupURL    string `json:"LineupURL"`
	LocalIP      string `json:"LocalIP,omitempty"` // from SiliconDust's discovery service
}

// hdhrBase turns hdhomerun://192.168.1.50 (or a bare address) into http://192.168.1.50.
func hdhrBase(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(s), hdhrScheme) {
		s = s[len(hdhrScheme):]
	}
	s = strings.TrimRight(s, "/")
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q isn't an HDHomeRun address; use hdhomerun://192.168.1.50", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

func getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := hdhrClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", u, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// DiscoverHDHomeRun asks a tuner at addr about itself.
func DiscoverHDHomeRun(ctx context.Context, addr string) (*HDHomeRun, error) {
	base, err := hdhrBase(addr)
	if err != nil {
		return nil, err
	}
	var d HDHomeRun
	if err := getJSON(ctx, base+"/discover.json", &d); err != nil {
		return nil, fmt.Errorf("no HDHomeRun answered at %s: %w", base, err)
	}
	if d.BaseURL == "" {
		d.BaseURL = base
	}
	if d.LineupURL == "" {
		d.LineupURL = base + "/lineup.json"
	}
	return &d, nil
}

// FindHDHomeRuns lists the tuners SiliconDust's discovery service sees on
// this network (it matches them by public address), for the admin page.
func FindHDHomeRuns(ctx context.Context) ([]HDHomeRun, error) {
	var found []HDHomeRun
	if err := getJSON(ctx, strings.Replace(hdhrGuideBase, "api.", "ipv4-api.", 1)+"/discover", &found); err != nil {
		return nil, err
	}
	out := []HDHomeRun{}
	for _, d := range found {
		if d.DeviceID == "" || d.LocalIP == "" {
			continue // storage (DVR) units list too
		}
		out = append(out, d)
	}
	return out, nil
}

type hdhrLineupEntry struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	DRM         int    `json:"DRM"`
	HD          int    `json:"HD"`
}

// hdhrStreams reads a tuner's lineup as playlist entries.
func hdhrStreams(ctx context.Context, src *store.LiveSource) ([]store.StreamEntry, *HDHomeRun, error) {
	dev, err := DiscoverHDHomeRun(ctx, src.URL)
	if err != nil {
		return nil, nil, err
	}
	var lineup []hdhrLineupEntry
	if err := getJSON(ctx, dev.LineupURL, &lineup); err != nil {
		return nil, nil, fmt.Errorf("channel lineup: %w", err)
	}
	group := "Antenna"
	if strings.Contains(strings.ToUpper(dev.ModelNumber), "DC") || strings.Contains(strings.ToLower(dev.FriendlyName), "prime") {
		group = "Cable"
	}
	var out []store.StreamEntry
	for _, e := range lineup {
		if e.URL == "" || e.DRM == 1 {
			continue // copy-protected cable channels can't be watched off the tuner
		}
		out = append(out, store.StreamEntry{Name: e.GuideName, URL: e.URL, Group: group, TvgID: e.GuideNumber, TvgChno: e.GuideNumber})
	}
	if len(out) == 0 {
		return nil, nil, fmt.Errorf("the tuner has no channels yet; run a channel scan in its own page (%s)", dev.BaseURL)
	}
	return out, dev, nil
}

type hdhrGuideChannel struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	ImageURL    string `json:"ImageURL"`
	Guide       []struct {
		StartTime       int64    `json:"StartTime"`
		EndTime         int64    `json:"EndTime"`
		Title           string   `json:"Title"`
		EpisodeNumber   string   `json:"EpisodeNumber"`
		EpisodeTitle    string   `json:"EpisodeTitle"`
		Synopsis        string   `json:"Synopsis"`
		ImageURL        string   `json:"ImageURL"`
		OriginalAirdate int64    `json:"OriginalAirdate"`
		Filter          []string `json:"Filter"`
	} `json:"Guide"`
}

// guideData is a guide fetched in full before it's imported, so the
// import's transaction doesn't wait on the network.
type guideData struct {
	channels [][3]string // id, name, icon
	programs []*store.GuideProgram
}

func (g *guideData) importInto(st *store.Store, sourceID int64) (int, error) {
	gi, err := st.BeginGuideImport(sourceID)
	if err != nil {
		return 0, err
	}
	for _, c := range g.channels {
		if err := gi.Channel(c[0], c[1], c[2]); err != nil {
			gi.Abort()
			return 0, err
		}
	}
	for _, p := range g.programs {
		if err := gi.Program(p); err != nil {
			gi.Abort()
			return 0, err
		}
	}
	return gi.Finish()
}

// hdhrGuide reads SiliconDust's free guide for a tuner, a few hours per
// request, as far ahead as it goes (about a day).
func hdhrGuide(ctx context.Context, src *store.LiveSource) (*guideData, error) {
	dev, err := DiscoverHDHomeRun(ctx, src.URL)
	if err != nil {
		return nil, err
	}
	if dev.DeviceAuth == "" {
		return nil, fmt.Errorf("the tuner didn't give a DeviceAuth for its guide")
	}
	out := &guideData{}
	seenCh := map[string]bool{}
	seenProg := map[string]bool{}
	start := time.Now().Add(-time.Hour).Unix()
	limit := time.Now().Add(guideAhead).Unix()
	for page := 0; page < 48 && start < limit; page++ {
		var chans []hdhrGuideChannel
		q := url.Values{"DeviceAuth": {dev.DeviceAuth}, "Start": {fmt.Sprint(start)}}
		if err := getJSON(ctx, hdhrGuideBase+"/api/guide.php?"+q.Encode(), &chans); err != nil {
			if len(out.programs) > 0 {
				break // what came in so far is kept
			}
			return nil, fmt.Errorf("HDHomeRun guide: %w", err)
		}
		// The next page starts where the channel with the shortest schedule ends.
		next := int64(0)
		for _, c := range chans {
			if !seenCh[c.GuideNumber] {
				seenCh[c.GuideNumber] = true
				out.channels = append(out.channels, [3]string{c.GuideNumber, c.GuideName, c.ImageURL})
			}
			last := int64(0)
			for _, p := range c.Guide {
				last = max(last, p.EndTime)
				key := c.GuideNumber + "@" + fmt.Sprint(p.StartTime)
				if p.EndTime <= start || seenProg[key] {
					continue
				}
				seenProg[key] = true
				cat := ""
				if len(p.Filter) > 0 {
					cat = p.Filter[0]
				}
				out.programs = append(out.programs, &store.GuideProgram{EPGID: c.GuideNumber, Start: p.StartTime, Stop: p.EndTime, Title: p.Title,
					Subtitle: p.EpisodeTitle, Description: p.Synopsis, Category: cat, Icon: p.ImageURL, Episode: p.EpisodeNumber,
					Genres: strings.Join(p.Filter, ", ")})
			}
			if last > start && (next == 0 || last < next) {
				next = last
			}
		}
		if next <= start {
			break // nothing further ahead
		}
		start = next
	}
	if len(out.channels) == 0 {
		return nil, fmt.Errorf("the HDHomeRun guide had no channels")
	}
	return out, nil
}
