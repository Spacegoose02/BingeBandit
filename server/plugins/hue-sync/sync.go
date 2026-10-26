package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/pkg/bbplugin"
)

// playerEvent is the part of a player.* event the sync needs.
type playerEvent struct {
	SessionID string  `json:"sessionId"`
	UserID    int64   `json:"userId"`
	User      string  `json:"user"`
	Client    string  `json:"client"`
	ItemID    int64   `json:"itemId"`
	FileID    int64   `json:"fileId"`
	Title     string  `json:"title"`
	Position  float64 `json:"position"`
	Paused    bool    `json:"paused"`
	// Lights is the device's Hue Sync switch; servers that predate it leave it out.
	Lights *bool `json:"lights"`
	// Area is the Hue room or zone the device picked; "" for every chosen light.
	Area string `json:"lightsArea"`
}

// lightsOff reports a device whose user turned Hue Sync off in the app.
func (p playerEvent) lightsOff() bool { return p.Lights != nil && !*p.Lights }

// session is one player and the lights that follow it. Players in different
// rooms each drive their own lights at the same time.
type session struct {
	ev      playerEvent
	pos     float64   // file position at `at`
	at      time.Time // when pos was reported
	paused  bool
	sampler *Sampler
	ctx     context.Context
	cancel  context.CancelFunc
	lights  [][2]string           // light id and the part of the picture it follows
	saved   map[string]LightState // how its lights were before, for putting back
	sent    map[string]Colour     // last colour sent to each light; guarded by Engine.mu
	problem string                // the last error, for the status; guarded by Engine.mu
}

func (s *session) position(now time.Time) float64 {
	if s.paused {
		return s.pos
	}
	return s.pos + now.Sub(s.at).Seconds()
}

// overlaps reports whether two sessions would drive any of the same lights.
func overlaps(a, b [][2]string) bool {
	for _, x := range a {
		for _, y := range b {
			if x[0] == y[0] {
				return true
			}
		}
	}
	return false
}

// Engine follows player events and drives the lights.
type Engine struct {
	a        *app
	mu       sync.Mutex
	sessions map[string]*session // by player session id
	status   string              // the last thing that happened, when nothing plays
}

const sampleFPS = 4 // frames read per second of video

func (e *Engine) onEvent(ctx context.Context, ev bbplugin.Event) {
	var p playerEvent
	if json.Unmarshal(ev.Data, &p) != nil || p.SessionID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sessions == nil {
		e.sessions = map[string]*session{}
	}
	cur := e.sessions[p.SessionID]
	mine := cur != nil && cur.ev.ItemID == p.ItemID && cur.ev.Area == p.Area
	switch ev.Event {
	case "player.stop":
		if cur != nil {
			e.endLocked(ctx, cur, "stopped")
		}
		return
	case "player.play", "player.progress", "player.pause", "player.resume":
	default:
		return
	}
	if p.lightsOff() {
		// Hue Sync is off on this device (perhaps just switched off mid-play).
		if cur != nil {
			e.endLocked(ctx, cur, "turned off on "+p.Client)
		}
		return
	}
	if !mine {
		// A new player, one we joined late, or the device moved to other lights.
		// Only take over when it matches the filters.
		if !e.a.follows(ctx, p) || p.FileID == 0 {
			return
		}
		lights, err := e.a.lightsFor(ctx, p.Area)
		if err != "" {
			e.status = err
			return
		}
		var clash []*session
		for _, o := range e.sessions {
			if o != cur && overlaps(o.lights, lights) {
				clash = append(clash, o)
			}
		}
		if len(clash) > 0 && ev.Event != "player.play" && cur == nil {
			return // keep following the players these lights already follow; a new one says player.play
		}
		for _, o := range clash {
			e.endLocked(ctx, o, "taken over by "+p.Client)
		}
		if cur != nil {
			e.endLocked(ctx, cur, "moved on")
		}
		e.startLocked(ctx, p, lights)
		return
	}
	now := time.Now()
	drift := math.Abs(cur.position(now) - p.Position)
	wasPaused := cur.paused
	cur.ev, cur.pos, cur.at, cur.paused = p, p.Position, now, p.Paused
	if drift > 2.5 && !cur.sampler.Covers(p.Position) {
		// A seek outside what's been read: read again from there.
		cur.sampler.Stop()
		cur.sampler = e.newSampler(cur.ctx, p.FileID, p.Position)
	}
	if p.Paused && !wasPaused {
		e.pauseLocked(ctx, cur)
	}
}

// lightsFor is the lights a device's area covers: every light in its room
// or zone (a chosen light keeps its part of the picture; the rest follow the
// whole picture), or every chosen light when it picked none or its room or
// zone is gone. err says why there are none.
func (a *app) lightsFor(ctx context.Context, area string) (lights [][2]string, err string) {
	conf := a.conf.get()
	lights = conf.assigned()
	if conf.Username == "" || (len(lights) == 0 && area == "") {
		return nil, "Not set up: pair a bridge and choose lights"
	}
	if area == "" {
		return lights, ""
	}
	areas, aerr := newBridge(conf.Bridge, conf.Username).Areas(ctx)
	if aerr != nil {
		return nil, "Bridge: " + aerr.Error()
	}
	for _, ar := range areas {
		if ar.ID != area {
			continue
		}
		var in [][2]string
		for _, id := range ar.Lights {
			zone := conf.Lights[id]
			if zone == "" {
				zone = "all"
			}
			in = append(in, [2]string{id, zone})
		}
		if len(in) == 0 {
			return nil, ar.Name + " has no lights"
		}
		return in, ""
	}
	if len(lights) == 0 {
		return nil, "That Hue room or zone is gone; pick another"
	}
	return lights, ""
}

func (e *Engine) newSampler(ctx context.Context, fileID int64, from float64) *Sampler {
	url, key := e.a.fileURL(fileID)
	// Start a moment early so the first frames are there when the lights need them.
	return startSampler(ctx, e.a.ffmpeg, url, key, math.Max(0, from-1), sampleFPS, 45)
}

func (e *Engine) startLocked(ctx context.Context, p playerEvent, lights [][2]string) {
	conf := e.a.conf.get()
	b := newBridge(conf.Bridge, conf.Username)
	saved := map[string]LightState{}
	if all, err := b.Lights(ctx); err == nil {
		for _, l := range all {
			for _, mine := range lights {
				if mine[0] == l.ID {
					saved[l.ID] = l.State
				}
			}
		}
	}
	sctx, cancel := context.WithCancel(ctx)
	s := &session{ev: p, pos: p.Position, at: time.Now(), paused: p.Paused, ctx: sctx, cancel: cancel,
		lights: lights, saved: saved, sent: map[string]Colour{}}
	s.sampler = e.newSampler(sctx, p.FileID, p.Position)
	e.sessions[p.SessionID] = s
	log.Printf("following %s on %s: %s (%d lights)", p.User, p.Client, p.Title, len(lights))
	go e.run(sctx, s, b)
}

// activeLights counts the lights every session drives, which share the bridge.
func (e *Engine) activeLights() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, s := range e.sessions {
		n += len(s.lights)
	}
	return n
}

// run sends colours until the session ends. The bridge takes about ten
// light commands a second, so more lights (in every room playing) mean
// fewer updates each.
func (e *Engine) run(ctx context.Context, s *session, b *Bridge) {
	lights := s.lights
	ups, _ := strconv.ParseFloat(e.a.setting(ctx, "updates_per_second"), 64)
	ups = math.Max(0.5, math.Min(10, ups))
	interval := time.Duration(math.Max(1/ups, float64(max(e.activeLights(), len(lights)))/10) * float64(time.Second))
	transition := int(math.Round(interval.Seconds() * 10)) // tenths of a second
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		maxPct, _ := strconv.ParseFloat(e.a.setting(ctx, "brightness"), 64)
		if maxPct <= 0 || maxPct > 100 {
			maxPct = 100
		}
		delay, _ := strconv.ParseFloat(e.a.setting(ctx, "delay_ms"), 64)
		e.mu.Lock()
		if e.sessions[s.ev.SessionID] != s || s.paused {
			e.mu.Unlock()
			continue
		}
		at := s.position(time.Now()) - delay/1000
		zones, ok := s.sampler.At(at)
		sampler := s.sampler
		type cmd struct {
			id string
			c  Colour
		}
		var todo []cmd
		for _, l := range lights {
			c, have := zones[l[1]]
			if !ok || !have {
				continue
			}
			if last, had := s.sent[l[0]]; had && math.Hypot(last.X-c.X, last.Y-c.Y) < 0.004 && math.Abs(last.Bri-c.Bri) < 0.01 {
				continue // no visible change
			}
			s.sent[l[0]] = c
			todo = append(todo, cmd{l[0], c})
		}
		e.mu.Unlock()
		if !ok {
			if err := sampler.Err(); err != nil {
				e.setProblem(s, "can't read the video: "+err.Error())
			}
			continue
		}
		for _, c := range todo {
			cctx, cancel := context.WithTimeout(ctx, time.Second)
			err := b.SetState(cctx, c.id, map[string]any{
				"on": true, "xy": []float64{round4(c.c.X), round4(c.c.Y)}, "bri": hueBri(c.c.Bri, maxPct), "transitiontime": transition,
			})
			cancel()
			if err != nil {
				e.setProblem(s, "bridge: "+err.Error())
				e.mu.Lock()
				delete(s.sent, c.id) // try again next tick
				e.mu.Unlock()
			}
		}
	}
}

func round4(f float64) float64 { return math.Round(f*1e4) / 1e4 }

// setProblem notes what's going wrong for a session, for the status line.
func (e *Engine) setProblem(s *session, msg string) {
	e.mu.Lock()
	s.problem = msg
	e.mu.Unlock()
}

// pauseLocked brings the lights up to a warm white when the admin chose that.
func (e *Engine) pauseLocked(ctx context.Context, s *session) {
	if e.a.setting(ctx, "on_pause") != "bright" {
		return
	}
	conf := e.a.conf.get()
	b := newBridge(conf.Bridge, conf.Username)
	maxPct, _ := strconv.ParseFloat(e.a.setting(ctx, "brightness"), 64)
	if maxPct <= 0 || maxPct > 100 {
		maxPct = 100
	}
	for _, l := range s.lights {
		go b.SetState(ctx, l[0], map[string]any{"on": true, "ct": 366, "bri": hueBri(0.6, maxPct), "transitiontime": 10})
		delete(s.sent, l[0]) // resend colours on resume
	}
}

// endLocked stops following one player and puts its lights back, turns
// them off, or leaves them, as the admin chose.
func (e *Engine) endLocked(ctx context.Context, s *session, why string) {
	s.cancel()
	s.sampler.Stop()
	delete(e.sessions, s.ev.SessionID)
	e.status = "Idle (last: " + s.ev.Title + ", " + why + ")"
	conf := e.a.conf.get()
	b := newBridge(conf.Bridge, conf.Username)
	mode := e.a.setting(ctx, "on_stop")
	go func() {
		for _, l := range s.lights {
			switch mode {
			case "off":
				b.SetState(ctx, l[0], map[string]any{"on": false, "transitiontime": 10})
			case "leave":
			default:
				if st, ok := s.saved[l[0]]; ok {
					b.Restore(ctx, l[0], st)
				}
			}
		}
	}()
}

// Status says who the lights follow, one line per player, or what happened last.
func (e *Engine) Status() (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.sessions) == 0 {
		if e.status == "" {
			return "Idle", false
		}
		return e.status, false
	}
	var lines []string
	for _, s := range e.sessions {
		line := fmt.Sprintf("Following %s (%s): %s, %d lights", s.ev.User, s.ev.Client, s.ev.Title, len(s.lights))
		if s.problem != "" {
			line += " (" + s.problem + ")"
		}
		lines = append(lines, line)
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n"), true
}

// follows applies the admin's filters (people and devices, comma-separated).
func (a *app) follows(ctx context.Context, p playerEvent) bool {
	match := func(list, v string) bool {
		list = strings.TrimSpace(list)
		if list == "" {
			return true
		}
		for _, x := range strings.Split(list, ",") {
			if strings.EqualFold(strings.TrimSpace(x), v) {
				return true
			}
		}
		return false
	}
	return match(a.setting(ctx, "users"), p.User) && match(a.setting(ctx, "clients"), p.Client)
}
