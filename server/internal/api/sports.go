package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/sports"
	"bingebandit/internal/store"
)

func (s *Server) sportsClient() *sports.Client {
	s.sportsOnce.Do(func() {
		if s.Sports == nil {
			s.Sports = sports.NewClient()
		}
		s.sportsWake = make(chan struct{}, 1)
	})
	return s.Sports
}

// sportsGame is a game with the user's teams marked, and where it's on in
// the Live TV guide.
type sportsGame struct {
	sports.Game
	Mine     []string       `json:"mine,omitempty"` // ids of the user's teams playing
	Channels []sportChannel `json:"channels,omitempty"`
}

type sportChannel struct {
	ID     int64   `json:"id"`
	Number float64 `json:"number"`
	Name   string  `json:"name"`
	Title  string  `json:"title"` // the guide entry
}

// sportsHome is the Sports page: the leagues, the user's teams, and their
// games from yesterday to a week ahead (live first, then upcoming, then
// recent results).
func (s *Server) sportsHome(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	teams, games, problems, err := s.myGames(r, 2, 7)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	sortGames(games, time.Now())
	if u.LiveTV {
		s.findChannels(u.ID, games)
	}
	resp := map[string]any{"leagues": sports.Leagues, "teams": teams, "games": orEmpty(games)}
	if len(problems) > 0 {
		sort.Strings(problems)
		resp["error"] = "Couldn't reach ESPN for " + strings.Join(problems, "; ")
	}
	writeJSON(w, http.StatusOK, resp)
}

// sportsLive is the games of your teams that are on now, for the player's
// score bug: just today's and yesterday's scoreboards, so it's cheap to poll.
func (s *Server) sportsLive(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	_, games, _, err := s.myGames(r, 1, 0)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	live := games[:0]
	for _, g := range games {
		if g.State == "in" {
			live = append(live, g)
		}
	}
	sortGames(live, time.Now())
	if u.LiveTV {
		s.findChannels(u.ID, live)
	}
	writeJSON(w, http.StatusOK, map[string]any{"games": orEmpty(live)})
}

// myGames lists the games of the person's teams from before days ago to
// after days ahead, with the leagues ESPN didn't answer for.
func (s *Server) myGames(r *http.Request, before, after int) ([]sports.Team, []sportsGame, []string, error) {
	teams, err := s.Store.SportsTeams(currentUser(r).ID)
	if err != nil {
		return nil, nil, nil, err
	}
	mine := map[string]map[string]bool{}
	for _, t := range teams {
		if mine[t.League] == nil {
			mine[t.League] = map[string]bool{}
		}
		mine[t.League][t.ID] = true
	}
	var games []sportsGame
	var problems []string
	for league, ids := range mine {
		all, err := s.sportsClient().Games(r.Context(), league, before, after)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", sports.LeagueByID(league).Name, err))
			continue
		}
		for _, g := range all {
			sg := sportsGame{Game: g}
			for _, id := range []string{g.Home.ID, g.Away.ID} {
				if ids[id] {
					sg.Mine = append(sg.Mine, id)
				}
			}
			if len(sg.Mine) > 0 {
				games = append(games, sg)
			}
		}
	}
	return teams, games, problems, nil
}

func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// sortGames puts live games first, then upcoming ones soonest first, then
// results newest first.
func sortGames(games []sportsGame, now time.Time) {
	rank := map[string]int{"in": 0, "pre": 1, "post": 2}
	sort.SliceStable(games, func(i, j int) bool {
		a, b := games[i], games[j]
		ra, rb := rank[a.State], rank[b.State]
		if a.State == "pre" && now.Sub(a.Start) > 12*time.Hour {
			ra = 3 // postponed and never played
		}
		if b.State == "pre" && now.Sub(b.Start) > 12*time.Hour {
			rb = 3
		}
		if ra != rb {
			return ra < rb
		}
		if a.State == "post" {
			return a.Start.After(b.Start)
		}
		return a.Start.Before(b.Start)
	})
}

// findChannels looks in the Live TV guide for each live or upcoming game: a
// programme around kick-off whose title names both teams.
func (s *Server) findChannels(uid int64, games []sportsGame) {
	var want []int
	from, to := int64(0), int64(0)
	for i, g := range games {
		if g.State == "post" || time.Until(g.Start) > 3*24*time.Hour {
			continue
		}
		st := g.Start.Unix()
		if from == 0 || st-3600 < from {
			from = st - 3600
		}
		if st+3600 > to {
			to = st + 3600
		}
		want = append(want, i)
	}
	if len(want) == 0 {
		return
	}
	chans, err := s.Store.Channels(uid, false)
	if err != nil {
		return
	}
	byEPG := map[string][]*store.Channel{}
	var ids []string
	for _, c := range chans {
		if c.EPGID != "" {
			if byEPG[c.EPGID] == nil {
				ids = append(ids, c.EPGID)
			}
			byEPG[c.EPGID] = append(byEPG[c.EPGID], c)
		}
	}
	progs, err := s.Store.Programs(ids, from, to)
	if err != nil {
		return
	}
	for _, i := range want {
		g := &games[i]
		st := g.Start.Unix()
		for _, p := range progs {
			if p.Start > st+30*60 || p.Stop <= st || p.Start < st-90*60 {
				continue
			}
			text := p.Title + " " + p.Subtitle
			if !namesTeam(text, g.Home.Team) || !namesTeam(text, g.Away.Team) {
				continue
			}
			for _, c := range byEPG[p.EPGID] {
				if len(g.Channels) < 6 {
					g.Channels = append(g.Channels, sportChannel{c.ID, c.Number, c.Name, p.Title})
				}
			}
		}
	}
}

// namesTeam reports whether a guide title names the team, by its nickname
// ("Hokies") or its place ("Virginia Tech"), as whole words.
func namesTeam(text string, t sports.Team) bool {
	for _, n := range []string{t.Short, strings.TrimSpace(strings.TrimSuffix(t.Name, t.Short)), t.Name} {
		if len(n) < 3 {
			continue
		}
		if hasWord(strings.ToLower(text), strings.ToLower(n)) {
			return true
		}
	}
	return false
}

// hasWord reports whether word appears in text with no letter or digit
// either side of it.
func hasWord(text, word string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], word)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(word)
		if (start == 0 || !isWordByte(text[start-1])) && (end == len(text) || !isWordByte(text[end])) {
			return true
		}
		i = start + 1
	}
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b >= 0x80
}

func (s *Server) sportsTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := s.sportsClient().Teams(r.Context(), chi.URLParam(r, "league"))
	if errors.Is(err, sports.ErrUnknownLeague) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, teams)
}

var reDay = regexp.MustCompile(`^\d{8}$`)

// sportsScores is one league's whole scoreboard for a day (today by default).
func (s *Server) sportsScores(w http.ResponseWriter, r *http.Request) {
	day := r.URL.Query().Get("date")
	if day == "" {
		day = sports.Day(time.Now())
	}
	if !reDay.MatchString(day) {
		writeErr(w, http.StatusBadRequest, "date must be YYYYMMDD")
		return
	}
	games, err := s.sportsClient().Scoreboard(r.Context(), chi.URLParam(r, "league"), day)
	if errors.Is(err, sports.ErrUnknownLeague) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, games)
}

// setSportsTeams replaces the user's teams. Each is looked up on ESPN, so
// names, logos and colours come from there rather than from the client.
func (s *Server) setSportsTeams(w http.ResponseWriter, r *http.Request) {
	var req []struct {
		League string `json:"league"`
		ID     string `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "send [{league, id}]")
		return
	}
	if len(req) > store.MaxSportsTeams {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("follow at most %d teams", store.MaxSportsTeams))
		return
	}
	u := currentUser(r)
	have, err := s.Store.SportsTeams(u.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	known := map[string]sports.Team{}
	for _, t := range have {
		known[t.League+"/"+t.ID] = t
	}
	teams := make([]sports.Team, 0, len(req))
	for _, x := range req {
		t, ok := known[x.League+"/"+x.ID]
		if !ok {
			if t, err = s.sportsClient().Team(r.Context(), x.League, x.ID); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		teams = append(teams, t)
	}
	if err := s.Store.SetSportsTeams(u.ID, teams); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.lookSoon()
	writeJSON(w, http.StatusOK, teams)
}

// lookSoon has the tracker look again now, so new teams' games are known.
func (s *Server) lookSoon() {
	s.sportsClient()
	select {
	case s.sportsWake <- struct{}{}:
	default:
	}
}

// RunSports follows the teams people picked until ctx ends: it sends the
// sports.* events to plugins and the game alerts to people.
func (s *Server) RunSports(ctx context.Context) {
	t := &sports.Tracker{Client: s.sportsClient(), Follows: s.sportsFollows, On: s.onSports}
	for {
		wait := t.Look(ctx)
		s.Store.PruneSportsAlerts()
		select {
		case <-ctx.Done():
			return
		case <-s.sportsWake:
		case <-time.After(wait):
		}
	}
}

func (s *Server) sportsFollows() (map[string]map[string]bool, error) {
	fans, err := s.Store.SportsFans()
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]bool{}
	for _, f := range fans {
		if out[f.League] == nil {
			out[f.League] = map[string]bool{}
		}
		out[f.League][f.TeamID] = true
	}
	return out, nil
}

// sportsEvent is what plugins get (sports.game.start, sports.score,
// sports.game.end, and sports.game.live on every look while a game is on,
// so a plugin that starts mid-game can join in): the game, what happened, and who follows
// each team playing.
type sportsEvent struct {
	Game   sports.Game       `json:"game"`
	Sport  string            `json:"sport"`
	Scorer string            `json:"scorer,omitempty"` // team id, for sports.score
	Fans   []store.SportsFan `json:"fans"`
}

func (s *Server) onSports(ctx context.Context, changes []sports.Change, games []sports.Game) {
	fans, err := s.Store.SportsFans()
	if err != nil {
		return
	}
	fansOf := func(g sports.Game) []store.SportsFan {
		var out []store.SportsFan
		for _, f := range fans {
			if f.League == g.League && g.Has(f.TeamID) {
				out = append(out, f)
			}
		}
		return out
	}
	event := func(g sports.Game, scorer string) sportsEvent {
		ev := sportsEvent{Game: g, Scorer: scorer, Fans: fansOf(g)}
		if l := sports.LeagueByID(g.League); l != nil {
			ev.Sport = l.Sport
		}
		return ev
	}
	for _, c := range changes {
		switch c.Kind {
		case "start":
			s.Emit("sports.game.start", event(c.Game, ""))
		case "score":
			s.Emit("sports.score", event(c.Game, c.Scorer))
		case "end":
			s.Emit("sports.game.end", event(c.Game, ""))
			s.sportsResult(c.Game, fansOf(c.Game))
		}
	}
	now := time.Now()
	for _, g := range games {
		switch {
		case g.State == "in":
			s.Emit("sports.game.live", event(g, ""))
		case g.State == "pre" && g.Start.After(now):
			s.sportsSoon(g, fansOf(g), now)
		}
	}
}

// sportsSoon alerts the fans whose alert time before the game has come.
func (s *Server) sportsSoon(g sports.Game, fans []store.SportsFan, now time.Time) {
	for _, f := range fans {
		p, err := s.Store.UserPrefs(f.UserID)
		if err != nil || p.GameAlertMinutes == 0 || g.Start.Sub(now) > time.Duration(p.GameAlertMinutes)*time.Minute {
			continue
		}
		if fresh, err := s.Store.MarkSportsAlert(f.UserID, g.ID, "soon"); err != nil || !fresh {
			continue
		}
		us, them := g.Sides(f.TeamID)
		mins := int(g.Start.Sub(now).Round(time.Minute).Minutes())
		n := &store.Notification{UserID: f.UserID, Kind: "sports", Link: "/sports", Image: us.Logo}
		n.Title = fmt.Sprintf("%s %s %s starts in %d minutes", us.Short, vsOrAt(us), them.Short, mins)
		if mins <= 1 {
			n.Title = fmt.Sprintf("%s %s %s is starting", us.Short, vsOrAt(us), them.Short)
		}
		n.Body = strings.Join(g.Broadcasts, ", ")
		if u, err := s.Store.UserByID(f.UserID); err == nil && u.LiveTV {
			list := []sportsGame{{Game: g}}
			s.findChannels(f.UserID, list)
			if len(list[0].Channels) > 0 {
				c := list[0].Channels[0]
				n.Body = strings.TrimPrefix(n.Body+" · On "+c.Name, " · ")
				n.Link = fmt.Sprintf("/live/%d", c.ID)
			}
		}
		if err := s.Store.AddNotification(n); err != nil {
			slog.Warn("notification", "err", err)
		}
	}
}

// sportsResult tells fans who want final scores how the game ended.
func (s *Server) sportsResult(g sports.Game, fans []store.SportsFan) {
	for _, f := range fans {
		p, err := s.Store.UserPrefs(f.UserID)
		if err != nil || !p.NotifyGameScores {
			continue
		}
		if fresh, err := s.Store.MarkSportsAlert(f.UserID, g.ID, "final"); err != nil || !fresh {
			continue
		}
		us, them := g.Sides(f.TeamID)
		verb := "lost to"
		switch {
		case us.Winner:
			verb = "beat"
		case !them.Winner && us.Score == them.Score:
			verb = "tied"
		}
		n := &store.Notification{UserID: f.UserID, Kind: "sports", Link: "/sports", Image: us.Logo,
			Title: fmt.Sprintf("%s %s %s %s–%s", us.Short, verb, them.Short, us.Score, them.Score), Body: g.Detail}
		if err := s.Store.AddNotification(n); err != nil {
			slog.Warn("notification", "err", err)
		}
	}
}

func vsOrAt(us *sports.Side) string {
	if us.Home {
		return "vs"
	}
	return "at"
}
