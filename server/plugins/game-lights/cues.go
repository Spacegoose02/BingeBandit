package main

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// A cue is an effect for someone whose own TV drives their Hue hub
// (Person.Local): the server can't reach a hub in another home, so the TV
// asks for cues and plays them on its hub at the time given.
type cue struct {
	ID    int64     `json:"id"`
	At    time.Time `json:"-"`
	Label string    `json:"label"`
	Steps []cueStep `json:"steps"`
}

// cueStep is one light setting: CIE xy, brightness 1–254, held for ms.
type cueStep struct {
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	Bri int     `json:"bri"`
	Ms  int64   `json:"ms"`
}

// cueBox keeps each person's recent cues until their TV picks them up.
type cueBox struct {
	mu   sync.Mutex
	next int64
	by   map[string][]cue // by user id
}

const keepCues = 5 * time.Minute

// add queues an effect for a person, to play after delay.
func (b *cueBox) add(uid, label string, steps []Step, delay time.Duration) {
	c := cue{At: time.Now().Add(delay), Label: label, Steps: make([]cueStep, len(steps))}
	for i, s := range steps {
		c.Steps[i] = cueStep{s.Colour.X, s.Colour.Y, s.Colour.Bri, s.Hold.Milliseconds()}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.by == nil {
		b.by = map[string][]cue{}
	}
	b.next++
	c.ID = b.next
	list := b.by[uid]
	for len(list) > 0 && time.Since(list[0].At) > keepCues {
		list = list[1:]
	}
	b.by[uid] = append(list, c)
}

// after is the person's cues newer than id, with how long until each plays.
func (b *cueBox) after(uid string, id int64) []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []map[string]any{}
	for _, c := range b.by[uid] {
		if c.ID > id && time.Since(c.At) < 30*time.Second {
			out = append(out, map[string]any{"id": c.ID, "label": c.Label, "steps": c.Steps, "inMs": time.Until(c.At).Milliseconds()})
		}
	}
	return out
}

// latest is the newest cue id, so a TV that just started skips old ones.
func (b *cueBox) latest() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.next
}

// fireLocal queues an effect for a person whose TV drives their lights.
func (a *app) fireLocal(uid, key string, pal Palette, label string, delay time.Duration) {
	steps := effectSteps(key, pal, a.setting(context.Background(), "reduced_flashing") == "on")
	if len(steps) > 0 {
		a.cues.add(uid, label, steps, delay)
	}
}

// myCues is what the TV polls: GET /me/cues?after=<id>.
func (a *app) myCues(w http.ResponseWriter, r *http.Request, uid string) {
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"cues": []any{}, "last": a.cues.latest()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cues": a.cues.after(uid, after), "last": a.cues.latest()})
}
