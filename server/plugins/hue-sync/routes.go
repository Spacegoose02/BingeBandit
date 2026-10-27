package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"bingebandit/pkg/bbplugin"
)

//go:embed page.html
var page []byte

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
		a.conf.update(func(c *Config) { *c = Config{People: c.People} })
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("PUT /lights", admin(a.assign))
	// Anyone signed in: the rooms and zones a device can pick for its lights.
	mux.HandleFunc("GET /areas", a.areas)
	mux.HandleFunc("POST /test", admin(a.test))
	// Anyone signed in: their own mode (off, sync or theater).
	mux.HandleFunc("GET /me", a.myMode)
	mux.HandleFunc("PUT /me", a.setMyMode)
	return mux
}

func (a *app) status(w http.ResponseWriter, r *http.Request) {
	conf := a.conf.get()
	status, active := a.engine.Status()
	resp := map[string]any{"bridge": conf.Bridge, "paired": conf.Username != "", "status": status, "active": active, "zones": Zones}
	if conf.Username != "" {
		lights, err := newBridge(conf.Bridge, conf.Username).Lights(r.Context())
		if err != nil {
			resp["error"] = err.Error()
		} else {
			type row struct {
				Light
				Zone string `json:"zone"`
			}
			rows := make([]row, 0, len(lights))
			for _, l := range lights {
				rows = append(rows, row{l, conf.Lights[l.ID]})
			}
			resp["lights"] = rows
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// areas lists the Hue rooms and zones, for the "Hue lights" choice on each
// device. Picking one syncs every light in it, chosen on the setup page or not.
func (a *app) areas(w http.ResponseWriter, r *http.Request) {
	if _, ok := bbplugin.UserOf(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in first"})
		return
	}
	type area struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Lights int    `json:"lights"`
	}
	out := []area{}
	conf := a.conf.get()
	if conf.Username == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	all, err := newBridge(conf.Bridge, conf.Username).Areas(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	for _, ar := range all {
		if len(ar.Lights) > 0 {
			out = append(out, area{ar.ID, ar.Name, ar.Type, len(ar.Lights)})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) myMode(w http.ResponseWriter, r *http.Request) {
	u, ok := bbplugin.UserOf(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in first"})
		return
	}
	conf := a.conf.get()
	writeJSON(w, http.StatusOK, map[string]any{"mode": conf.mode(u.ID), "paired": conf.Username != "", "modes": []string{ModeOff, ModeSync, ModeTheater}})
}

// setMyMode saves the person's mode and applies it at once to whatever
// they're playing, so the player's quick switch doesn't wait for a ping.
func (a *app) setMyMode(w http.ResponseWriter, r *http.Request) {
	u, ok := bbplugin.UserOf(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in first"})
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req)
	if req.Mode != ModeOff && req.Mode != ModeSync && req.Mode != ModeTheater {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode is off, sync or theater"})
		return
	}
	err := a.conf.update(func(c *Config) {
		if c.People == nil {
			c.People = map[string]string{}
		}
		key := strconv.FormatInt(u.ID, 10)
		if req.Mode == ModeOff {
			delete(c.People, key)
		} else {
			c.People[key] = req.Mode
		}
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	a.engine.setMode(context.WithoutCancel(r.Context()), u.ID, req.Mode)
	writeJSON(w, http.StatusOK, map[string]string{"mode": req.Mode})
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
	conf := a.conf.get()
	conf.Bridge, conf.Username = req.Address, user
	if err := a.conf.set(conf); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// assign sets which zone each light follows ("" for none).
func (a *app) assign(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "send {lightId: zone}"})
		return
	}
	conf := a.conf.get()
	for id, z := range req {
		if z != "" && !slices.Contains(Zones, z) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown zone " + z})
			return
		}
		if z == "" {
			delete(conf.Lights, id)
		} else {
			conf.Lights[id] = z
		}
	}
	if err := a.conf.set(conf); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// test runs the chosen lights through red, green and blue, then puts them back.
func (a *app) test(w http.ResponseWriter, r *http.Request) {
	conf := a.conf.get()
	if conf.Username == "" || len(conf.assigned()) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pair a bridge and choose lights first"})
		return
	}
	b := newBridge(conf.Bridge, conf.Username)
	all, err := b.Lights(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, rgb := range [][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}} {
			x, y := rgbToXY(rgb[0], rgb[1], rgb[2])
			for _, l := range conf.assigned() {
				b.SetState(ctx, l[0], map[string]any{"on": true, "xy": []float64{x, y}, "bri": 200, "transitiontime": 3})
			}
			time.Sleep(1200 * time.Millisecond)
		}
		for _, l := range all {
			if _, ok := conf.Lights[l.ID]; ok {
				b.Restore(ctx, l.ID, l.State)
			}
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
