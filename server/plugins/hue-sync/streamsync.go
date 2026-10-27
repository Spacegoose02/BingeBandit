package main

import (
	"context"
	"log"
	"strconv"
	"time"
)

// Warm whites as xy, for theater mode (about 2200 K, the ct 447 the light
// commands use) and for the lights on pause (about 2700 K, ct 366).
var (
	theaterXY = Colour{X: 0.5268, Y: 0.4133}
	pausedXY  = Colour{X: 0.4573, Y: 0.4100}
)

// stream drives the session's lights over an entertainment stream when the
// device picked an entertainment area. It returns false, with nothing
// sent, when the session should use light commands instead: not an
// entertainment area, a bridge paired before streaming, another player
// already streaming, or a stream that won't open (the reason goes in the
// status). Otherwise it streams until the session ends and returns true.
func (e *Engine) stream(ctx context.Context, s *session, b *Bridge) bool {
	conf := e.a.conf.get()
	if s.ev.Area == "" {
		return false
	}
	area, err := b.entertainmentArea(ctx, s.ev.Area)
	if err != nil {
		return false // a room or zone, or a bridge without the v2 API
	}
	if conf.ClientKey == "" {
		e.setProblem(s, "pair the bridge again on the Hue Sync page to stream to entertainment areas")
		return false
	}
	e.mu.Lock()
	for _, o := range e.sessions {
		if o != s && o.stream != nil {
			e.mu.Unlock()
			e.setProblem(s, "another player is streaming to "+o.stream.area.Name+"; using light commands")
			return false
		}
	}
	e.mu.Unlock()
	st, err := openStream(ctx, b, conf.ClientKey, *area)
	if err != nil {
		if ctx.Err() == nil {
			e.setProblem(s, "entertainment streaming: "+err.Error()+"; using light commands")
		}
		return false
	}
	e.mu.Lock()
	if e.sessions[s.ev.SessionID] != s {
		e.mu.Unlock()
		st.Close(ctx)
		close(s.streamDone)
		return true
	}
	s.stream, s.fps, s.problem = st, streamFPS, ""
	if s.sampler != nil {
		// Read the picture more often now the lights can keep up.
		s.sampler.Stop()
		s.sampler = e.newSampler(s, s.position(time.Now()))
	}
	e.mu.Unlock()
	log.Printf("streaming to entertainment area %s (%d channels)", area.Name, len(area.Channels))

	defer func() {
		st.Close(ctx)
		e.mu.Lock()
		s.stream = nil
		e.mu.Unlock()
		close(s.streamDone)
	}()
	t := time.NewTicker(time.Second / streamHz)
	defer t.Stop()
	cur := make([]Colour, len(area.Channels))
	have := make([]bool, len(area.Channels))
	fails := 0
	for {
		select {
		case <-ctx.Done():
			return true
		case <-t.C:
		}
		target, ok := e.streamTargets(ctx, s, area.Channels)
		for i := range cur {
			if !ok[i] {
				continue
			}
			if !have[i] {
				cur[i], have[i] = target[i], true
				continue
			}
			// Ease towards the picture so the lights glide rather than step
			// between the frames the sampler reads.
			const k = 0.35
			cur[i].X += (target[i].X - cur[i].X) * k
			cur[i].Y += (target[i].Y - cur[i].Y) * k
			cur[i].Bri += (target[i].Bri - cur[i].Bri) * k
		}
		// Keep sending, even unchanged: the bridge ends a stream that goes
		// quiet for ten seconds.
		if err := st.Send(cur); err != nil {
			if fails++; fails == streamHz { // a second of failures
				e.setProblem(s, "entertainment stream: "+err.Error())
			}
			continue
		}
		if fails >= streamHz {
			e.setProblem(s, "")
		}
		fails = 0
	}
}

// streamTargets is the colour each channel should have now: the picture
// for its part of the screen, a warm dim light in theater mode, warm white
// on pause when the admin chose that. ok is false for channels to hold.
func (e *Engine) streamTargets(ctx context.Context, s *session, chans []EntChannel) ([]Colour, []bool) {
	out, ok := make([]Colour, len(chans)), make([]bool, len(chans))
	maxPct, _ := strconv.ParseFloat(e.a.setting(ctx, "brightness"), 64)
	if maxPct <= 0 || maxPct > 100 {
		maxPct = 100
	}
	delay, _ := strconv.ParseFloat(e.a.setting(ctx, "delay_ms"), 64)
	onPause := e.a.setting(ctx, "on_pause")
	theater, err := strconv.ParseFloat(e.a.setting(ctx, "theater_brightness"), 64)
	if err != nil || theater < 0 || theater > 100 {
		theater = 8
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	fill := func(c Colour) {
		for i := range out {
			out[i], ok[i] = c, true
		}
	}
	switch {
	case s.paused && onPause == "bright":
		c := pausedXY
		c.Bri = float64(hueBri(0.6, maxPct)) / 254
		fill(c)
	case s.paused:
		// keep the colours
	case s.mode == ModeTheater:
		c := theaterXY
		c.Bri = theater / 100
		fill(c)
	case s.sampler != nil:
		zones, have := s.sampler.At(s.position(time.Now()) - delay/1000)
		if !have {
			if err := s.sampler.Err(); err != nil {
				s.problem = "can't read the video: " + err.Error()
			}
			break
		}
		for i, ch := range chans {
			if c, found := zones[ch.zone()]; found {
				c.Bri = float64(hueBri(c.Bri, maxPct)) / 254
				out[i], ok[i] = c, true
			}
		}
	}
	return out, ok
}
