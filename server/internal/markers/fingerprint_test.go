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
