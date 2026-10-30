package main

import (
	_ "embed"
	"maps"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"bingebandit/pkg/bbplugin"
)

//go:embed me.html
var mePage []byte

// meRoutes are each person's own choices: My Game Lights on the web, and
// the TV app's Sports tab.
func (a *app) meRoutes(mux *http.ServeMux) {
	signedIn := func(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			u, ok := bbplugin.UserOf(r)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in first"})
				return
			}
			h(w, r, strconv.FormatInt(u.ID, 10))
		}
	}
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(mePage)
	})
	mux.HandleFunc("GET /me/state", signedIn(a.myState))
	mux.HandleFunc("PUT /me/settings", signedIn(a.saveMine))
	mux.HandleFunc("POST /me/test", signedIn(a.testMine))
	mux.HandleFunc("POST /me/calibrate", signedIn(a.calibrate))
	mux.HandleFunc("GET /me/cues", signedIn(a.myCues))
	mux.HandleFunc("PUT /me/devices/{id}", signedIn(a.saveDevice))
}

func (a *app) myState(w http.ResponseWriter, r *http.Request, uid string) {
	conf := a.conf.get()
	p := conf.People[uid]
	if p.Source == "" {
		p.Source = Sources[0]
	}
	if p.Delays == nil {
		p.Delays = map[string]int{}
	}
	if p.Colours == nil {
		p.Colours = map[string][2]string{}
	}
	def := atoi(a.setting(r.Context(), "delay_seconds"), 25)
	sources := slices.Clone(Sources)
	if !slices.Contains(sources, p.Source) {
		sources = append(sources, p.Source)
	}
	for s := range p.Delays {
		if !slices.Contains(sources, s) {
			sources = append(sources, s)
		}
	}
	// Person drops empty maps from the saved file (omitempty), but the web
	// page and the TV app index into them, so they always go out as {}.
	person := struct {
		Person
		Delays  map[string]int       `json:"delays"`
		Colours map[string][2]string `json:"colours"`
	}{p, p.Delays, p.Colours}
	resp := map[string]any{"paired": conf.Username != "", "person": person, "delay": p.delay(def), "defaultDelay": def,
		"sources": sources, "areas": []Area{}, "effects": Effects}
	if conf.Username != "" {
		if areas, err := a.runner.bridge(conf).Areas(r.Context()); err != nil {
			resp["error"] = err.Error()
		} else {
			resp["areas"] = areas
		}
	}
	if at, text := a.follower.Last(uid); !at.IsZero() {
		resp["lastPlay"] = map[string]any{"text": text, "secondsAgo": int(time.Since(at).Seconds())}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *app) saveMine(w http.ResponseWriter, r *http.Request, uid string) {
	var p Person
	if !readBody(w, r, &p) {
		return
	}
	p.Source = strings.TrimSpace(p.Source)
	if len(p.Source) > 40 || len(p.Area) > 20 || len(p.Delays) > 20 || len(p.Colours) > 100 || len(p.Skip) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too long"})
		return
	}
	if p.Opponent != "" && p.Opponent != "red" && p.Opponent != "colour" && p.Opponent != "off" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "opponent is red, colour or off"})
		return
	}
	for _, d := range p.Delays {
		if d < 0 || d > 300 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "delays are 0 to 300 seconds"})
			return
		}
	}
	for _, c := range p.Colours {
		if !reHex.MatchString(c[0]) || !reHex.MatchString(c[1]) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "colours look like #861F41"})
			return
		}
	}
	a.save(w, func(c *Config) {
		if c.People == nil {
			c.People = map[string]Person{}
		}
		p.Devices = c.People[uid].Devices // each TV sets its own (PUT /me/devices/{id})
		c.People[uid] = p
	})
}

// testMine plays an effect now in the person's room, in their colours for
// the team they send (from their Sports teams).
func (a *app) testMine(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Effect string `json:"effect"`
		Team   Team   `json:"team"`
	}
	if !readBody(w, r, &req) {
		return
	}
	if req.Effect == "" {
		req.Effect = "touchdown"
	}
	if effectSteps(req.Effect, Palette{}, false) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown effect"})
		return
	}
	conf := a.conf.get()
	p := conf.People[uid]
	t := req.Team
	if t.ID == "" {
		t = a.sampleTeam("")
	}
	spots := p.spots(uid)
	if len(spots) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "turn Game Lights on for a TV first, in its Settings"})
		return
	}
	label := "Test for " + bbUser(r) + ": " + req.Effect + " (" + t.Short + ")"
	for _, s := range spots {
		if s.local {
			a.fireLocal(s.box, req.Effect, conf.paletteFor(t, p), label, 0)
		} else if conf.Username == "" || (s.area == "" && len(conf.Lights) == 0) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "an admin needs to pair the Hue bridge first, or pair this home's hub in the TV's Settings"})
			return
		} else {
			a.runnerFor(s.area).Fire(req.Effect, conf.paletteFor(t, p), label, 0)
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// calibrate sets the delay for the person's current source from the
// moment they saw the newest play of their game on TV.
func (a *app) calibrate(w http.ResponseWriter, r *http.Request, uid string) {
	at, text := a.follower.Last(uid)
	if at.IsZero() || time.Since(at) > 5*time.Minute {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no play from your games in the last 5 minutes; try during a game, right as a play shows on your TV"})
		return
	}
	delay := int(math.Round(time.Since(at).Seconds()))
	delay = min(delay, 300)
	var source string
	err := a.conf.update(func(c *Config) {
		if c.People == nil {
			c.People = map[string]Person{}
		}
		p := c.People[uid]
		if p.Source == "" {
			p.Source = Sources[0]
		}
		p.Delays = maps.Clone(p.Delays)
		if p.Delays == nil {
			p.Delays = map[string]int{}
		}
		p.Delays[p.Source] = delay
		source = p.Source
		c.People[uid] = p
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"delay": delay, "source": source, "play": text})
}

func bbUser(r *http.Request) string {
	u, _ := bbplugin.UserOf(r)
	return u.Username
}

// saveDevice sets one TV's own switch and room, leaving the person's other
// choices (and other TVs) as they are.
func (a *app) saveDevice(w http.ResponseWriter, r *http.Request, uid string) {
	id := r.PathValue("id")
	var d Device
	if !readBody(w, r, &d) {
		return
	}
	if len(id) > 64 || len(d.Name) > 60 || len(d.Area) > 20 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too long"})
		return
	}
	a.save(w, func(c *Config) {
		if c.People == nil {
			c.People = map[string]Person{}
		}
		p := c.People[uid]
		p.Devices = maps.Clone(p.Devices)
		if p.Devices == nil {
			p.Devices = map[string]Device{}
		}
		if _, known := p.Devices[id]; !known && len(p.Devices) >= 20 {
			// Forget an old TV that's off rather than grow without end.
			for k, o := range p.Devices {
				if !o.On {
					delete(p.Devices, k)
					break
				}
			}
		}
		p.Devices[id] = d
		c.People[uid] = p
	})
}
