package markers

import (
	"context"
	"log"
	"sort"

	"bingebandit/internal/store"
)

// Recaps ("Previously on…") are clips of earlier episodes near the start of
// an episode. Each episode's opening minutes are compared with the bodies
// of the one or two before it (between their intro and credits, so the
// shared theme doesn't count); a few clips found close together are the
// recap.
const (
	recapWindow   = 240.0 // recaps live in the first 4 minutes
	recapClipMin  = 2.5   // a clip at least this long counts
	recapTotalMin = 10.0  // the clips add up to at least this
	recapGapMax   = 25.0  // clips further apart than this belong to different things
	recapSnap     = 15.0  // a recap starting this close to the start runs from 0
	recapMaxClips = 12
)

var (
	recapPart = part{name: "recap", window: recapWindow, share: 0.25}
	bodyPart  = part{name: "body", window: 1 << 20, share: 1}
)

// FindClips lists the stretches of a (at least minLen seconds) that also
// appear somewhere in b, longest first, by finding the longest, setting it
// aside and looking again on each side.
func FindClips(a, b []uint32, minLen float64, maxClips int) []Match {
	var out []Match
	type span struct{ lo, hi int }
	todo := []span{{0, len(a)}}
	minFrames := int(minLen / FrameSeconds)
	for len(todo) > 0 && len(out) < maxClips {
		s := todo[0]
		todo = todo[1:]
		if s.hi-s.lo < minFrames {
			continue
		}
		m, ok := FindCommon(a[s.lo:s.hi], b, minLen, recapWindow)
		if !ok {
			continue
		}
		off := float64(s.lo) * FrameSeconds
		m.AStart += off
		m.AEnd += off
		out = append(out, m)
		todo = append(todo, span{s.lo, int(m.AStart / FrameSeconds)}, span{int(m.AEnd/FrameSeconds) + 1, s.hi})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AStart < out[j].AStart })
	return out
}

// RecapFrom turns clips into a recap [start, end], skipping anything inside
// the intro (introStart..introEnd, 0 for none).
func RecapFrom(clips []Match, introStart, introEnd float64) (float64, float64, bool) {
	var kept []Match
	for _, c := range clips {
		if introEnd > 0 && c.AStart < introEnd && c.AEnd > introStart {
			continue
		}
		kept = append(kept, c)
	}
	// The first close-together group of clips.
	var start, end, total float64
	n := 0
	for _, c := range kept {
		if n > 0 && c.AStart-end > recapGapMax {
			break
		}
		if n == 0 {
			start = c.AStart
		}
		end = max(end, c.AEnd)
		total += c.Duration()
		n++
	}
	if n == 0 || total < recapTotalMin || (n < 2 && total < 2*recapTotalMin) {
		return 0, 0, false
	}
	if start < recapSnap {
		start = 0
	}
	// A recap that stops just short of the intro runs up to it.
	if introStart > end && introStart-end < 8 {
		end = introStart
	}
	return start, end, true
}

// bodySpan is the part of an episode between its intro and its credits.
func (d *Detector) bodySpan(ep *episode) (float64, float64) {
	start, end := 0.0, ep.file.Duration
	if ms, err := d.st.Markers(ep.item.ID); err == nil {
		for _, m := range ms {
			if m.Kind == "intro" {
				start = m.End
			}
			if m.Kind == "credits" && m.Start > start {
				end = m.Start
			}
		}
	}
	return start, end
}

// recaps looks for the recap of every episode after the first.
func (d *Detector) recaps(ctx context.Context, eps []*episode) int {
	if d.st.Setting(store.SettingDetectRecaps) == "false" {
		return 0
	}
	found := 0
	bodies := map[int][]uint32{}
	body := func(i int) []uint32 {
		if fp, ok := bodies[i]; ok {
			return fp
		}
		full, err := d.fingerprint(ctx, eps[i].file, bodyPart)
		if err != nil {
			log.Printf("markers: %s: %v", eps[i].file.Path, err)
			bodies[i] = nil
			return nil
		}
		s, e := d.bodySpan(eps[i])
		lo, hi := int(s/FrameSeconds), min(int(e/FrameSeconds), len(full))
		if lo >= hi {
			lo, hi = 0, len(full)
		}
		bodies[i] = full[lo:hi]
		return bodies[i]
	}
	for i := 1; i < len(eps) && ctx.Err() == nil; i++ {
		a, err := d.fp(ctx, eps[i], recapPart)
		if err != nil {
			continue
		}
		var clips []Match
		for _, j := range []int{i - 1, i - 2} {
			if j < 0 {
				continue
			}
			if b := body(j); len(b) > 0 {
				clips = append(clips, FindClips(a, b, recapClipMin, recapMaxClips)...)
			}
		}
		sort.Slice(clips, func(x, y int) bool { return clips[x].AStart < clips[y].AStart })
		introStart, introEnd := 0.0, 0.0
		if ms, err := d.st.Markers(eps[i].item.ID); err == nil {
			for _, m := range ms {
				if m.Kind == "intro" {
					introStart, introEnd = m.Start, m.End
				}
			}
		}
		if s, e, ok := RecapFrom(clips, introStart, introEnd); ok {
			if err := d.st.SetDetectedMarker(eps[i].item.ID, "recap", s, e); err == nil {
				found++
			}
		}
	}
	return found
}
