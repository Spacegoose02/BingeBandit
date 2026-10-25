package markers

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"bingebandit/internal/store"
)

// Where to look and how long a section may be, in seconds.
const (
	introWindow   = 600 // intros live in the first 10 minutes…
	introShare    = 0.5 // …or the first half of a short episode
	introMin      = 15
	introMax      = 130
	creditsWindow = 420 // credits live in the last 7 minutes…
	creditsShare  = 0.3
	creditsMin    = 15
	creditsMax    = 420
	// endSnap: credits that end this close to the end of the file run to it.
	endSnap = 25
)

type Detector struct {
	st       *store.Store
	ffmpeg   string
	cacheDir string
	mu       sync.Mutex // one season at a time; decoding audio is CPU-heavy
}

func NewDetector(st *store.Store, ffmpeg, cacheDir string) *Detector {
	return &Detector{st: st, ffmpeg: ffmpeg, cacheDir: cacheDir}
}

type part struct {
	name           string
	window, share  float64
	fromEnd        bool
	minLen, maxLen float64
}

var (
	introPart   = part{"intro", introWindow, introShare, false, introMin, introMax}
	creditsPart = part{"credits", creditsWindow, creditsShare, true, creditsMin, creditsMax}
)

// span is the slice of the file that gets fingerprinted for a part.
func (p part) span(duration float64) (start, length float64) {
	length = min(p.window, duration*p.share)
	if p.fromEnd {
		return duration - length, length
	}
	return 0, length
}

// fingerprint decodes and hashes one part of a file, caching the result on
// disk keyed by file id, size and window, so new episodes compare cheaply.
func (d *Detector) fingerprint(ctx context.Context, f *store.MediaFile, p part) ([]uint32, error) {
	start, length := p.span(f.Duration)
	cache := filepath.Join(d.cacheDir, fmt.Sprintf("%d-%d-%s-%d.fp", f.ID, f.Size, p.name, int(length)))
	if b, err := os.ReadFile(cache); err == nil && len(b)%4 == 0 {
		fp := make([]uint32, len(b)/4)
		for i := range fp {
			fp[i] = binary.LittleEndian.Uint32(b[i*4:])
		}
		return fp, nil
	}
	pcm, err := Extract(ctx, d.ffmpeg, f.Path, start, length)
	if err != nil {
		return nil, err
	}
	fp := Fingerprint(pcm)
	b := make([]byte, len(fp)*4)
	for i, v := range fp {
		binary.LittleEndian.PutUint32(b[i*4:], v)
	}
	if err := os.MkdirAll(d.cacheDir, 0o755); err == nil {
		os.WriteFile(cache, b, 0o644)
	}
	return fp, nil
}

type episode struct {
	item *store.Item
	file *store.MediaFile
	fp   map[string][]uint32
}

// Season detects intros and credits for every episode of a season by
// comparing each with its neighbours. It returns how many markers it set.
func (d *Detector) Season(ctx context.Context, seasonID int64) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	items, err := d.st.Children(0, seasonID)
	if err != nil {
		return 0, err
	}
	var eps []*episode
	var ids []int64
	for _, it := range items {
		ids = append(ids, it.ID)
		files, err := d.st.FilesForItem(it.ID)
		if err != nil || len(files) == 0 || files[0].Duration < 2*introMin {
			continue
		}
		eps = append(eps, &episode{item: it, file: files[0], fp: map[string][]uint32{}})
	}
	found := 0
	if len(eps) >= 2 {
		for _, p := range []part{introPart, creditsPart} {
			for i, ep := range eps {
				if ctx.Err() != nil {
					return found, ctx.Err()
				}
				if m, ok := d.bestMatch(ctx, eps, i, p); ok {
					start, _ := p.span(ep.file.Duration)
					s, e := start+m.AStart, start+m.AEnd
					if p.fromEnd && ep.file.Duration-e < endSnap {
						e = ep.file.Duration
					}
					if err := d.st.SetDetectedMarker(ep.item.ID, p.name, s, e); err != nil {
						return found, err
					}
					found++
				}
			}
		}
	}
	return found, d.st.MarkMarkersScanned(ids)
}

// bestMatch compares episode i with its nearest neighbours (the theme is
// most likely unchanged between adjacent episodes) and keeps the longest hit.
func (d *Detector) bestMatch(ctx context.Context, eps []*episode, i int, p part) (Match, bool) {
	a, err := d.fp(ctx, eps[i], p)
	if err != nil {
		log.Printf("markers: %s: %v", eps[i].file.Path, err)
		return Match{}, false
	}
	var best Match
	ok := false
	for _, j := range []int{i + 1, i - 1, i + 2, i - 2} {
		if j < 0 || j >= len(eps) {
			continue
		}
		b, err := d.fp(ctx, eps[j], p)
		if err != nil {
			continue
		}
		if m, hit := FindCommon(a, b, p.minLen, p.maxLen); hit && (!ok || m.Duration() > best.Duration()) {
			best, ok = m, true
		}
		if ok && j == i-1 {
			break // two neighbours checked; good enough
		}
	}
	return best, ok
}

func (d *Detector) fp(ctx context.Context, ep *episode, p part) ([]uint32, error) {
	if fp, ok := ep.fp[p.name]; ok {
		return fp, nil
	}
	fp, err := d.fingerprint(ctx, ep.file, p)
	if err == nil {
		ep.fp[p.name] = fp
	}
	return fp, err
}

// Pending analyzes every season that has new or unanalyzed episodes, and
// every movie not yet checked for end credits.
func (d *Detector) Pending(ctx context.Context) (string, error) {
	seasons, err := d.st.SeasonsNeedingMarkers()
	if err != nil {
		return "", err
	}
	total := 0
	for _, s := range seasons {
		n, err := d.Season(ctx, s)
		if err != nil {
			return "", err
		}
		total += n
	}
	movies, err := d.st.MoviesNeedingMarkers()
	if err != nil {
		return "", err
	}
	for _, id := range movies {
		found, err := d.Movie(ctx, id)
		if err != nil {
			return "", err
		}
		if found {
			total++
		}
	}
	return fmt.Sprintf("analyzed %d seasons and %d movies, set %d markers", len(seasons), len(movies), total), nil
}
