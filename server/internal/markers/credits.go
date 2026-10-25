package markers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

// Movies have no sibling episodes to compare, so their end credits are found
// from the picture instead: a long stretch of dark, colourless frames near
// the end (white or coloured text on black). Stylised credits over footage
// aren't caught; an admin can still set those by hand.
const (
	movieMinDuration   = 1200 // shorter files aren't treated as movies
	movieCreditsWindow = 900  // look at the last 15 minutes…
	movieCreditsShare  = 0.2  // …or the last fifth of a shorter film
	movieCreditsMin    = 60
	// creditsBreak: a stretch of normal picture this long ends a credits run.
	// Shorter ones (a logo, a flash of colour) are tolerated.
	creditsBreak = 8.0
	// movieEndSnap: credits that end this close to the end run to it.
	movieEndSnap = 45
	darkLuma     = 45 // YAVG on the 16–235 scale
	greySat      = 12 // SATAVG; real footage sits well above this
)

// frameSample is one analysed frame: its time and whether it looks like credits.
type frameSample struct {
	t       float64
	credits bool
}

// Movie looks for the end credits of a movie and stores them as a marker.
// It reports whether it found any.
func (d *Detector) Movie(ctx context.Context, itemID int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	files, err := d.st.FilesForItem(itemID)
	if err != nil {
		return false, err
	}
	found := false
	if len(files) > 0 && files[0].Duration >= movieMinDuration {
		f := files[0]
		length := min(movieCreditsWindow, f.Duration*movieCreditsShare)
		start := f.Duration - length
		samples, err := d.sampleFrames(ctx, f.Path, start, length)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			// A file ffmpeg can't read is marked scanned, so it isn't retried every night.
			log.Printf("markers: %s: %v", f.Path, err)
		}
		if s, e, ok := FindCreditsRun(samples, length); ok {
			s, e = start+s, start+e
			if f.Duration-e < movieEndSnap {
				e = f.Duration
			}
			if err := d.st.SetDetectedMarker(itemID, "credits", s, e); err != nil {
				return false, err
			}
			found = true
		}
	}
	return found, d.st.MarkMarkersScanned([]int64{itemID})
}

// sampleFrames measures the brightness and colour of frames from start for
// length seconds. Only keyframes are decoded, which is fast; if the file has
// too few of them, it falls back to decoding everything and taking one frame
// every two seconds.
func (d *Detector) sampleFrames(ctx context.Context, path string, start, length float64) ([]frameSample, error) {
	ss := strconv.FormatFloat(start, 'f', 3, 64)
	t := strconv.FormatFloat(length, 'f', 3, 64)
	const filter = "scale=160:-2,signalstats,metadata=print:file=-"
	samples, err := d.runSignalstats(ctx, "-skip_frame", "nokey", "-ss", ss, "-t", t, "-i", path, "-an", "-sn", "-dn", "-vf", filter)
	if err != nil || len(samples) >= int(length/30) {
		return samples, err
	}
	return d.runSignalstats(ctx, "-ss", ss, "-t", t, "-i", path, "-an", "-sn", "-dn", "-vf", "fps=1/2,"+filter)
}

func (d *Detector) runSignalstats(ctx context.Context, args ...string) ([]frameSample, error) {
	args = append([]string{"-nostdin", "-loglevel", "error"}, args...)
	args = append(args, "-f", "null", "-")
	cmd := exec.CommandContext(ctx, d.ffmpeg, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	samples := ParseSignalstats(out)
	if err := cmd.Wait(); err != nil {
		return samples, fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return samples, nil
}

// ParseSignalstats reads the per-frame output of ffmpeg's
// signalstats + metadata=print filters.
func ParseSignalstats(r io.Reader) []frameSample {
	var out []frameSample
	var cur *frameSample
	var luma, sat float64
	flush := func() {
		if cur != nil {
			cur.credits = luma < darkLuma && sat < greySat
			out = append(out, *cur)
		}
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "frame:") {
			flush()
			cur, luma, sat = nil, 255, 255
			for _, f := range strings.Fields(line) {
				if v, ok := strings.CutPrefix(f, "pts_time:"); ok {
					if t, err := strconv.ParseFloat(v, 64); err == nil {
						cur = &frameSample{t: t}
					}
				}
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			continue
		}
		switch k {
		case "lavfi.signalstats.YAVG":
			luma = n
		case "lavfi.signalstats.SATAVG":
			sat = n
		}
	}
	flush()
	return out
}

// FindCreditsRun picks the longest run of credits-like frames, in seconds from
// the start of the sampled window (length seconds long). Normal frames that
// span less than creditsBreak are bridged; a run must last at least
// movieCreditsMin seconds. It ends at the first normal frame after it, or at
// the end of the window.
func FindCreditsRun(samples []frameSample, length float64) (start, end float64, ok bool) {
	runStart, runEnd := -1.0, 0.0
	breakAt, lastNormal := -1.0, 0.0 // the normal frames since the last credits frame
	consider := func() {
		if runStart >= 0 && runEnd-runStart >= movieCreditsMin && runEnd-runStart > end-start {
			start, end, ok = runStart, runEnd, true
		}
	}
	for i, s := range samples {
		if !s.credits {
			if breakAt < 0 {
				breakAt = s.t
			}
			lastNormal = s.t
			if runStart >= 0 && lastNormal-breakAt >= creditsBreak {
				consider()
				runStart = -1
			}
			continue
		}
		if runStart < 0 {
			runStart = s.t
		}
		runEnd = length
		if i+1 < len(samples) {
			runEnd = min(samples[i+1].t, length)
		}
		breakAt = -1
	}
	consider()
	return start, end, ok
}
