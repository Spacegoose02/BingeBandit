package livetv

import (
	"sync"
	"time"
)

// pace measures whether an upstream delivers video as fast as it plays, by
// comparing the stream's own clock (MPEG-TS PCR) with the wall clock. A
// stream that keeps sending bytes but falls behind real time never trips the
// no-data timeout, yet every player watching it drains its buffer and stalls.
//
// "Behind" is how far the stream has fallen from the furthest ahead it has
// been: a provider that bursts 10 s ahead and then plays in real time has
// none; one that sends 90% of real time gains a second every ten.
type pace struct {
	mu       sync.Mutex
	rest     []byte // a partial packet carried to the next chunk
	pid      int    // the PID carrying the PCR, -1 until seen
	started  bool
	wall0    time.Time
	lastPCR  float64 // seconds
	elapsed  float64 // stream time since the first PCR
	maxAhead float64
	now      func() time.Time
}

func newPace() *pace { return &pace{pid: -1, now: time.Now} }

const (
	tsPacket = 188
	pcrHz    = 90000.0
	pcrWrap  = float64(1<<33) / pcrHz
)

// feed scans a chunk of MPEG-TS for PCRs.
func (p *pace) feed(chunk []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	buf := chunk
	if len(p.rest) > 0 {
		buf = append(p.rest, chunk...)
		p.rest = nil
	}
	i := 0
	for i+tsPacket <= len(buf) {
		if buf[i] != 0x47 {
			i++ // resync
			continue
		}
		p.packet(buf[i : i+tsPacket])
		i += tsPacket
	}
	if i < len(buf) {
		p.rest = append([]byte(nil), buf[i:]...)
	}
}

func (p *pace) packet(pkt []byte) {
	pid := int(pkt[1]&0x1f)<<8 | int(pkt[2])
	afc := (pkt[3] >> 4) & 3
	if afc != 2 && afc != 3 || pkt[4] < 7 || pkt[5]&0x10 == 0 {
		return // no adaptation field, or no PCR in it
	}
	if p.pid == -1 {
		p.pid = pid
	} else if pid != p.pid {
		return
	}
	base := uint64(pkt[6])<<25 | uint64(pkt[7])<<17 | uint64(pkt[8])<<9 | uint64(pkt[9])<<1 | uint64(pkt[10])>>7
	p.pcr(float64(base) / pcrHz)
}

func (p *pace) pcr(media float64) {
	now := p.now()
	if !p.started {
		p.started, p.wall0, p.lastPCR = true, now, media
		return
	}
	step := media - p.lastPCR
	if step < -pcrWrap/2 {
		step += pcrWrap // the 33-bit clock wrapped (every ~26.5 h)
	}
	if step < 0 || step > 10 {
		step = 0 // a discontinuity (ad splice, encoder restart): carry on from here
	}
	p.lastPCR = media
	p.elapsed += step
	if ahead := p.elapsed - now.Sub(p.wall0).Seconds(); ahead > p.maxAhead {
		p.maxAhead = ahead
	}
}

// behind reports how many seconds the stream has fallen behind real time
// (counting any quiet time since the last PCR), and whether a PCR has been
// seen at all: streams without one can't be measured.
func (p *pace) behind() (float64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		return 0, false
	}
	ahead := p.elapsed - p.now().Sub(p.wall0).Seconds()
	return p.maxAhead - ahead, true
}
