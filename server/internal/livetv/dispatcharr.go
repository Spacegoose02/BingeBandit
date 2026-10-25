package livetv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/store"
)

// Dispatcharr import: copies a Dispatcharr setup (provider accounts, guides,
// and channels with their numbers, groups, logos, guide ids and fallback
// streams) through its REST API, so BingeBandit can take over from it.
// BingeBandit then talks to the providers itself; Dispatcharr isn't needed
// afterwards (except for logos that were uploaded to it).

// Dispatcharr is a connection to a Dispatcharr server. Sign in with an admin
// username and password, or an API key.
type Dispatcharr struct {
	base     string
	username string
	password string
	apiKey   string
	token    string
	hc       *http.Client
}

func NewDispatcharr(base, username, password, apiKey string) *Dispatcharr {
	return &Dispatcharr{base: strings.TrimRight(base, "/"), username: username, password: password, apiKey: apiKey,
		hc: &http.Client{Timeout: 3 * time.Minute}}
}

func (d *Dispatcharr) login(ctx context.Context) error {
	if d.apiKey != "" || d.token != "" {
		return nil
	}
	var out struct {
		Access string `json:"access"`
	}
	err := d.call(ctx, http.MethodPost, "/api/accounts/token/", map[string]string{"username": d.username, "password": d.password}, &out)
	var he *httpError
	if errors.As(err, &he) && (he.status == http.StatusUnauthorized || he.status == http.StatusBadRequest) {
		return errors.New("Dispatcharr didn't accept that username and password")
	}
	if err != nil {
		return err
	}
	if out.Access == "" {
		return errors.New("Dispatcharr didn't return a sign-in token; is this the right address?")
	}
	d.token = out.Access
	return nil
}

type httpError struct {
	status int
	path   string
	body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("Dispatcharr %s: %d %s", e.path, e.status, e.body)
}

func (d *Dispatcharr) call(ctx context.Context, method, p string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.base+p, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if d.apiKey != "" {
		req.Header.Set("X-API-Key", d.apiKey)
	} else if d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Dispatcharr: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if p != "/api/accounts/token/" {
			return errors.New("Dispatcharr refused access; use an admin account or an admin's API key")
		}
	}
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return &httpError{status: resp.StatusCode, path: p, body: strings.TrimSpace(string(msg))}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("Dispatcharr %s: unexpected reply (%v); is this the right address?", p, err)
	}
	return nil
}

// listAll reads a list endpoint, whether it answers with a plain list or
// DRF pages ({"results": [...], "next": "..."}).
func listAll[T any](ctx context.Context, d *Dispatcharr, p string) ([]T, error) {
	var out []T
	for p != "" {
		var raw json.RawMessage
		if err := d.call(ctx, http.MethodGet, p, nil, &raw); err != nil {
			return nil, err
		}
		if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '[' {
			var page []T
			if err := json.Unmarshal(t, &page); err != nil {
				return nil, fmt.Errorf("Dispatcharr %s: %w", p, err)
			}
			return append(out, page...), nil
		}
		var page struct {
			Results []T     `json:"results"`
			Next    *string `json:"next"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("Dispatcharr %s: %w", p, err)
		}
		out = append(out, page.Results...)
		p = ""
		if page.Next != nil && *page.Next != "" {
			// Follow the path only: behind a proxy Dispatcharr may name
			// itself by an address we can't reach.
			if u, err := url.Parse(*page.Next); err == nil {
				p = u.RequestURI()
			}
		}
	}
	return out, nil
}

type dAccount struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	ServerURL       string `json:"server_url"`
	FilePath        string `json:"file_path"`
	MaxStreams      int    `json:"max_streams"`
	IsActive        bool   `json:"is_active"`
	Locked          bool   `json:"locked"`
	UserAgent       *int64 `json:"user_agent"`
	AccountType     string `json:"account_type"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	RefreshInterval int    `json:"refresh_interval"` // hours; 0 = never
	ChannelGroups   []struct {
		ChannelGroup int64 `json:"channel_group"`
		Enabled      bool  `json:"enabled"`
	} `json:"channel_groups"`
	AutoEnableNewGroups *bool `json:"auto_enable_new_groups_live"`
}

type dUserAgent struct {
	ID        int64  `json:"id"`
	UserAgent string `json:"user_agent"`
}

type dEPGSource struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	SourceType      string `json:"source_type"`
	URL             string `json:"url"`
	IsActive        bool   `json:"is_active"`
	RefreshInterval int    `json:"refresh_interval"`
}

type dEPGData struct {
	ID    int64  `json:"id"`
	TvgID string `json:"tvg_id"`
}

type dNamed struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type dStream struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	M3UAccount   *int64 `json:"m3u_account"`
	ChannelGroup *int64 `json:"channel_group"`
	StreamID     *int64 `json:"stream_id"` // the provider's id, for Xtream accounts
}

type dChannel struct {
	ID               int64           `json:"id"`
	Number           *float64        `json:"channel_number"`
	Name             string          `json:"name"`
	GroupID          *int64          `json:"channel_group_id"`
	TvgID            string          `json:"tvg_id"`
	EPGDataID        *int64          `json:"epg_data_id"`
	LogoID           *int64          `json:"logo_id"`
	HiddenFromOutput bool            `json:"hidden_from_output"`
	Streams          json.RawMessage `json:"streams"` // ids, or objects with include_streams

	// Newer versions report what a channel shows after its overrides.
	EffName      *string  `json:"effective_name"`
	EffNumber    *float64 `json:"effective_channel_number"`
	EffGroupID   *int64   `json:"effective_channel_group_id"`
	EffLogoID    *int64   `json:"effective_logo_id"`
	EffTvgID     *string  `json:"effective_tvg_id"`
	EffEPGDataID *int64   `json:"effective_epg_data_id"`

	streams []int64
}

func (c *dChannel) resolve() {
	if c.EffName != nil && *c.EffName != "" {
		c.Name = *c.EffName
	}
	if c.EffNumber != nil {
		c.Number = c.EffNumber
	}
	if c.EffGroupID != nil {
		c.GroupID = c.EffGroupID
	}
	if c.EffLogoID != nil {
		c.LogoID = c.EffLogoID
	}
	if c.EffTvgID != nil {
		c.TvgID = *c.EffTvgID
	}
	if c.EffEPGDataID != nil {
		c.EPGDataID = c.EffEPGDataID
	}
}

// DispatcharrSetup is everything read from Dispatcharr.
type DispatcharrSetup struct {
	base       string
	accounts   []dAccount
	userAgents map[int64]string
	guides     []dEPGSource
	groups     map[int64]string
	logos      map[int64]dNamed
	epgData    map[int64]string // EPGData id → tvg_id
	channels   []dChannel
	streams    map[int64]dStream
}

// Fetch reads the Dispatcharr setup.
func (d *Dispatcharr) Fetch(ctx context.Context) (*DispatcharrSetup, error) {
	if err := d.login(ctx); err != nil {
		return nil, err
	}
	s := &DispatcharrSetup{base: d.base, userAgents: map[int64]string{}, groups: map[int64]string{}, logos: map[int64]dNamed{},
		epgData: map[int64]string{}, streams: map[int64]dStream{}}
	var err error
	if s.accounts, err = listAll[dAccount](ctx, d, "/api/m3u/accounts/"); err != nil {
		return nil, err
	}
	if uas, err := listAll[dUserAgent](ctx, d, "/api/core/useragents/"); err == nil {
		for _, ua := range uas {
			s.userAgents[ua.ID] = ua.UserAgent
		}
	}
	if s.guides, err = listAll[dEPGSource](ctx, d, "/api/epg/sources/"); err != nil {
		return nil, err
	}
	groups, err := listAll[dNamed](ctx, d, "/api/channels/groups/")
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		s.groups[g.ID] = g.Name
	}
	logos, err := listAll[dNamed](ctx, d, "/api/channels/logos/?no_pagination=true")
	if err != nil {
		return nil, err
	}
	for _, l := range logos {
		s.logos[l.ID] = l
	}
	if s.channels, err = listAll[dChannel](ctx, d, "/api/channels/channels/?include_streams=true"); err != nil {
		return nil, err
	}
	var missing []int64
	needEPG := false
	for i := range s.channels {
		c := &s.channels[i]
		c.resolve()
		var objs []dStream
		if json.Unmarshal(c.Streams, &objs) == nil {
			for _, st := range objs {
				c.streams = append(c.streams, st.ID)
				s.streams[st.ID] = st
			}
		} else if json.Unmarshal(c.Streams, &c.streams) == nil {
			for _, id := range c.streams {
				if _, ok := s.streams[id]; !ok {
					missing = append(missing, id)
				}
			}
		}
		if c.EPGDataID != nil {
			needEPG = true
		}
	}
	// Older versions list stream ids only.
	for len(missing) > 0 {
		n := min(len(missing), 500)
		var page []dStream
		if err := d.call(ctx, http.MethodPost, "/api/channels/streams/by-ids/", map[string]any{"ids": missing[:n]}, &page); err != nil {
			return nil, err
		}
		for _, st := range page {
			s.streams[st.ID] = st
		}
		missing = missing[n:]
	}
	if needEPG {
		// Guide matches are by the guide's own channel id, which may differ
		// from the channel's tvg-id. Optional: without it tvg-id is used.
		if data, err := listAll[dEPGData](ctx, d, "/api/epg/epgdata/"); err == nil {
			for _, e := range data {
				s.epgData[e.ID] = e.TvgID
			}
		}
	}
	return s, nil
}

// playlistURL is where BingeBandit reads an account's playlist, or a reason
// it can't.
func (a dAccount) playlistURL() (string, string) {
	if a.Locked || a.Name == "custom" {
		return "", "streams you added by hand in Dispatcharr"
	}
	server := strings.TrimSpace(a.ServerURL)
	if server == "" {
		return "", "uses an uploaded playlist file; add it by URL or path under Sources"
	}
	if strings.EqualFold(a.AccountType, "XC") {
		if a.Username == "" || a.Password == "" {
			return "", "Dispatcharr didn't share this Xtream account's login; sign in as an admin"
		}
		u, err := url.Parse(server)
		if err != nil {
			return "", "its server address isn't a URL"
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/get.php"
		u.RawQuery = url.Values{"username": {a.Username}, "password": {a.Password}, "type": {"m3u_plus"}, "output": {"ts"}}.Encode()
		return u.String(), ""
	}
	return server, ""
}

func (g dEPGSource) usable() string {
	switch g.SourceType {
	case "", "xmltv":
	case "schedules_direct":
		return "Schedules Direct guides aren't supported yet"
	case "dummy":
		return "a placeholder guide made by Dispatcharr"
	default:
		return "not an XMLTV guide"
	}
	if strings.TrimSpace(g.URL) == "" {
		return "uses an uploaded file; add it by URL or path under Sources"
	}
	return ""
}

// DispatcharrPlan summarises what an import will do.
type DispatcharrPlan struct {
	Accounts []PlanItem `json:"accounts"`
	Guides   []PlanItem `json:"guides"`
	Channels int        `json:"channels"`
	Groups   int        `json:"groups"`
}

type PlanItem struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	Skip   string `json:"skip,omitempty"` // why it won't be imported
}

func (s *DispatcharrSetup) Plan() DispatcharrPlan {
	p := DispatcharrPlan{Accounts: []PlanItem{}, Guides: []PlanItem{}, Channels: len(s.channels)}
	for _, a := range s.accounts {
		_, skip := a.playlistURL()
		if skip != "" && (a.Locked || a.Name == "custom") && s.accountChannels(a.ID) == 0 {
			continue // Dispatcharr's built-in custom account, unused
		}
		detail := "M3U playlist"
		if strings.EqualFold(a.AccountType, "XC") {
			detail = "Xtream Codes"
		}
		if a.MaxStreams > 0 {
			detail += fmt.Sprintf(" · %d connections", a.MaxStreams)
		}
		if !a.IsActive {
			detail += " · disabled"
		}
		if n := s.accountChannels(a.ID); n > 0 {
			detail += fmt.Sprintf(" · used by %d channels", n)
		}
		p.Accounts = append(p.Accounts, PlanItem{Name: a.Name, Detail: detail, Skip: skip})
	}
	for _, g := range s.guides {
		p.Guides = append(p.Guides, PlanItem{Name: g.Name, Detail: "XMLTV", Skip: g.usable()})
	}
	used := map[int64]bool{}
	for _, c := range s.channels {
		if c.GroupID != nil {
			used[*c.GroupID] = true
		}
	}
	p.Groups = len(used)
	return p
}

func (s *DispatcharrSetup) accountChannels(accountID int64) int {
	n := 0
	for _, c := range s.channels {
		for _, sid := range c.streams {
			if st, ok := s.streams[sid]; ok && st.M3UAccount != nil && *st.M3UAccount == accountID {
				n++
				break
			}
		}
	}
	return n
}

// DispatcharrStatus is what the admin page shows while and after an import.
type DispatcharrStatus struct {
	Running    bool     `json:"running"`
	Phase      string   `json:"phase"`
	StartedAt  int64    `json:"startedAt"`
	FinishedAt int64    `json:"finishedAt"`
	Sources    int      `json:"sources"`  // playlists and guides added
	Channels   int      `json:"channels"` // channels created
	Existing   int      `json:"existing"` // channels already in BingeBandit
	Notes      []string `json:"notes"`    // what was skipped or went wrong
	Error      string   `json:"error,omitempty"`
}

// DispatcharrJob runs one import at a time in the background.
type DispatcharrJob struct {
	mu     sync.Mutex
	status DispatcharrStatus
}

func (j *DispatcharrJob) Status() DispatcharrStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := j.status
	s.Notes = append([]string{}, s.Notes...)
	return s
}

func (j *DispatcharrJob) update(f func(s *DispatcharrStatus)) {
	j.mu.Lock()
	f(&j.status)
	j.mu.Unlock()
}

func (j *DispatcharrJob) note(format string, args ...any) {
	j.update(func(s *DispatcharrStatus) { s.Notes = append(s.Notes, fmt.Sprintf(format, args...)) })
}

func (j *DispatcharrJob) Start(ctx context.Context, st *store.Store, setup *DispatcharrSetup) error {
	j.mu.Lock()
	if j.status.Running {
		j.mu.Unlock()
		return errors.New("an import is already running")
	}
	j.status = DispatcharrStatus{Running: true, Phase: "Starting", StartedAt: time.Now().Unix(), Notes: []string{}}
	j.mu.Unlock()
	go func() {
		err := j.run(ctx, st, setup)
		j.update(func(s *DispatcharrStatus) {
			s.Running, s.FinishedAt, s.Phase = false, time.Now().Unix(), "Finished"
			if err != nil {
				s.Phase, s.Error = "Stopped", err.Error()
			}
		})
	}()
	return nil
}

func (j *DispatcharrJob) phase(p string) { j.update(func(s *DispatcharrStatus) { s.Phase = p }) }

func (j *DispatcharrJob) run(ctx context.Context, st *store.Store, setup *DispatcharrSetup) error {
	existing, err := st.LiveSources("")
	if err != nil {
		return err
	}
	byURL := map[string]*store.LiveSource{}
	for _, src := range existing {
		byURL[src.Kind+" "+src.URL] = src
	}

	// Playlists first: channels can only use streams that have been read.
	sourceOf := map[int64]int64{} // Dispatcharr account → BingeBandit source
	for _, a := range setup.accounts {
		u, skip := a.playlistURL()
		if skip != "" {
			if setup.accountChannels(a.ID) > 0 || !(a.Locked || a.Name == "custom") {
				j.note("Skipped playlist %q: %s.", a.Name, skip)
			}
			continue
		}
		if src, ok := byURL["m3u "+u]; ok {
			sourceOf[a.ID] = src.ID
			j.note("Playlist %q is already in BingeBandit as %q; kept it as it is.", a.Name, src.Name)
			continue
		}
		src := &store.LiveSource{Kind: "m3u", Name: a.Name, URL: u, MaxStreams: a.MaxStreams,
			RefreshHours: refreshHours(a.RefreshInterval, 24), Enabled: a.IsActive}
		if a.UserAgent != nil {
			src.UserAgent = setup.userAgents[*a.UserAgent]
		}
		if err := st.SaveLiveSource(src); err != nil {
			return err
		}
		j.update(func(s *DispatcharrStatus) { s.Sources++ })
		sourceOf[a.ID] = src.ID
		if groups, newOn, ok := setup.groupFilter(a); ok {
			if err := st.SeedSourceGroups(src.ID, newOn, groups); err != nil {
				return err
			}
		}
		j.phase(fmt.Sprintf("Reading playlist %q", a.Name))
		if err := Refresh(ctx, st, src); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			j.note("Playlist %q was added, but reading it failed: %v. Its channels were skipped; refresh it under Sources and import again.", a.Name, err)
		}
	}

	for _, g := range setup.guides {
		if skip := g.usable(); skip != "" {
			j.note("Skipped guide %q: %s.", g.Name, skip)
			continue
		}
		if src, ok := byURL["epg "+g.URL]; ok {
			j.note("Guide %q is already in BingeBandit as %q.", g.Name, src.Name)
			continue
		}
		src := &store.LiveSource{Kind: "epg", Name: g.Name, URL: g.URL, RefreshHours: refreshHours(g.RefreshInterval, 12), Enabled: g.IsActive}
		if err := st.SaveLiveSource(src); err != nil {
			return err
		}
		j.update(func(s *DispatcharrStatus) { s.Sources++ })
		j.phase(fmt.Sprintf("Reading guide %q", g.Name))
		if err := Refresh(ctx, st, src); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			j.note("Guide %q was added, but reading it failed: %v.", g.Name, err)
		}
	}

	j.phase("Creating channels")
	return j.channels(st, setup, sourceOf)
}

func refreshHours(h, def int) int {
	if h <= 0 {
		return def
	}
	return h
}

// groupFilter is the account's groups as Dispatcharr has them switched on,
// plus any group a channel takes a stream from.
func (s *DispatcharrSetup) groupFilter(a dAccount) (map[string]bool, bool, bool) {
	if len(a.ChannelGroups) == 0 {
		return nil, false, false
	}
	on := map[string]bool{}
	for _, g := range a.ChannelGroups {
		if name, ok := s.groups[g.ChannelGroup]; ok {
			on[name] = on[name] || g.Enabled
		}
	}
	for _, c := range s.channels {
		for _, sid := range c.streams {
			st := s.streams[sid]
			if st.M3UAccount != nil && *st.M3UAccount == a.ID && st.ChannelGroup != nil {
				if name, ok := s.groups[*st.ChannelGroup]; ok {
					on[name] = true
				}
			}
		}
	}
	newOn := true
	if a.AutoEnableNewGroups != nil {
		newOn = *a.AutoEnableNewGroups
	}
	return on, newOn, true
}

func (j *DispatcharrJob) channels(st *store.Store, setup *DispatcharrSetup, sourceOf map[int64]int64) error {
	have, err := st.Channels(0, true)
	if err != nil {
		return err
	}
	taken := map[float64]bool{}
	exists := map[string]bool{}
	names := map[string]bool{}
	maxNum := 0.0
	for _, c := range have {
		taken[c.Number] = true
		exists[channelKey(c.Number, c.Name)] = true
		names[strings.ToLower(strings.TrimSpace(c.Name))] = true
		maxNum = max(maxNum, c.Number)
	}
	matchers := map[int64]*streamMatcher{}
	matcher := func(sourceID int64) (*streamMatcher, error) {
		if m, ok := matchers[sourceID]; ok {
			return m, nil
		}
		streams, _, err := st.LiveStreams(store.StreamQuery{SourceID: sourceID, Limit: 1 << 30})
		if err != nil {
			return nil, err
		}
		m := newStreamMatcher(streams)
		matchers[sourceID] = m
		return m, nil
	}

	chans := append([]dChannel(nil), setup.channels...)
	sort.SliceStable(chans, func(a, b int) bool {
		na, nb := chans[a].Number, chans[b].Number
		if na == nil || nb == nil {
			return na != nil
		}
		return *na < *nb
	})
	var noStreams []string
	renumbered := 0
	for _, dc := range chans {
		number := 0.0
		if dc.Number != nil {
			number = *dc.Number
		}
		// Importing again skips channels made last time. Unnumbered ones
		// were given a number then, so go by name.
		if exists[channelKey(number, dc.Name)] || (number <= 0 && names[strings.ToLower(strings.TrimSpace(dc.Name))]) {
			j.update(func(s *DispatcharrStatus) { s.Existing++ })
			continue
		}
		var ids []int64
		for _, sid := range dc.streams {
			ds, ok := setup.streams[sid]
			if !ok || ds.M3UAccount == nil {
				continue
			}
			srcID, ok := sourceOf[*ds.M3UAccount]
			if !ok {
				continue
			}
			m, err := matcher(srcID)
			if err != nil {
				return err
			}
			if id := m.find(ds, setup.groups); id != 0 {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			noStreams = append(noStreams, dc.Name)
			continue
		}
		if number <= 0 || taken[number] {
			if number > 0 {
				renumbered++
			}
			maxNum = float64(int(maxNum) + 1)
			number = maxNum
		}
		taken[number] = true
		maxNum = max(maxNum, number)
		c := &store.Channel{Number: number, Name: dc.Name, Enabled: !dc.HiddenFromOutput, EPGID: dc.TvgID}
		if dc.GroupID != nil {
			c.Group = setup.groups[*dc.GroupID]
		}
		if dc.EPGDataID != nil {
			if id := setup.epgData[*dc.EPGDataID]; id != "" {
				c.EPGID = id
			}
		}
		if dc.LogoID != nil {
			c.Logo = setup.logoURL(*dc.LogoID)
		}
		if err := st.CreateChannel(c, ids); err != nil {
			return err
		}
		j.update(func(s *DispatcharrStatus) { s.Channels++ })
	}
	if renumbered > 0 {
		j.note("%d channels had a number another channel already uses, so they were put at the end of the list.", renumbered)
	}
	if len(noStreams) > 0 {
		list := noStreams
		if len(list) > 10 {
			list = list[:10]
		}
		more := ""
		if len(noStreams) > len(list) {
			more = fmt.Sprintf(" and %d more", len(noStreams)-len(list))
		}
		j.note("%d channels were skipped because none of their streams were found in the imported playlists: %s%s.", len(noStreams), strings.Join(list, ", "), more)
	}
	return nil
}

func channelKey(number float64, name string) string {
	return strconv.FormatFloat(number, 'f', -1, 64) + "\x00" + strings.ToLower(strings.TrimSpace(name))
}

// logoURL is where the logo can be fetched. Logos uploaded to Dispatcharr
// only exist there, so those come from its (public) logo cache.
func (s *DispatcharrSetup) logoURL(id int64) string {
	l, ok := s.logos[id]
	if !ok {
		return ""
	}
	if strings.HasPrefix(l.URL, "http://") || strings.HasPrefix(l.URL, "https://") {
		return l.URL
	}
	return fmt.Sprintf("%s/api/channels/logos/%d/cache/", s.base, id)
}

// streamMatcher finds the BingeBandit copy of a Dispatcharr stream. Same URL
// is best; Xtream accounts are matched by the provider's stream id (the
// playlist and Dispatcharr may build the URL differently); otherwise by name
// within the group, then by a name that's unique in the playlist.
type streamMatcher struct {
	byURL, byXC, byNameGroup, byName map[string]int64
}

func newStreamMatcher(streams []*store.LiveStream) *streamMatcher {
	m := &streamMatcher{byURL: map[string]int64{}, byXC: map[string]int64{}, byNameGroup: map[string]int64{}, byName: map[string]int64{}}
	for _, ls := range streams {
		m.byURL[ls.URL] = ls.ID
		if k := xcStreamID(ls.URL); k != "" {
			m.byXC[k] = ls.ID
		}
		m.byNameGroup[strings.ToLower(ls.Group)+"\x00"+strings.ToLower(ls.Name)] = ls.ID
		key := strings.ToLower(ls.Name)
		if _, dup := m.byName[key]; dup {
			m.byName[key] = 0
		} else {
			m.byName[key] = ls.ID
		}
	}
	return m
}

func (m *streamMatcher) find(ds dStream, groups map[int64]string) int64 {
	if id := m.byURL[ds.URL]; id != 0 {
		return id
	}
	if ds.StreamID != nil {
		if id := m.byXC[strconv.FormatInt(*ds.StreamID, 10)]; id != 0 {
			return id
		}
	}
	if k := xcStreamID(ds.URL); k != "" {
		if id := m.byXC[k]; id != 0 {
			return id
		}
	}
	if ds.ChannelGroup != nil {
		if id := m.byNameGroup[strings.ToLower(groups[*ds.ChannelGroup])+"\x00"+strings.ToLower(ds.Name)]; id != 0 {
			return id
		}
	}
	return m.byName[strings.ToLower(ds.Name)]
}

// xcStreamID is the stream id at the end of an Xtream Codes live URL
// (…/live/user/pass/123.ts or …/user/pass/123), or "".
func xcStreamID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || strings.Count(strings.Trim(u.Path, "/"), "/") < 2 {
		return ""
	}
	base := path.Base(u.Path)
	base = strings.TrimSuffix(base, path.Ext(base))
	if _, err := strconv.ParseInt(base, 10, 64); err != nil {
		return ""
	}
	return base
}
