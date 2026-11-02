package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"bingebandit/internal/sports"
	"bingebandit/internal/store"
)

// Which leagues the Sports section shows. An admin hides leagues for the
// whole server (Server → Settings → Sports data); each profile can then move
// leagues to the top and turn more rows off (prefs sportsOrder and
// sportsHidden). The TV, Roku and web apps draw a row per league in the
// order the server sends them, so they all follow this with no app change.

// splitLeagues reads a comma separated list of league ids.
func splitLeagues(v string) []string {
	var out []string
	for _, id := range strings.Split(v, ",") {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// validSportsLeagues checks and tidies the leagues an admin hides.
func validSportsLeagues(in map[string]string) error {
	v, ok := in[store.SettingSportsHiddenLeagues]
	if !ok {
		return nil
	}
	ids := splitLeagues(v)
	for _, id := range ids {
		if sports.LeagueByID(id) == nil {
			return errors.New("unknown league " + id)
		}
	}
	if len(ids) >= len(sports.Leagues) {
		return errors.New("leave at least one league in the Sports section")
	}
	in[store.SettingSportsHiddenLeagues] = strings.Join(ids, ",")
	return nil
}

// leagueAllowed reports whether a league is in the Sports section at all
// (known, and not hidden by an admin).
func (s *Server) leagueAllowed(id string) bool {
	return sports.LeagueByID(id) != nil && !slices.Contains(splitLeagues(s.Store.Setting(store.SettingSportsHiddenLeagues)), id)
}

// serverLeagues are the leagues an admin hasn't hidden, in the usual order.
func (s *Server) serverLeagues() []sports.League {
	hidden := splitLeagues(s.Store.Setting(store.SettingSportsHiddenLeagues))
	out := make([]sports.League, 0, len(sports.Leagues))
	for _, l := range sports.Leagues {
		if !slices.Contains(hidden, l.ID) {
			out = append(out, l)
		}
	}
	return out
}

// orderLeagues puts the leagues named in order first (in that order), then
// the rest as they came.
func orderLeagues(leagues []sports.League, order []string) []sports.League {
	out := make([]sports.League, 0, len(leagues))
	for _, id := range order {
		if i := slices.IndexFunc(leagues, func(l sports.League) bool { return l.ID == id }); i >= 0 && !slices.ContainsFunc(out, func(l sports.League) bool { return l.ID == id }) {
			out = append(out, leagues[i])
		}
	}
	for _, l := range leagues {
		if !slices.ContainsFunc(out, func(o sports.League) bool { return o.ID == l.ID }) {
			out = append(out, l)
		}
	}
	return out
}

// myLeagues are the person's league rows (shown) and every league they could
// show or follow teams in (all), both in their order.
func (s *Server) myLeagues(uid int64) (shown, all []sports.League) {
	all = s.serverLeagues()
	p, err := s.Store.UserPrefs(uid)
	if err != nil {
		return all, all
	}
	all = orderLeagues(all, p.SportsOrder)
	shown = make([]sports.League, 0, len(all))
	for _, l := range all {
		if !slices.Contains(p.SportsHidden, l.ID) {
			shown = append(shown, l)
		}
	}
	return shown, all
}

// sportsLeagueList is every league the server knows, with whether an admin
// hid it: for the admin's Sports data settings.
func (s *Server) sportsLeagueList(w http.ResponseWriter, r *http.Request) {
	hidden := splitLeagues(s.Store.Setting(store.SettingSportsHiddenLeagues))
	type league struct {
		sports.League
		Hidden bool `json:"hidden"`
	}
	out := make([]league, 0, len(sports.Leagues))
	for _, l := range sports.Leagues {
		out = append(out, league{l, slices.Contains(hidden, l.ID)})
	}
	writeJSON(w, http.StatusOK, out)
}
