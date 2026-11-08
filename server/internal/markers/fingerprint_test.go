package markers

import (
	"math"
	"math/rand"
	"testing"
)

// tune is a deterministic "theme song": a few gliding tones.
func tune(seconds float64) []float64 {
	n := int(seconds * SampleRate)
	out := make([]float64, n)
	for i := range out {
		t := float64(i) / SampleRate
		out[i] = 0.3*math.Sin(2*math.Pi*(440+180*math.Sin(2*math.Pi*0.4*t))*t) +
			0.2*math.Sin(2*math.Pi*(880+90*math.Sin(2*math.Pi*1.3*t))*t) +
			0.15*math.Sin(2*math.Pi*1320*t*(1+0.05*math.Sin(2*math.Pi*0.2*t)))
	}
	return out
}

func noise(seconds float64, seed int64) []float64 {
	r := rand.New(rand.NewSource(seed))
	out := make([]float64, int(seconds*SampleRate))
	for i := range out {
		out[i] = r.Float64()*0.6 - 0.3
	}
	return out
}

func pcm(parts ...[]float64) []int16 {
	var out []int16
	r := rand.New(rand.NewSource(99))
	for _, p := range parts {
		for _, v := range p {
			// A little hiss so the copies aren't bit-identical, like two encodes.
			out = append(out, int16((v+r.Float64()*0.02)*20000))
		}
	}
	return out
}

func TestFindCommonIntro(t *testing.T) {
	theme := tune(30)
	a := Fingerprint(pcm(noise(8, 1), theme, noise(40, 2)))
	b := Fingerprint(pcm(noise(21, 3), theme, noise(30, 4)))

	m, ok := FindCommon(a, b, 15, 120)
	if !ok {
		t.Fatal("no common section found")
	}
	near := func(got, want float64) bool { return math.Abs(got-want) < 0.6 }
	if !near(m.AStart, 8) || !near(m.AEnd, 38) || !near(m.BStart, 21) || !near(m.BEnd, 51) {
		t.Errorf("match = %+v, want A 8-38s, B 21-51s", m)
	}
}

func TestFindCommonRejectsUnrelatedAudio(t *testing.T) {
	a := Fingerprint(pcm(noise(60, 5)))
	b := Fingerprint(pcm(noise(60, 6)))
	if m, ok := FindCommon(a, b, 15, 120); ok {
		t.Errorf("unrelated audio matched: %+v", m)
	}
}

// A recap: three clips of the last episode's body, with new scenes around.
func TestRecapClips(t *testing.T) {
	clipA, clipB, clipC := tune(6), noise(5, 31), noise(7, 32)
	// The last episode: clips A, B and C at 100, 300 and 500 s, among other noise.
	prev := Fingerprint(pcm(noise(100, 40), clipA, noise(194, 41), clipB, noise(195, 42), clipC, noise(60, 43)))
	// This one: "Previously on" (new), the three clips (ending near 23.5 s), a
	// cold open, then the intro at 40 s and the episode.
	cur := Fingerprint(pcm(noise(3, 50), clipC, noise(1, 51), clipA, noise(1, 52), clipB, noise(17, 53), noise(80, 54)))
	clips := FindClips(cur, prev, recapClipMin, recapMaxClips)
	if len(clips) < 3 {
		t.Fatalf("clips: %+v", clips)
	}
	s, e, ok := RecapFrom(clips, 40, 70)
	if !ok || s != 0 || math.Abs(e-23.5) > 1 {
		t.Fatalf("recap %v %v %v from %+v", s, e, ok, clips)
	}
	// Unrelated openings have no recap.
	if _, _, ok := RecapFrom(FindClips(Fingerprint(pcm(noise(60, 60))), prev, recapClipMin, recapMaxClips), 0, 0); ok {
		t.Fatal("recap found in unrelated audio")
	}
}
