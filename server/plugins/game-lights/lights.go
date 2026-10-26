package main

import (
	"context"
	"math"
	"slices"
	"sync"
	"time"
)

// bridgeAPI is what the light runner needs from a bridge (tests fake it).
type bridgeAPI interface {
	Lights(ctx context.Context) ([]Light, error)
	SetState(ctx context.Context, id string, state map[string]any) error
	Restore(ctx context.Context, id string, s LightState) error
	Areas(ctx context.Context) ([]Area, error)
}

// show is one effect waiting to run.
type show struct {
	key   string
	pal   Palette
	label string
}

// Runner plays effects on the chosen lights one at a time. Before the first
// it notes how the lights were; once nothing is left to play it puts them
// back and checks they took it.
type Runner struct {
	a      *app
	area   string // Hue room or zone whose lights it plays on; "" = the admin's ticked lights
	bridge func(Config) bridgeAPI
	// gap is the least time per light change: the bridge takes about ten a second.
	gap time.Duration

	mu       sync.Mutex
	queue    []show
	running  bool
	pending  int
	baseline map[string]LightState
	stop     context.CancelFunc
}

// Fire plays an effect after delay (how far the TV runs behind ESPN).
func (r *Runner) Fire(key string, pal Palette, label string, delay time.Duration) {
	r.mu.Lock()
	r.pending++
	r.mu.Unlock()
	time.AfterFunc(delay, func() {
		r.mu.Lock()
		r.pending--
		r.queue = append(r.queue, show{key, pal, label})
		start := !r.running
		r.running = true
		r.mu.Unlock()
		if start {
			go r.drain()
		}
	})
}

// Idle reports whether nothing is waiting or playing.
func (r *Runner) Idle() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.running && r.pending == 0 && len(r.queue) == 0
}

func (r *Runner) drain() {
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.stop = cancel
	r.mu.Unlock()
	defer cancel()
	for {
		r.mu.Lock()
		if len(r.queue) == 0 || ctx.Err() != nil {
			r.queue = nil
			r.mu.Unlock()
			break
		}
		next := r.queue[0]
		r.queue = r.queue[1:]
		r.mu.Unlock()
		r.play(ctx, next)
	}
	r.putBack()
	r.mu.Lock()
	r.running = false
	again := len(r.queue) > 0
	if again {
		r.running = true
	}
	r.mu.Unlock()
	if again {
		go r.drain()
	}
}

func (r *Runner) play(ctx context.Context, s show) {
	conf := r.a.conf.get()
	if conf.Username == "" || (r.area == "" && len(conf.Lights) == 0) {
		r.a.logf("%s: no lights chosen yet", s.label)
		return
	}
	steps := effectSteps(s.key, s.pal, r.a.setting(ctx, "reduced_flashing") == "on")
	if len(steps) == 0 {
		return
	}
	b := r.bridge(conf)
	lights, err := b.Lights(ctx)
	if err != nil {
		r.a.logf("%s: %v", s.label, err)
		return
	}
	ids := conf.Lights
	if r.area != "" {
		ids = nil
		areas, err := b.Areas(ctx)
		if err != nil {
			r.a.logf("%s: %v", s.label, err)
			return
		}
		for _, ar := range areas {
			if ar.ID == r.area {
				ids = ar.Lights
			}
		}
		if ids == nil {
			r.a.logf("%s: that Hue room or zone is gone; pick another on My Game Lights", s.label)
			return
		}
	}
	var chosen []Light
	for _, l := range lights {
		if slices.Contains(ids, l.ID) {
			chosen = append(chosen, l)
		}
	}
	// Keep the state from before the first effect until a restore is
	// confirmed, so a failed restore never becomes the new normal.
	r.mu.Lock()
	if r.baseline == nil {
		r.baseline = map[string]LightState{}
		for _, l := range chosen {
			r.baseline[l.ID] = l.State
		}
	}
	r.mu.Unlock()
	r.a.logf("Lights: %s", s.label)
	brightest := r.maxBri(ctx)
	for _, st := range steps {
		if ctx.Err() != nil {
			return
		}
		began := time.Now()
		bri := int(math.Round(float64(st.Colour.Bri) * brightest))
		state := map[string]any{"on": true, "bri": max(1, bri), "transitiontime": min(2, int(st.Hold/(100*time.Millisecond)))}
		for _, l := range chosen {
			if l.Colour {
				state["xy"] = []float64{st.Colour.X, st.Colour.Y}
			} else {
				delete(state, "xy") // white bulbs follow the rhythm by brightness
			}
			b.SetState(ctx, l.ID, state)
		}
		// Many lights mean many changes a step; slow down just enough.
		hold := max(st.Hold, time.Duration(len(chosen))*r.gap)
		sleep(ctx, hold-time.Since(began))
	}
}

func (r *Runner) maxBri(ctx context.Context) float64 {
	pct := atoi(r.a.setting(ctx, "brightness"), 100)
	if pct <= 0 || pct > 100 {
		pct = 100
	}
	return float64(pct) / 100
}

// putBack restores the lights, waiting for the bridge to catch up, and
// tries again until they match.
func (r *Runner) putBack() {
	r.mu.Lock()
	base := r.baseline
	r.mu.Unlock()
	if base == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b := r.bridge(r.a.conf.get())
	sleep(ctx, 15*r.gap)
	for attempt := 1; attempt <= 4; attempt++ {
		for id, s := range base {
			b.Restore(ctx, id, s)
		}
		sleep(ctx, 12*r.gap)
		if matches(ctx, b, base) {
			r.mu.Lock()
			r.baseline = nil
			r.mu.Unlock()
			return
		}
	}
	r.a.logf("The lights didn't go back as they were; press Reset lights")
}

// Reset stops any effect and puts the lights back now.
func (r *Runner) Reset() {
	r.mu.Lock()
	r.queue = nil
	if r.stop != nil {
		r.stop()
	}
	running := r.running
	r.mu.Unlock()
	if !running {
		r.putBack()
	}
}

func matches(ctx context.Context, b bridgeAPI, base map[string]LightState) bool {
	lights, err := b.Lights(ctx)
	if err != nil {
		return false
	}
	now := map[string]LightState{}
	for _, l := range lights {
		now[l.ID] = l.State
	}
	for id, want := range base {
		got, ok := now[id]
		if !ok {
			continue // gone from the bridge
		}
		if got.On != want.On {
			return false
		}
		if !want.On {
			continue
		}
		if want.Bri > 0 && absInt(got.Bri-want.Bri) > 10 {
			return false
		}
		if want.ColorMode != "ct" && len(want.XY) == 2 && len(got.XY) == 2 &&
			(math.Abs(got.XY[0]-want.XY[0]) > 0.03 || math.Abs(got.XY[1]-want.XY[1]) > 0.03) {
			return false
		}
	}
	return true
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
