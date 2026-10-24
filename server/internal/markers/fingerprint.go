// Package markers finds intros and end credits by comparing the audio of
// episodes in a season: the stretch of sound two episodes share near their
// start is the intro, and near their end, the credits.
package markers

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"math/cmplx"
	"os/exec"
	"strconv"

	"bingebandit/internal/media"
)

// Fingerprint parameters, in the spirit of Chromaprint/Haitsma-Kalker:
// a 32-bit hash per frame from how energy differences between adjacent
// frequency bands change from one frame to the next.
const (
	SampleRate = 5512
	frameSize  = 2048
	hop        = frameSize / 3
	bands      = 33
	minHz      = 300.0
	maxHz      = 3000.0
)

// FrameSeconds is the time step between fingerprint frames (~0.124s).
const FrameSeconds = float64(hop) / SampleRate

// Extract decodes [start, start+dur) of a file's audio as mono 16-bit PCM
// at SampleRate.
func Extract(ctx context.Context, ffmpeg, path string, start, dur float64) ([]int16, error) {
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-loglevel", "error",
		"-ss", strconv.FormatFloat(start, 'f', 3, 64), "-t", strconv.FormatFloat(dur, 'f', 3, 64),
		"-i", path, "-vn", "-sn", "-ac", "1", "-ar", strconv.Itoa(SampleRate), "-f", "s16le", "-")
	media.HideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("decode audio: %v %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	pcm := make([]int16, len(out)/2)
	binary.Read(bytes.NewReader(out[:len(pcm)*2]), binary.LittleEndian, pcm)
	return pcm, nil
}

var (
	window   = hann(frameSize)
	bandBins = bandEdges()
)

func hann(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	return w
}

// bandEdges splits minHz..maxHz into bands+1 log-spaced FFT bin edges.
func bandEdges() []int {
	edges := make([]int, bands+1)
	for i := range edges {
		f := minHz * math.Pow(maxHz/minHz, float64(i)/bands)
		edges[i] = int(f * frameSize / SampleRate)
	}
	return edges
}

// Fingerprint turns PCM into one 32-bit hash per frame.
func Fingerprint(pcm []int16) []uint32 {
	if len(pcm) < frameSize {
		return nil
	}
	n := (len(pcm)-frameSize)/hop + 1
	energies := make([][bands]float64, n)
	buf := make([]complex128, frameSize)
	for f := 0; f < n; f++ {
		off := f * hop
		for i := 0; i < frameSize; i++ {
			buf[i] = complex(float64(pcm[off+i])*window[i], 0)
		}
		fft(buf)
		for b := 0; b < bands; b++ {
			var e float64
			for k := bandBins[b]; k < max(bandBins[b+1], bandBins[b]+1); k++ {
				e += cmplx.Abs(buf[k])
			}
			energies[f][b] = e
		}
	}
	out := make([]uint32, n)
	for f := 1; f < n; f++ {
		var h uint32
		for b := 0; b < bands-1; b++ {
			d := (energies[f][b] - energies[f][b+1]) - (energies[f-1][b] - energies[f-1][b+1])
			if d > 0 {
				h |= 1 << b
			}
		}
		out[f] = h
	}
	return out[1:]
}

// fft is an in-place iterative radix-2 FFT; len(a) must be a power of two.
func fft(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := 0; k < size/2; k++ {
				u, v := a[start+k], a[start+k+size/2]*wk
				a[start+k], a[start+k+size/2] = u+v, u-v
				wk *= w
			}
		}
	}
}

// Match is a stretch of audio two fingerprints share, in seconds from the
// start of each fingerprint's window.
type Match struct {
	AStart, AEnd float64
	BStart, BEnd float64
}

func (m Match) Duration() float64 { return m.AEnd - m.AStart }

const (
	maxBitErrors = 10 // of 32; unrelated audio averages 16
	maxGapFrames = 8  // ~1s of mismatches allowed inside a run
	edgeFrames   = 8  // window used to trim sparse matches off a run's ends
)

// FindCommon returns the longest stretch, between minLen and maxLen
// seconds, that appears in both a and b (at any offset).
func FindCommon(a, b []uint32, minLen, maxLen float64) (Match, bool) {
	if len(a) == 0 || len(b) == 0 {
		return Match{}, false
	}
	// Score every alignment on a sample of frames, then examine the best few
	// in full. shift = index in b minus index in a.
	type cand struct{ shift, votes int }
	var top [4]cand
	for shift := -len(a) + 1; shift < len(b); shift++ {
		lo, hi := max(0, -shift), min(len(a), len(b)-shift)
		votes := 0
		for i := lo; i < hi; i += 3 {
			if bits.OnesCount32(a[i]^b[i+shift]) <= maxBitErrors {
				votes++
			}
		}
		for k := range top {
			if votes > top[k].votes {
				copy(top[k+1:], top[k:len(top)-1])
				top[k] = cand{shift, votes}
				break
			}
		}
	}

	best, found := Match{}, false
	for _, c := range top {
		if c.votes == 0 {
			continue
		}
		lo, hi := max(0, -c.shift), min(len(a), len(b)-c.shift)
		hit := func(i int) bool { return i >= lo && i < hi && bits.OnesCount32(a[i]^b[i+c.shift]) <= maxBitErrors }
		// dense reports whether most of the edgeFrames frames from i (in
		// direction dir) match; random look-alikes next to the real section
		// are sparse, so this trims them off the ends of a run.
		dense := func(i, dir int) bool {
			n := 0
			for k := 0; k < edgeFrames; k++ {
				if hit(i + k*dir) {
					n++
				}
			}
			return n*2 > edgeFrames
		}
		runStart, lastHit := -1, -1
		consider := func(start, end int) {
			for start < end && !dense(start, 1) {
				start++
			}
			for end > start && !dense(end, -1) {
				end--
			}
			m := Match{
				AStart: float64(start) * FrameSeconds, AEnd: float64(end+1) * FrameSeconds,
				BStart: float64(start+c.shift) * FrameSeconds, BEnd: float64(end+1+c.shift) * FrameSeconds,
			}
			if d := m.Duration(); d >= minLen && d <= maxLen && (!found || d > best.Duration()) {
				best, found = m, true
			}
		}
		for i := lo; i < hi; i++ {
			if !hit(i) {
				continue
			}
			if runStart < 0 || i-lastHit > maxGapFrames {
				if runStart >= 0 {
					consider(runStart, lastHit)
				}
				runStart = i
			}
			lastHit = i
		}
		if runStart >= 0 {
			consider(runStart, lastHit)
		}
	}
	return best, found
}
