package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
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
}

// session is the one player the lights follow.
type session struct {
	ev      playerEvent
	pos     float64   // file position at `at`
	at      time.Time // when pos was reported
	paused  bool
	sampler *Sampler
	ctx     context.Context
	cancel  context.CancelFunc
	sent    map[string]Colour // last colour sent to each light; guarded by Engine.mu
}

func (s *session) position(now time.Time) float64 {
	if s.paused {
		return s.pos
	}
	return s.pos + now.Sub(s.at).Seconds()
}

// Engine follows player events and drives the lights.
type Engine struct {
	a  *app
	mu sync.Mutex
	s  *session
	// saved is how the lights were before syncing started, for putting back.
	saved  map[string]LightState
	status string
}

const sampleFPS = 4 // frames read per second of video

func (e *Engine) onEvent(ctx context.Context, ev bbplugin.Event) {
	var p playerEvent
	if json.Unmarshal(ev.Data, &p) != nil || p.SessionID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	cur := e.s
	mine := cur != nil && cur.ev.SessionID == p.SessionID && cur.ev.ItemID == p.ItemID
	switch ev.Event {
	case "player.stop":
		if mine {
			e.endLocked(ctx, "stopped")
		}
		return
	case "player.play", "player.progress", "player.pause", "player.resume":
	default:
		return
	}
	if !mine {
		// A new player, or one we joined late. Only take over when it matches the filters.
		if !e.a.follows(ctx, p) || p.FileID == 0 {
			return
		}
		if cur != nil && ev.Event != "player.play" {
			return // keep following the current player; a new one says player.play
		}
		e.startLocked(ctx, p)
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

func (e *Engine) newSampler(ctx context.Context, fileID int64, from float64) *Sampler {
	url, key := e.a.fileURL(fileID)
	// Start a moment early so the first frames are there when the lights need them.
	return startSampler(ctx, e.a.ffmpeg, url, key, math.Max(0, from-1), sampleFPS, 45)
}

func (e *Engine) startLocked(ctx context.Context, p playerEvent) {
	if e.s != nil {
		e.s.cancel()
		e.s.sampler.Stop()
	}
	conf := e.a.conf.get()
	lights := conf.assigned()
	if conf.Username == "" || len(lights) == 0 {
		e.status = "Not set up: pair a bridge and choose lights"
		e.s = nil
		return
	}
	b := newBridge(conf.Bridge, conf.Username)
	if e.saved == nil {
		// Remember how the lights were, once per run of syncing.
		if all, err := b.Lights(ctx); err == nil {
			e.saved = map[string]LightState{}
			for _, l := range all {
				if _, ok := conf.Lights[l.ID]; ok {
					e.saved[l.ID] = l.State
				}
			}
		}
	}
	sctx, cancel := context.WithCancel(ctx)
	s := &session{ev: p, pos: p.Position, at: time.Now(), paused: p.Paused, ctx: sctx, cancel: cancel, sent: map[string]Colour{}}
	s.sampler = e.newSampler(sctx, p.FileID, p.Position)
	e.s = s
	e.status = "Following " + p.User + " (" + p.Client + "): " + p.Title
	log.Printf("following %s on %s: %s", p.User, p.Client, p.Title)
	go e.run(sctx, s, b, lights)
}

// run sends colours until the session ends. The bridge takes about ten
// light commands a second, so more lights mean fewer updates each.
func (e *Engine) run(ctx context.Context, s *session, b *Bridge, lights [][2]string) {
	ups, _ := strconv.ParseFloat(e.a.setting(ctx, "updates_per_second"), 64)
	ups = math.Max(0.5, math.Min(10, ups))
	interval := time.Duration(math.Max(1/ups, float64(len(lights))/10) * float64(time.Second))
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
		if e.s != s || s.paused {
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
				e.setStatus("Can't read the video: " + err.Error())
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
				e.setStatus("Bridge: " + err.Error())
				e.mu.Lock()
				delete(s.sent, c.id) // try again next tick
				e.mu.Unlock()
			}
		}
	}
}

func round4(f float64) float64 { return math.Round(f*1e4) / 1e4 }

func (e *Engine) setStatus(msg string) {
	e.mu.Lock()
	e.status = msg
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
	for _, l := range conf.assigned() {
		go b.SetState(ctx, l[0], map[string]any{"on": true, "ct": 366, "bri": hueBri(0.6, maxPct), "transitiontime": 10})
		delete(s.sent, l[0]) // resend colours on resume
	}
}

// endLocked stops following and puts the lights back, turns them off, or
// leaves them, as the admin chose.
func (e *Engine) endLocked(ctx context.Context, why string) {
	s := e.s
	if s == nil {
		return
	}
	s.cancel()
	s.sampler.Stop()
	e.s = nil
	e.status = "Idle (last: " + s.ev.Title + ", " + why + ")"
	conf := e.a.conf.get()
	b := newBridge(conf.Bridge, conf.Username)
	saved := e.saved
	e.saved = nil
	mode := e.a.setting(ctx, "on_stop")
	go func() {
		for _, l := range conf.assigned() {
			switch mode {
			case "off":
				b.SetState(ctx, l[0], map[string]any{"on": false, "transitiontime": 10})
			case "leave":
			default:
				if st, ok := saved[l[0]]; ok {
					b.Restore(ctx, l[0], st)
				}
			}
		}
	}()
}

func (e *Engine) Status() (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status, e.s != nil
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
