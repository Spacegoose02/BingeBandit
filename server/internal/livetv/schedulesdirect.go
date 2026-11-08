package livetv

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"bingebandit/internal/store"
)

// Schedules Direct (schedulesdirect.org) as a guide source: two weeks of
// guide for the lineups added to the account (an antenna lineup by ZIP or
// postcode, or a cable or satellite provider's). The source's URL is
// schedulesdirect://username:password; lineups are added on the Schedules
// Direct website. Channels of an antenna lineup are named by their number
// ("7.1") so they match an HDHomeRun's channels; other lineups' by
// Schedules Direct's station id.

const sdScheme = "schedulesdirect://"

// sdBase is the Schedules Direct JSON API; a variable so tests can point it elsewhere.
var sdBase = "https://json.schedulesdirect.org/20141201"

// SDDays is how many days of guide are fetched.
const SDDays = 14

// IsSchedulesDirect reports whether a source reads Schedules Direct.
func IsSchedulesDirect(src *store.LiveSource) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(src.URL)), sdScheme)
}

type sdClient struct {
	token string
	hc    *http.Client
}

func (c *sdClient) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, sdBase+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "BingeBandit")
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("token", c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message string `json:"message"`
		}
		json.Unmarshal(b, &e)
		if e.Message == "" {
			e.Message = resp.Status
		}
		return fmt.Errorf("Schedules Direct: %s", e.Message)
	}
	return json.Unmarshal(b, out)
}

// sdCredentials reads schedulesdirect://user:password (or user:password@anything).
func sdCredentials(raw string) (user, pass string, err error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(s), sdScheme) {
		s = s[len(sdScheme):]
	}
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		s = s[:i]
	}
	user, pass, _ = strings.Cut(strings.TrimRight(s, "/"), ":")
	if u, err := url.PathUnescape(user); err == nil {
		user = u
	}
	if p, err := url.PathUnescape(pass); err == nil {
		pass = p
	}
	if user == "" || pass == "" {
		return "", "", fmt.Errorf("use schedulesdirect://username:password")
	}
	return user, pass, nil
}

func sdLogin(ctx context.Context, raw string) (*sdClient, error) {
	user, pass, err := sdCredentials(raw)
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(pass))
	c := &sdClient{hc: &http.Client{Timeout: 2 * time.Minute}}
	var tok struct {
		Token   string `json:"token"`
		Message string `json:"message"`
	}
	if err := c.do(ctx, http.MethodPost, "/token", map[string]string{"username": user, "password": hex.EncodeToString(sum[:])}, &tok); err != nil {
		return nil, err
	}
	if tok.Token == "" {
		return nil, fmt.Errorf("Schedules Direct didn't sign in: %s", tok.Message)
	}
	c.token = tok.Token
	return c, nil
}

// sdChannelNumber turns "007.1" or "7-1" into "7.1".
func sdChannelNumber(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "-", ".")
	parts := strings.Split(s, ".")
	for i, p := range parts {
		if n, err := strconv.Atoi(p); err == nil {
			parts[i] = strconv.Itoa(n)
		}
	}
	return strings.Join(parts, ".")
}

// schedulesDirectGuide fetches the account's lineups and two weeks of their programs.
func schedulesDirectGuide(ctx context.Context, src *store.LiveSource) (*guideData, error) {
	c, err := sdLogin(ctx, src.URL)
	if err != nil {
		return nil, err
	}
	var lineups struct {
		Lineups []struct {
			Lineup    string `json:"lineup"`
			URI       string `json:"uri"`
			IsDeleted bool   `json:"isDeleted"`
		} `json:"lineups"`
	}
	if err := c.do(ctx, http.MethodGet, "/lineups", nil, &lineups); err != nil {
		return nil, err
	}
	out := &guideData{}
	// station id -> the guide channel ids it's listed under
	ids := map[string][]string{}
	seen := map[string]bool{}
	for _, l := range lineups.Lineups {
		if l.IsDeleted {
			continue
		}
		var m struct {
			Map []struct {
				StationID string `json:"stationID"`
				Channel   string `json:"channel"`
				ATSCMajor int    `json:"atscMajor"`
				ATSCMinor int    `json:"atscMinor"`
			} `json:"map"`
			Stations []struct {
				StationID string `json:"stationID"`
				Name      string `json:"name"`
				Callsign  string `json:"callsign"`
				Logo      struct {
					URL string `json:"URL"`
				} `json:"logo"`
			} `json:"stations"`
		}
		path := strings.TrimPrefix(l.URI, "/20141201")
		if path == "" {
			path = "/lineups/" + l.Lineup
		}
		if err := c.do(ctx, http.MethodGet, path, nil, &m); err != nil {
			return nil, err
		}
		ota := strings.Contains(strings.ToUpper(l.Lineup), "-OTA-")
		names := map[string][2]string{}
		for _, st := range m.Stations {
			name := st.Callsign
			if name == "" {
				name = st.Name
			}
			names[st.StationID] = [2]string{name, st.Logo.URL}
		}
		for _, e := range m.Map {
			id := e.StationID
			if ota {
				if e.ATSCMajor > 0 {
					id = fmt.Sprintf("%d.%d", e.ATSCMajor, e.ATSCMinor)
				} else if e.Channel != "" {
					id = sdChannelNumber(e.Channel)
				}
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			ids[e.StationID] = append(ids[e.StationID], id)
			out.channels = append(out.channels, [3]string{id, names[e.StationID][0], names[e.StationID][1]})
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("the Schedules Direct account has no lineups; add one on schedulesdirect.org")
	}

	// Schedules, a few hundred stations a request.
	var dates []string
	for d := range SDDays {
		dates = append(dates, time.Now().UTC().AddDate(0, 0, d-1).Format("2006-01-02"))
	}
	type airing struct {
		ProgramID    string `json:"programID"`
		AirDateTime  string `json:"airDateTime"`
		Duration     int64  `json:"duration"`
	}
	var stations []string
	for id := range ids {
		stations = append(stations, id)
	}
	type sched struct {
		StationID string   `json:"stationID"`
		Programs  []airing `json:"programs"`
	}
	var airings []struct {
		station string
		a       airing
	}
	need := map[string]bool{}
	for i := 0; i < len(stations); i += 400 {
		var req []map[string]any
		for _, st := range stations[i:min(i+400, len(stations))] {
			req = append(req, map[string]any{"stationID": st, "date": dates})
		}
		var res []sched
		if err := c.do(ctx, http.MethodPost, "/schedules", req, &res); err != nil {
			return nil, err
		}
		for _, s := range res {
			for _, a := range s.Programs {
				airings = append(airings, struct {
					station string
					a       airing
				}{s.StationID, a})
				need[a.ProgramID] = true
			}
		}
	}

	// Program details, up to 5000 a request.
	type program struct {
		ProgramID string `json:"programID"`
		Titles    []struct {
			Title120 string `json:"title120"`
		} `json:"titles"`
		EpisodeTitle150 string `json:"episodeTitle150"`
		Descriptions    struct {
			Description1000 []struct {
				Description string `json:"description"`
			} `json:"description1000"`
			Description100 []struct {
				Description string `json:"description"`
			} `json:"description100"`
		} `json:"descriptions"`
		Genres   []string `json:"genres"`
		Metadata []map[string]struct {
			Season  int `json:"season"`
			Episode int `json:"episode"`
		} `json:"metadata"`
		Movie struct {
			Year string `json:"year"`
		} `json:"movie"`
		ContentRating []struct {
			Body string `json:"body"`
			Code string `json:"code"`
		} `json:"contentRating"`
	}
	var pids []string
	for id := range need {
		pids = append(pids, id)
	}
	progs := map[string]*program{}
	for i := 0; i < len(pids); i += 5000 {
		var res []program
		if err := c.do(ctx, http.MethodPost, "/programs", pids[i:min(i+5000, len(pids))], &res); err != nil {
			return nil, err
		}
		for k := range res {
			progs[res[k].ProgramID] = &res[k]
		}
	}

	for _, x := range airings {
		start, err := time.Parse(time.RFC3339, x.a.AirDateTime)
		if err != nil {
			continue
		}
		gp := &store.GuideProgram{Start: start.Unix(), Stop: start.Unix() + x.a.Duration}
		if p := progs[x.a.ProgramID]; p != nil {
			if len(p.Titles) > 0 {
				gp.Title = p.Titles[0].Title120
			}
			gp.Subtitle = p.EpisodeTitle150
			if len(p.Descriptions.Description1000) > 0 {
				gp.Description = p.Descriptions.Description1000[0].Description
			} else if len(p.Descriptions.Description100) > 0 {
				gp.Description = p.Descriptions.Description100[0].Description
			}
			if len(p.Genres) > 0 {
				gp.Category = p.Genres[0]
				gp.Genres = strings.Join(p.Genres, ", ")
			}
			for _, m := range p.Metadata {
				if g, ok := m["Gracenote"]; ok && g.Season > 0 && g.Episode > 0 {
					gp.Episode = fmt.Sprintf("S%02dE%02d", g.Season, g.Episode)
				}
			}
			gp.Year, _ = strconv.Atoi(p.Movie.Year)
			for _, r := range p.ContentRating {
				if r.Body == "USA Parental Rating" || r.Body == "Motion Picture Association" || gp.Rating == "" {
					gp.Rating = r.Code
				}
			}
		}
		if gp.Title == "" {
			gp.Title = "To be announced"
		}
		for _, id := range ids[x.station] {
			cp := *gp
			cp.EPGID = id
			out.programs = append(out.programs, &cp)
		}
	}
	return out, nil
}
