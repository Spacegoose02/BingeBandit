package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"bingebandit/pkg/bbplugin"
)

var (
	//go:embed page.html
	page []byte
	// replay is a handful of sample plays for "Try a sample game".
	//go:embed replay.json
	replay []byte
)

// routes are the setup page and its calls, for admins only.
func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	admin := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if u, ok := bbplugin.UserOf(r); !ok || !u.Admin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "admins only"})
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /{$}", admin(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(page)
	}))
	mux.HandleFunc("GET /status", admin(a.status))
	mux.HandleFunc("POST /discover", admin(func(w http.ResponseWriter, r *http.Request) {
		ips, err := discover(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "couldn't reach Philips' discovery service; type the bridge's address instead (" + err.Error() + ")"})
			return
		}
		writeJSON(w, http.StatusOK, ips)
	}))
	mux.HandleFunc("POST /pair", admin(a.pair))
	mux.HandleFunc("POST /unpair", admin(func(w http.ResponseWriter, r *http.Request) {
		a.conf.update(func(c *Config) { c.Bridge, c.Username, c.Lights = "", "", nil })
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("PUT /lights", admin(func(w http.ResponseWriter, r *http.Request) {
		var ids []string
		if !readBody(w, r, &ids) {
			return
		}
		a.save(w, func(c *Config) { c.Lights = ids })
	}))
	mux.HandleFunc("PUT /effects", admin(func(w http.ResponseWriter, r *http.Request) {
		var on map[string]bool
		if !readBody(w, r, &on) {
			return
		}
		a.save(w, func(c *Config) {
			for k, v := range on {
				c.Off = slices.DeleteFunc(c.Off, func(x string) bool { return x == k })
				if !v {
					c.Off = append(c.Off, k)
				}
			}
			sort.Strings(c.Off)
		})
	}))
	mux.HandleFunc("PUT /colours", admin(a.setColours))
	mux.HandleFunc("POST /test", admin(a.test))
	mux.HandleFunc("POST /sample", admin(a.sample))
	mux.HandleFunc("POST /reset", admin(func(w http.ResponseWriter, r *http.Request) {
		go a.runner.Reset()
		w.WriteHeader(http.StatusAccepted)
	}))
	return mux
}

func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad body"})
		return false
	}
	return true
}

func (a *app) save(w http.ResponseWriter, f func(*Config)) {
	if err := a.conf.update(f); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type teamRow struct {
	Team
	Key     string    `json:"key"`
	Colours [2]string `json:"colours"`
	Custom  bool      `json:"custom"`
}

func (a *app) status(w http.ResponseWriter, r *http.Request) {
	conf := a.conf.get()
	resp := map[string]any{"bridge": conf.Bridge, "paired": conf.Username != "", "games": a.follower.Games()}
	type effect struct {
		Key   string `json:"key"`
		Label string `json:"label"`
		On    bool   `json:"on"`
	}
	effects := []effect{}
	for _, e := range Effects {
		effects = append(effects, effect{e.Key, e.Label, !contains(conf.Off, e.Key)})
	}
	resp["effects"] = effects
	teams := []teamRow{}
	for k, t := range conf.Teams {
		_, custom := conf.Colours[k]
		teams = append(teams, teamRow{t, k, conf.palette(t).Hex, custom})
	}
	sort.Slice(teams, func(i, j int) bool { return teams[i].Name < teams[j].Name })
	resp["teams"] = teams
	a.mu.Lock()
	// Cloned into a non-nil slice so an empty log goes out as [], not null.
	logs := append([]logLine{}, a.log...)
	a.mu.Unlock()
	slices.Reverse(logs)
	resp["log"] = logs
	if conf.Username != "" {
		lights, err := newBridge(conf.Bridge, conf.Username).Lights(r.Context())
		if err != nil {
			resp["error"] = err.Error()
		} else {
			type row struct {
				Light
				On bool `json:"chosen"`
			}
			rows := make([]row, 0, len(lights))
			for _, l := range lights {
				rows = append(rows, row{l, slices.Contains(conf.Lights, l.ID)})
			}
			resp["lights"] = rows
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *app) pair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
	req.Address = strings.TrimSpace(req.Address)
	if req.Address == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enter the bridge's address"})
		return
	}
	user, err := newBridge(req.Address, "").Pair(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	a.save(w, func(c *Config) { c.Bridge, c.Username = req.Address, user })
}

var reHex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// setColours sets a team's two colours, or with null goes back to ESPN's.
func (a *app) setColours(w http.ResponseWriter, r *http.Request) {
	var req map[string]*[2]string
	if !readBody(w, r, &req) {
		return
	}
	for _, c := range req {
		if c != nil && (!reHex.MatchString(c[0]) || !reHex.MatchString(c[1])) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "colours look like #861F41"})
			return
		}
	}
	a.save(w, func(c *Config) {
		for k, v := range req {
			if v == nil {
				delete(c.Colours, k)
				continue
			}
			if c.Colours == nil {
				c.Colours = map[string][2]string{}
			}
			c.Colours[k] = *v
		}
	})
}

// sampleTeam colours tests when no team has been followed yet.
func (a *app) sampleTeam(key string) Team {
	conf := a.conf.get()
	if t, ok := conf.Teams[key]; ok {
		return t
	}
	for _, t := range conf.Teams {
		return t
	}
	var rp struct {
		Team Team `json:"team"`
	}
	json.Unmarshal(replay, &rp)
	return rp.Team
}

// test plays one effect now.
func (a *app) test(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Effect string `json:"effect"`
		Team   string `json:"team"`
	}
	if !readBody(w, r, &req) {
		return
	}
	if effectSteps(req.Effect, Palette{}, false) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown effect"})
		return
	}
	if !a.ready(w) {
		return
	}
	t := a.sampleTeam(req.Team)
	a.runner.Fire(req.Effect, a.conf.get().palette(t), "Test: "+req.Effect+" ("+t.Short+")", 0)
	w.WriteHeader(http.StatusAccepted)
}

func (a *app) ready(w http.ResponseWriter) bool {
	if conf := a.conf.get(); conf.Username == "" || len(conf.Lights) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pair a bridge and choose lights first"})
		return false
	}
	return true
}

// sample runs the sample plays through the classifier and the lights, a
// few seconds apart as in a game, but without the TV delay.
func (a *app) sample(w http.ResponseWriter, r *http.Request) {
	if !a.ready(w) {
		return
	}
	var rp struct {
		Team  Team `json:"team"`
		Plays []struct {
			Play
			WaitMs int `json:"waitMs"`
		} `json:"plays"`
	}
	if err := json.Unmarshal(replay, &rp); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	conf := a.conf.get()
	pal := conf.palette(rp.Team)
	a.logf("Sample game: %d plays for the %s", len(rp.Plays), rp.Team.Short)
	at := time.Duration(0)
	for _, p := range rp.Plays {
		if e, ok := classify(p.Play, rp.Team.ID); ok {
			if key := effectKey(e); key != "" && !contains(conf.Off, key) {
				a.runner.Fire(key, pal, "Sample: "+truncate(p.Text, 60), at)
			}
		}
		at += time.Duration(p.WaitMs) * time.Millisecond
	}
	w.WriteHeader(http.StatusAccepted)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
