package livetv

import (
	"bytes"
	"time"
)

// gopCache keeps the relayed MPEG-TS from the latest video keyframe on, so
// a raw-TS viewer joining a running channel (Plex's tuner, an IPTV app)
// starts with a picture it can decode. Starting mid-way between keyframes,
// the decoder has no parameter sets ("non-existing PPS 0 referenced") and
// Plex gives up on the channel.
//
// Keyframes are found by the random access indicator that muxers set on the
// video stream's packets, with the video stream's PID taken from the PAT
// and PMT.
type gopCache struct {
	buf   bytes.Buffer // from the latest keyframe on; empty until one is seen
	valid bool
	seen  bool   // a keyframe has been found in this upstream
	rest  []byte // a partial packet carried to the next chunk
	pmt   int    // PMT PID, -1 until the PAT is seen
	video int    // video PID, -1 until the PMT is seen
}

// maxGOP caps the cache; a stream with keyframes further apart than this
// (or none marked) is relayed as it comes.
const maxGOP = 16 << 20

func newGOPCache() *gopCache { return &gopCache{pmt: -1, video: -1} }

// reset forgets everything, for a new upstream with its own PIDs.
func (g *gopCache) reset() {
	g.buf.Reset()
	g.valid, g.seen, g.rest, g.pmt, g.video = false, false, nil, -1, -1
}

// feed adds relayed data, restarting the cache at the last keyframe in it.
func (g *gopCache) feed(chunk []byte) {
	buf := chunk
	if len(g.rest) > 0 {
		buf = append(g.rest, chunk...)
		g.rest = nil
	}
	key := -1
	i := 0
	for i+tsPacket <= len(buf) {
		if buf[i] != 0x47 {
			i++ // resync
			continue
		}
		if g.packet(buf[i : i+tsPacket]) {
			key = i
		}
		i += tsPacket
	}
	if i < len(buf) {
		g.rest = append([]byte(nil), buf[i:]...)
	}
	switch {
	case key >= 0:
		g.buf.Reset()
		g.buf.Write(buf[key:])
		g.valid, g.seen = true, true
	case g.valid:
		g.buf.Write(chunk)
		if g.buf.Len() > maxGOP {
			g.buf.Reset()
			g.valid = false
		}
	}
}

// How long a joining viewer waits for a keyframe before it gets the stream
// as it comes: briefly for a stream not yet seen to mark any, longer for
// one that does.
const (
	keyWait     = 2 * time.Second
	keyWaitSeen = 10 * time.Second
)

// worthWaiting reports whether a viewer that has waited through bytes of
// the stream, for wait, should keep waiting for a keyframe.
func (g *gopCache) worthWaiting(bytes int64, wait time.Duration) bool {
	if g.seen {
		return bytes < maxGOP && wait < keyWaitSeen
	}
	return wait < keyWait
}

// start is a copy of the cache, or nil when no keyframe has been seen.
func (g *gopCache) start() []byte {
	if !g.valid {
		return nil
	}
	return append([]byte(nil), g.buf.Bytes()...)
}

// packet reads PAT and PMT and reports whether pkt starts a video keyframe.
func (g *gopCache) packet(pkt []byte) bool {
	pid := int(pkt[1]&0x1f)<<8 | int(pkt[2])
	pusi := pkt[1]&0x40 != 0
	afc := (pkt[3] >> 4) & 3
	payload := 4
	rai := false
	if afc&2 != 0 {
		alen := int(pkt[4])
		if alen > 0 {
			rai = pkt[5]&0x40 != 0
		}
		payload = 5 + alen
	}
	if pid == g.video {
		return rai
	}
	if afc&1 == 0 || payload >= tsPacket {
		return false
	}
	if !pusi || (pid != 0 && pid != g.pmt) {
		return false
	}
	p := pkt[payload:]
	ptr := int(p[0])
	if 1+ptr+3 > len(p) {
		return false
	}
	s := p[1+ptr:]
	end := min(len(s), 3+(int(s[1]&0x0f)<<8|int(s[2]))-4) // without the CRC
	switch {
	case pid == 0 && s[0] == 0x00:
		for j := 8; j+4 <= end; j += 4 {
			if prog := int(s[j])<<8 | int(s[j+1]); prog != 0 {
				g.pmt = int(s[j+2]&0x1f)<<8 | int(s[j+3])
				break
			}
		}
	case pid == g.pmt && s[0] == 0x02 && end > 12:
		j := 12 + (int(s[10]&0x0f)<<8 | int(s[11]))
		for j+5 <= end {
			switch s[j] {
			case 0x01, 0x02, 0x10, 0x1b, 0x24, 0xea: // MPEG-1/2, MPEG-4, H.264, HEVC, VC-1
				g.video = int(s[j+1]&0x1f)<<8 | int(s[j+2])
				return false
			}
			j += 5 + (int(s[j+3]&0x0f)<<8 | int(s[j+4]))
		}
	}
	return false
}
