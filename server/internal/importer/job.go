package importer

import (
	"context"
	"fmt"
	"io"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

// SourceUser is an account on the source server.
type SourceUser struct {
	Name  string `json:"name"`
	Plays int    `json:"plays,omitempty"` // when the source can count cheaply
}

// Collection is a collection on the source, with its members as records.
type Collection struct {
	Name     string
	Summary  string
	Items    []Record
	Poster   string // source image paths for ArtworkSource.Image, or a "tmdb:/<file>" reference; "" = none
	Backdrop string
	// Label makes the collection gather everything carrying it, as well as
	// its members (Cinemask's "fill from label").
	Label string
	// ItemLabel is put on every member (Cinemask's Plex label).
	ItemLabel string
}

// LabelSet is a label on the source and the titles carrying it.
type LabelSet struct {
	Name  string
	Items []Record
}

// labelSource is a source that can send its labels (Plex, where Kometa puts them).
type labelSource interface {
	Labels(ctx context.Context, emit func(LabelSet) error) error
}

// Art is an item's poster and background on the source.
type Art struct {
	Record          // which item (Kind movie, show or season)
	Poster   string // source image paths for ArtworkSource.Image; "" = none
	Backdrop string
}

// ArtworkSource is a Source that can also hand over its posters and backgrounds.
type ArtworkSource interface {
	Artwork(ctx context.Context, emit func(Art) error, progress func(phase string, done, total int)) error
	Image(ctx context.Context, path string) (io.ReadCloser, error)
}

// ImageSaver stores a downloaded image and returns its artwork reference.
type ImageSaver interface {
	Save(r io.Reader) (string, error)
}

// Source is somewhere history can be imported from.
type Source interface {
	Kind() string // "plex", "tautulli"
	Users(ctx context.Context) ([]SourceUser, error)
	// Records sends history plays (and, when state is set, watched/resume state).
	Records(ctx context.Context, state bool, emit func(Record) error, progress func(phase string, done, total int)) error
	// Collections sends collections; sources without any return nil.
	Collections(ctx context.Context, emit func(Collection) error) error
}

// Options say what to import and for whom.
type Options struct {
	History     bool             `json:"history"`
	Watched     bool             `json:"watched"` // watched marks and resume points
	Collections bool             `json:"collections"`
	Artwork     bool             `json:"artwork"` // posters and backgrounds (ArtworkSource only)
	Labels      bool             `json:"labels"`  // put the source's labels on matching titles
	DryRun      bool             `json:"dryRun"`
	Users       map[string]int64 `json:"users"` // source user → BingeBandit user id; missing or 0 = skip
}

// Status is what the admin page shows while and after an import runs.
type Status struct {
	Running      bool     `json:"running"`
	Source       string   `json:"source"`
	DryRun       bool     `json:"dryRun"`
	Phase        string   `json:"phase"`
	Done         int      `json:"done"`
	Total        int      `json:"total"`
	StartedAt    int64    `json:"startedAt"`
	FinishedAt   int64    `json:"finishedAt"`
	Seen         int      `json:"seen"`    // records read
	Matched      int      `json:"matched"` // records that found a library item
	Plays        int      `json:"plays"`   // history rows added
	Duplicates   int      `json:"duplicates"`
	Watched      int      `json:"watched"` // items marked watched
	Resumes      int      `json:"resumes"` // resume points set
	SkippedUser  int      `json:"skippedUser"`
	Collections  int      `json:"collections"`
	CollItems    int      `json:"collectionItems"`
	Artwork      int      `json:"artwork"` // images replaced
	ArtFailed    int      `json:"artworkFailed"`
	Labels       int      `json:"labels"`    // labels put on titles
	Unmatched    []string `json:"unmatched"` // most-played titles that weren't found
	UnmatchedNum int      `json:"unmatchedCount"`
	Error        string   `json:"error,omitempty"`
}

// Runner runs one import at a time in the background.
type Runner struct {
	st *store.Store
	// Images stores artwork brought over from the source; without it artwork is skipped.
	Images ImageSaver

	mu      sync.Mutex
	status  Status
	missing map[string]int
	cancel  context.CancelFunc
}

func NewRunner(st *store.Store) *Runner { return &Runner{st: st} }

func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.status
	s.Unmatched = append([]string(nil), s.Unmatched...)
	return s
}

func (r *Runner) update(f func(s *Status)) {
	r.mu.Lock()
	f(&r.status)
	r.mu.Unlock()
}

// Cancel stops a running import; what was imported so far stays.
func (r *Runner) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}

// Start begins an import in the background.
func (r *Runner) Start(parent context.Context, src Source, opt Options) error {
	r.mu.Lock()
	if r.status.Running {
		r.mu.Unlock()
		return fmt.Errorf("an import is already running")
	}
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.missing = map[string]int{}
	r.status = Status{Running: true, Source: src.Kind(), DryRun: opt.DryRun, Phase: "Loading your libraries", StartedAt: time.Now().Unix()}
	r.mu.Unlock()

	go func() {
		defer cancel()
		err := r.run(ctx, src, opt)
		r.mu.Lock()
		defer r.mu.Unlock()
		r.status.Running = false
		r.status.FinishedAt = time.Now().Unix()
		r.status.Phase = "Finished"
		if err != nil {
			r.status.Error = err.Error()
			r.status.Phase = "Stopped"
			if ctx.Err() != nil {
				r.status.Error = "cancelled"
			}
		}
		r.status.Unmatched, r.status.UnmatchedNum = topMissing(r.missing, 50)
		log.Printf("import (%s): %d read, %d matched, %d plays, %d watched, %d resume points, %d collection items, %d images; err=%v",
			src.Kind(), r.status.Seen, r.status.Matched, r.status.Plays, r.status.Watched, r.status.Resumes, r.status.CollItems, r.status.Artwork, err)
	}()
	return nil
}

// RunOnce imports synchronously, outside the shared admin runner (used for
// one person's own Trakt history).
func RunOnce(ctx context.Context, st *store.Store, src Source, opt Options) (Status, error) {
	r := &Runner{st: st, missing: map[string]int{}}
	opt.Artwork = false
	r.status = Status{Running: true, Source: src.Kind(), DryRun: opt.DryRun, StartedAt: time.Now().Unix()}
	err := r.run(ctx, src, opt)
	r.status.Running, r.status.FinishedAt, r.status.Phase = false, time.Now().Unix(), "Finished"
	if err != nil {
		r.status.Error = err.Error()
	}
	r.status.Unmatched, r.status.UnmatchedNum = topMissing(r.missing, 50)
	return r.status, err
}

func topMissing(m map[string]int, n int) ([]string, int) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	total := len(keys)
	if len(keys) > n {
		keys = keys[:n]
	}
	return keys, total
}

func (r *Runner) run(ctx context.Context, src Source, opt Options) error {
	idx, err := r.st.ImportIndex()
	if err != nil {
		return err
	}
	m := NewMatcher(idx)
	progress := func(phase string, done, total int) {
		r.update(func(s *Status) { s.Phase, s.Done, s.Total = phase, done, total })
	}

	// Collections first: they're quick, and a long history import that
	// fails part way shouldn't take them with it.
	if opt.Collections {
		progress("Collections", 0, 0)
		err := src.Collections(ctx, func(c Collection) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return r.applyCollection(ctx, src, m, c, opt)
		})
		if err != nil {
			return err
		}
	}
	if ls, ok := src.(labelSource); ok && opt.Labels {
		progress("Labels", 0, 0)
		err := ls.Labels(ctx, func(l LabelSet) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return r.applyLabel(m, l, opt)
		})
		if err != nil {
			return err
		}
	}
	if as, ok := src.(ArtworkSource); ok && opt.Artwork && r.Images != nil {
		err := as.Artwork(ctx, func(a Art) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return r.applyArtwork(ctx, as, m, a, opt)
		}, progress)
		if err != nil {
			return err
		}
	}
	if opt.History || opt.Watched {
		err := src.Records(ctx, opt.Watched, func(rec Record) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !wanted(&rec, opt) {
				return nil
			}
			return r.apply(m, &rec, opt)
		}, progress)
		if err != nil {
			return err
		}
	}
	return nil
}

// wanted: state records need "watched"; plays need "history", or "watched"
// when the play finished the item.
func wanted(rec *Record, opt Options) bool {
	if rec.StateOnly {
		return opt.Watched
	}
	return opt.History || (opt.Watched && rec.Completed)
}

func (r *Runner) apply(m *Matcher, rec *Record, opt Options) error {
	uid := opt.Users[rec.User]
	it := m.Match(rec)
	r.update(func(s *Status) {
		s.Seen++
		if it != nil {
			s.Matched++
		}
		if uid == 0 {
			s.SkippedUser++
		}
	})
	if it == nil {
		r.mu.Lock()
		r.missing[rec.Label()]++
		r.mu.Unlock()
		return nil
	}
	if uid == 0 {
		return nil
	}
	if opt.DryRun {
		// Count what would be written (duplicates aren't detected without writing).
		r.update(func(s *Status) {
			switch {
			case rec.StateOnly && rec.Completed:
				s.Watched++
			case rec.StateOnly && rec.Position > 0:
				s.Resumes++
			case !rec.StateOnly:
				if opt.History {
					s.Plays++
				}
				if opt.Watched && rec.Completed {
					s.Watched++
				}
			}
		})
		return nil
	}

	if rec.StateOnly {
		if rec.Completed {
			if err := r.st.ImportWatched(uid, it.ID, rec.ViewedAt); err != nil {
				return err
			}
			r.update(func(s *Status) { s.Watched++ })
		} else if rec.Position > 0 {
			set, err := r.st.ImportResume(uid, it.ID, rec.Position, rec.Duration, rec.ViewedAt)
			if err != nil {
				return err
			}
			if set {
				r.update(func(s *Status) { s.Resumes++ })
			}
		}
		return nil
	}

	if opt.History {
		started := rec.StartedAt
		if started == 0 {
			started = rec.ViewedAt - max(rec.Played, 0)
		}
		// Titles come from this library, not the source, so history reads like everything else.
		p := &store.Play{
			UserID: uid, ItemID: it.ID, Title: it.Title, Kind: rec.Kind, LibraryID: it.LibraryID, ShowID: it.ShowID,
			StartedAt: started, StoppedAt: rec.ViewedAt, Watched: rec.Played, Completed: rec.Completed,
			Mode: "imported", Client: rec.Client,
		}
		if rec.Kind == "episode" {
			p.Title = rec.ShowTitle
			if show := m.Item(it.ShowID); show != nil {
				p.Title = show.Title
			}
			p.Subtitle = fmt.Sprintf("S%d · E%d %s", it.Season, it.Episode, it.Title)
		}
		added, err := r.st.ImportPlay(p, rec.Key)
		if err != nil {
			return err
		}
		r.update(func(s *Status) {
			if added {
				s.Plays++
			} else {
				s.Duplicates++
			}
		})
	}
	if opt.Watched && rec.Completed {
		if err := r.st.ImportWatched(uid, it.ID, rec.ViewedAt); err != nil {
			return err
		}
		r.update(func(s *Status) { s.Watched++ })
	}
	return nil
}

func (r *Runner) applyCollection(ctx context.Context, src Source, m *Matcher, c Collection, opt Options) error {
	byLib := map[int64][]int64{}
	for i := range c.Items {
		if it := m.Match(&c.Items[i]); it != nil {
			byLib[it.LibraryID] = append(byLib[it.LibraryID], it.ID)
		}
	}
	if len(byLib) == 0 {
		r.mu.Lock()
		r.missing["Collection: "+c.Name]++
		r.mu.Unlock()
		return nil
	}
	as := r.artworkFor(src, c, opt)
	for lib, ids := range byLib {
		n := len(ids)
		if !opt.DryRun {
			id, added, err := r.st.EnsureCollection(lib, c.Name, c.Summary, ids)
			if err != nil {
				return err
			}
			n = added
			if c.Label != "" {
				if err := r.st.SetCollectionLabel(id, c.Label); err != nil {
					return err
				}
			}
			if c.ItemLabel != "" {
				for _, it := range ids {
					if _, err := r.st.AddItemLabel(it, c.ItemLabel); err != nil {
						return err
					}
				}
			}
			if as != nil {
				for kind, path := range map[string]string{"poster": c.Poster, "backdrop": c.Backdrop} {
					if path == "" {
						continue
					}
					ref, err := r.fetchImage(ctx, as, path)
					if err == nil {
						err = r.st.SetCollectionArtwork(id, kind, ref)
					}
					if ctx.Err() != nil {
						return ctx.Err()
					}
					r.countArt(err, "collection "+c.Name)
				}
			}
		} else if as != nil {
			r.update(func(s *Status) {
				for _, p := range []string{c.Poster, c.Backdrop} {
					if p != "" {
						s.Artwork++
					}
				}
			})
		}
		r.update(func(s *Status) { s.Collections++; s.CollItems += n })
	}
	return nil
}

// artworkFor returns the source to download a collection's images from, when
// artwork is being imported.
func (r *Runner) artworkFor(src Source, c Collection, opt Options) ArtworkSource {
	if !opt.Artwork || r.Images == nil || (c.Poster == "" && c.Backdrop == "") {
		return nil
	}
	as, _ := src.(ArtworkSource)
	return as
}

func (r *Runner) applyArtwork(ctx context.Context, as ArtworkSource, m *Matcher, a Art, opt Options) error {
	it := m.Match(&a.Record)
	if it == nil {
		return nil // history already reports what's missing; a season without a folder here is normal
	}
	for _, img := range []struct{ kind, path string }{{"poster", a.Poster}, {"backdrop", a.Backdrop}} {
		if img.path == "" {
			continue
		}
		if opt.DryRun {
			r.update(func(s *Status) { s.Artwork++ })
			continue
		}
		ref, err := r.fetchImage(ctx, as, img.path)
		if err == nil {
			err = r.st.SetArtwork(it.ID, img.kind, ref)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.countArt(err, a.Label())
	}
	return nil
}

func (r *Runner) applyLabel(m *Matcher, l LabelSet, opt Options) error {
	for i := range l.Items {
		it := m.Match(&l.Items[i])
		if it == nil {
			continue
		}
		added := true
		if !opt.DryRun {
			var err error
			if added, err = r.st.AddItemLabel(it.ID, l.Name); err != nil {
				return err
			}
		}
		if added {
			r.update(func(s *Status) { s.Labels++ })
		}
	}
	return nil
}

func (r *Runner) fetchImage(ctx context.Context, as ArtworkSource, path string) (string, error) {
	if metadata.ValidTMDBRef(path) {
		return path, nil // TMDB images are served by reference
	}
	body, err := as.Image(ctx, path)
	if err != nil {
		return "", err
	}
	defer body.Close()
	return r.Images.Save(body)
}

// countArt tallies one image; a single bad image doesn't stop the import.
func (r *Runner) countArt(err error, what string) {
	if err != nil {
		log.Printf("import: artwork for %s: %v", what, err)
	}
	r.update(func(s *Status) {
		if err != nil {
			s.ArtFailed++
		} else {
			s.Artwork++
		}
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

func fmtEpisode(show string, season, episode int, title string) string {
	s := fmt.Sprintf("%s S%02dE%02d", show, season, episode)
	if title != "" {
		s += " · " + title
	}
	return s
}
