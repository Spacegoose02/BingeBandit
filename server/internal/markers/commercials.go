package markers

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

// Ad breaks in DVR recordings, for Skip Ad. Comskip is used when it's
// installed (BB_COMSKIP or on the PATH); otherwise ffmpeg looks for the
// moments broadcasters cut to and from ads: a few black frames with
// silence. A run of stretches between such cuts that last as long as ads
// do (multiples of five seconds, ten seconds to two minutes) is a break.

const (
	adMin      = 9.0   // shortest ad
	adMax      = 125.0 // longest ad
	adStep     = 5.0   // ads run in whole multiples of this
	adSlack    = 1.6   // how far a stretch may miss a multiple (IPTV timing is loose)
	breakMin   = 40.0  // shortest break
	breakMax   = 480.0 // longest break
	cutJoin    = 1.0   // cuts this close together are one cut
	cutOverlap = 0.35  // black and silence this close together make a cut
)

// Commercials finds the ad breaks in a recording, as [start, end] seconds.
func (d *Detector) Commercials(ctx context.Context, path string, duration float64) ([][2]float64, error) {
	if bin := comskipBinary(); bin != "" {
		breaks, err := d.comskip(ctx, bin, path)
		if err == nil {
			return breaks, nil
		}
		// Fall through to our own detection.
	}
	blacks, silences, err := blackAndSilence(ctx, d.ffmpeg, path)
	if err != nil {
		return nil, err
	}
	return FindAdBreaks(blacks, silences, duration), nil
}

func comskipBinary() string {
	if p := os.Getenv("BB_COMSKIP"); p != "" {
		return p
	}
	if p, err := exec.LookPath("comskip"); err == nil {
		return p
	}
	return ""
}

// comskip runs Comskip and reads the .edl file it writes.
func (d *Detector) comskip(ctx context.Context, bin, path string) ([][2]float64, error) {
	dir, err := os.MkdirTemp(d.cacheDir, "comskip-")
	if err != nil {
		if dir, err = os.MkdirTemp("", "comskip-"); err != nil {
			return nil, err
		}
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, bin, "--output="+dir, path)
	media.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	edl := filepath.Join(dir, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+".edl")
	b, rerr := os.ReadFile(edl)
	if rerr != nil {
		if err != nil {
			return nil, fmt.Errorf("comskip: %v %s", err, lastLine(string(out)))
		}
		return [][2]float64{}, nil // Comskip found nothing (it writes no file then)
	}
	return ParseEDL(string(b)), nil
}

// ParseEDL reads an edit decision list: "start end type" per line.
func ParseEDL(s string) [][2]float64 {
	out := [][2]float64{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		a, err1 := strconv.ParseFloat(f[0], 64)
		b, err2 := strconv.ParseFloat(f[1], 64)
		if err1 == nil && err2 == nil && b > a {
			out = append(out, [2]float64{a, b})
		}
	}
	return out
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

var (
	blackRe   = regexp.MustCompile(`black_start:\s*([\d.]+)\s+black_end:\s*([\d.]+)`)
	silStart  = regexp.MustCompile(`silence_start:\s*(-?[\d.]+)`)
	silEnd    = regexp.MustCompile(`silence_end:\s*([\d.]+)`)
	numberish = regexp.MustCompile(`^-?[\d.]+$`)
)

// blackAndSilence decodes a recording (small and at a few frames a second,
// to keep it cheap) and lists its black and silent stretches.
func blackAndSilence(ctx context.Context, ffmpeg, path string) (blacks, silences [][2]float64, err error) {
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-nostats", "-i", path,
		"-vf", "fps=6,scale=128:-2,blackdetect=d=0.08:pic_th=0.94:pix_th=0.12",
		"-af", "silencedetect=noise=-45dB:d=0.12",
		"-sn", "-f", "null", "-")
	media.HideWindow(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	open := -1.0
	var tail []string
	for sc.Scan() {
		line := sc.Text()
		if m := blackRe.FindStringSubmatch(line); m != nil {
			a, _ := strconv.ParseFloat(m[1], 64)
			b, _ := strconv.ParseFloat(m[2], 64)
			blacks = append(blacks, [2]float64{a, b})
		}
		if m := silStart.FindStringSubmatch(line); m != nil && numberish.MatchString(m[1]) {
			open, _ = strconv.ParseFloat(m[1], 64)
			open = math.Max(open, 0)
		}
		if m := silEnd.FindStringSubmatch(line); m != nil && open >= 0 {
			b, _ := strconv.ParseFloat(m[1], 64)
			silences = append(silences, [2]float64{open, b})
			open = -1
		}
		if len(tail) > 4 {
			tail = tail[1:]
		}
		tail = append(tail, line)
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fmt.Errorf("ffmpeg: %v %s", err, strings.Join(tail, " "))
	}
	return blacks, silences, nil
}

// FindAdBreaks turns black and silent stretches into ad breaks.
func FindAdBreaks(blacks, silences [][2]float64, duration float64) [][2]float64 {
	// A cut is black that comes with silence.
	var cuts []float64
	for _, b := range blacks {
		for _, s := range silences {
			if s[0] <= b[1]+cutOverlap && s[1] >= b[0]-cutOverlap {
				cuts = append(cuts, (b[0]+b[1])/2)
				break
			}
		}
	}
	sort.Float64s(cuts)
	var joined []float64
	for _, c := range cuts {
		if len(joined) > 0 && c-joined[len(joined)-1] < cutJoin {
			continue
		}
		joined = append(joined, c)
	}
	adLike := func(l float64) bool {
		if l < adMin || l > adMax {
			return false
		}
		return math.Abs(l-math.Round(l/adStep)*adStep) <= adSlack
	}
	out := [][2]float64{}
	flush := func(start, end float64, n int) {
		if n >= 2 && end-start >= breakMin && end-start <= breakMax {
			out = append(out, [2]float64{start, end})
		}
	}
	start, n := -1.0, 0
	for i := 1; i < len(joined); i++ {
		a, b := joined[i-1], joined[i]
		if adLike(b - a) {
			if n == 0 {
				start = a
			}
			n++
			continue
		}
		if n > 0 {
			flush(start, a, n)
		}
		n = 0
	}
	if n > 0 {
		flush(start, joined[len(joined)-1], n)
	}
	// A break that runs to the end of the file is the next show's ads (or
	// padding); skipping into nothing helps nobody.
	if len(out) > 0 && duration > 0 && duration-out[len(out)-1][1] < 5 {
		out = out[:len(out)-1]
	}
	return out
}

// PendingCommercials looks for the ad breaks in DVR recordings that came
// in since the last run, a few at a time.
func (d *Detector) PendingCommercials(ctx context.Context) (string, error) {
	if d.st.Setting(store.SettingDVRDetectAds) == "false" {
		return "turned off in Live TV → DVR settings", nil
	}
	recs, err := d.st.RecordingsNeedingCommercials(20)
	if err != nil {
		return "", err
	}
	found, done := 0, 0
	for _, r := range recs {
		if ctx.Err() != nil {
			break
		}
		d.mu.Lock()
		breaks, err := d.Commercials(ctx, r.Path, r.Duration)
		d.mu.Unlock()
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			// Marked checked anyway, so a file ffmpeg can't read isn't retried every hour.
			log.Printf("ad breaks: %s: %v", r.Path, err)
		} else if err := d.st.SetDetectedCommercials(r.ItemID, breaks); err != nil {
			return "", err
		}
		found += len(breaks)
		done++
		d.st.MarkCommercialsChecked(r.RecordingID)
	}
	return fmt.Sprintf("looked through %d recordings and found %d ad breaks", done, found), nil
}
