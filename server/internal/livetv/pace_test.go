package livetv

import (
	"testing"
	"time"
)

// tsWithPCR builds one 188-byte packet carrying a PCR (in seconds).
func tsWithPCR(pid int, secs float64) []byte {
	p := make([]byte, tsPacket)
	p[0] = 0x47
	p[1] = byte(pid >> 8 & 0x1f)
	p[2] = byte(pid)
	p[3] = 0x20 // adaptation field only
	p[4] = 183
	p[5] = 0x10 // PCR flag
	base := uint64(secs * pcrHz)
	p[6], p[7], p[8], p[9], p[10] = byte(base>>25), byte(base>>17), byte(base>>9), byte(base>>1), byte(base<<7)
	return p
}

func TestPace(t *testing.T) {
	clock := time.Unix(1000, 0)
	run := func(rate float64, burst float64, secs int) float64 {
		p := newPace()
		p.now = func() time.Time { return clock }
		media := 100.0
		p.feed(tsWithPCR(256, media))
		media += burst // the provider sends a burst ahead first
		p.feed(tsWithPCR(256, media))
		for i := 0; i < secs*10; i++ {
			clock = clock.Add(100 * time.Millisecond)
			media += 0.1 * rate
			pkt := tsWithPCR(256, media)
			p.feed(pkt[:50]) // split across chunks
			p.feed(pkt[50:])
			p.feed(tsWithPCR(300, 5)) // another PID's PCR is ignored
		}
		b, ok := p.behind()
		if !ok {
			t.Fatal("no PCR seen")
		}
		return b
	}
	if b := run(1, 8, 60); b > 0.5 {
		t.Errorf("a real-time stream after a burst is %.1fs behind, want ~0", b)
	}
	if b := run(0.8, 0, 60); b < 11 || b > 13 {
		t.Errorf("an 80%% speed stream after 60s is %.1fs behind, want ~12", b)
	}
	// Gone quiet: no PCRs for 15 s counts as 15 s behind.
	p := newPace()
	p.now = func() time.Time { return clock }
	p.feed(tsWithPCR(256, 1))
	clock = clock.Add(15 * time.Second)
	if b, _ := p.behind(); b < 14.9 {
		t.Errorf("quiet stream is %.1fs behind, want 15", b)
	}
	// A wrap of the 33-bit clock and a splice don't count as falling behind.
	p = newPace()
	p.now = func() time.Time { return clock }
	p.feed(tsWithPCR(256, pcrWrap-1))
	clock = clock.Add(2 * time.Second)
	p.feed(tsWithPCR(256, 1))
	clock = clock.Add(time.Second)
	p.feed(tsWithPCR(256, 5000))
	if b, _ := p.behind(); b > 1.1 {
		t.Errorf("wrap/splice counted as %.1fs behind", b)
	}
}
