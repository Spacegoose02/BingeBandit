// Package library walks library folders, turns files into movies/shows/
// seasons/episodes, probes them, and prunes what disappeared.
package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/media"
	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

type Status struct {
	LibraryID int64  `json:"libraryId"`
	Running   bool   `json:"running"`
	Phase     string `json:"phase"` // scanning | metadata | done
	Found     int    `json:"found"`
	Processed int    `json:"processed"`
	Started   int64  `json:"started"`
	Finished  int64  `json:"finished"`
	Error     string `json:"error,omitempty"`
}

type Scanner struct {
	st      *store.Store
	meta    *metadata.Service
	ffprobe string

	mu     sync.Mutex
	status map[int64]*Status
}

func NewScanner(st *store.Store, meta *metadata.Service, ffprobe string) *Scanner {
	return &Scanner{st: st, meta: meta, ffprobe: ffprobe, status: map[int64]*Status{}}
}

var ErrRunning = errors.New("a scan is already running for this library")

// Scan starts a background scan; metadata for new items is fetched afterwards.
func (s *Scanner) Scan(libraryID int64) error {
	return s.start(libraryID, func(ctx context.Context, lib *store.Library, st *Status) error {
		if err := s.scanFiles(ctx, lib, st); err != nil {
			return err
		}
		return s.fetchMetadata(ctx, lib, st, false)
	})
}

// RefreshMetadata re-fetches metadata for every item in a library.
func (s *Scanner) RefreshMetadata(libraryID int64) error {
	return s.start(libraryID, func(ctx context.Context, lib *store.Library, st *Status) error {
		return s.fetchMetadata(ctx, lib, st, true)
	})
}

func (s *Scanner) start(libraryID int64, fn func(context.Context, *store.Library, *Status) error) error {
	lib, err := s.st.Library(libraryID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if cur := s.status[libraryID]; cur != nil && cur.Running {
		s.mu.Unlock()
		return ErrRunning
	}
	st := &Status{LibraryID: libraryID, Running: true, Phase: "scanning", Started: time.Now().Unix()}
	s.status[libraryID] = st
	s.mu.Unlock()

	go func() {
		err := fn(context.Background(), lib, st)
		s.mu.Lock()
		defer s.mu.Unlock()
		st.Running, st.Phase, st.Finished = false, "done", time.Now().Unix()
		if err != nil {
			st.Error = err.Error()
			log.Printf("scan %q: %v", lib.Name, err)
		}
	}()
	return nil
}

func (s *Scanner) update(st *Status, fn func(*Status)) {
	s.mu.Lock()
	fn(st)
	s.mu.Unlock()
}

func (s *Scanner) Statuses() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Status, 0, len(s.status))
	for _, st := range s.status {
		out = append(out, *st)
	}
	return out
}

func (s *Scanner) ScanAll() {
	libs, err := s.st.Libraries()
	if err != nil {
		log.Printf("scan all: %v", err)
		return
	}
	for _, l := range libs {
		if err := s.Scan(l.ID); err != nil && !errors.Is(err, ErrRunning) {
			log.Printf("scan %q: %v", l.Name, err)
		}
	}
}

// RunSchedule rescans libraries whose last scan is older than the configured interval.
func (s *Scanner) RunSchedule(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		hours, _ := strconv.Atoi(s.st.Setting(store.SettingScanIntervalHours))
		if hours <= 0 {
			continue
		}
		libs, err := s.st.Libraries()
		if err != nil {
			continue
		}
		for _, l := range libs {
			if time.Since(time.Unix(l.LastScan, 0)) >= time.Duration(hours)*time.Hour {
				s.Scan(l.ID)
			}
		}
	}
}

type found struct{ root, path string }

func (s *Scanner) scanFiles(ctx context.Context, lib *store.Library, st *Status) error {
	gen := time.Now().UnixNano()
	complete := true
	var files []found
	for _, root := range lib.Paths {
		root = filepath.Clean(root)
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			// An offline drive must not wipe the library, so skip pruning.
			log.Printf("scan %q: root %s unavailable: %v", lib.Name, root, err)
			complete = false
			continue
		}
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				log.Printf("scan %q: %v", lib.Name, err)
				if d != nil && d.IsDir() {
					complete = false
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if p != root && skipDir(d.Name()) {
					return fs.SkipDir
				}
				return nil
			}
			if isVideo(d.Name()) && !isSample(d.Name()) {
				files = append(files, found{root, p})
			}
			return nil
		})
	}
	s.update(st, func(st *Status) { st.Found = len(files) })

	for i, f := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.processFile(ctx, lib, f, gen); err != nil {
			log.Printf("scan %q: %v", lib.Name, err)
		}
		s.update(st, func(st *Status) { st.Processed = i + 1 })
	}

	if complete {
		removed, err := s.st.PruneLibrary(lib.ID, gen)
		if err != nil {
			return fmt.Errorf("prune: %w", err)
		}
		if removed > 0 {
			log.Printf("scan %q: removed %d missing files", lib.Name, removed)
		}
	}
	return s.st.SetLibraryScanned(lib.ID)
}

func (s *Scanner) processFile(ctx context.Context, lib *store.Library, f found, gen int64) error {
	info, err := os.Stat(f.path)
	if err != nil {
		return err
	}
	var itemID int64
	switch lib.Kind {
	case "movies":
		itemID, err = s.movieItem(lib, f)
	case "shows":
		itemID, err = s.episodeItem(lib, f)
	default:
		err = fmt.Errorf("unknown library kind %q", lib.Kind)
	}
	if err != nil {
		return err
	}

	existing, err := s.st.FileByPath(f.path)
	if err == nil && existing.Size == info.Size() && existing.MTime == info.ModTime().Unix() && existing.ProbedAt > 0 {
		return s.st.TouchFile(existing.ID, itemID, gen)
	}
	mf := &store.MediaFile{ItemID: itemID, Path: f.path, Size: info.Size(), MTime: info.ModTime().Unix()}
	if err := media.Probe(ctx, s.ffprobe, mf); err != nil {
		log.Printf("probe: %v", err)
		mf.Container = strings.TrimPrefix(strings.ToLower(filepath.Ext(f.path)), ".")
	}
	return s.st.UpsertFile(mf, gen)
}

// movieItem prefers a "Title (Year)" folder over the filename, since folders
// are usually curated and filenames often carry release-group noise.
func (s *Scanner) movieItem(lib *store.Library, f found) (int64, error) {
	name := strings.TrimSuffix(filepath.Base(f.path), filepath.Ext(f.path))
	title, year, tmdb := parseTitleYear(name)
	if dir := filepath.Dir(f.path); dir != f.root {
		dt, dy, dtmdb := parseTitleYear(filepath.Base(dir))
		// Without a year the folder may just be a grouping like "Action".
		if dy != 0 {
			title, year = dt, dy
		}
		if tmdb == 0 {
			tmdb = dtmdb
		}
	}
	if title == "" {
		title = name
	}
	return s.st.UpsertItem(&store.Item{
		LibraryID: lib.ID, Kind: "movie", Title: title, Year: year, TMDBID: tmdb,
		MatchKey: fmt.Sprintf("movie:%s:%d", normKey(title), year),
	})
}

// episodeItem maps Show/Season N/Show - S01E02 - Title.mkv (and looser
// layouts) onto show, season and episode rows, creating them as needed.
func (s *Scanner) episodeItem(lib *store.Library, f found) (int64, error) {
	rel, err := filepath.Rel(f.root, f.path)
	if err != nil {
		return 0, err
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	name := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(f.path))

	season, episode, prefix, rest, ok := parseEpisode(name)
	if !ok && len(parts) >= 3 {
		if sn, sok := parseSeasonDir(parts[len(parts)-2]); sok {
			if en, eok := parseLeadingNumber(name); eok {
				season, episode, ok = sn, en, true
				rest = reLeadNum.ReplaceAllString(name, "")
			}
		}
	}
	if !ok {
		return 0, fmt.Errorf("cannot find season/episode in %s", f.path)
	}

	showName := parts[0]
	if len(parts) == 1 {
		showName = prefix
	}
	showTitle, showYear, tmdb := parseTitleYear(showName)
	if showTitle == "" {
		return 0, fmt.Errorf("cannot find show name for %s", f.path)
	}
	showKey := fmt.Sprintf("show:%s:%d", normKey(showTitle), showYear)

	showID, err := s.st.UpsertItem(&store.Item{
		LibraryID: lib.ID, Kind: "show", Title: showTitle, Year: showYear, TMDBID: tmdb, MatchKey: showKey,
	})
	if err != nil {
		return 0, err
	}
	seasonTitle := fmt.Sprintf("Season %d", season)
	if season == 0 {
		seasonTitle = "Specials"
	}
	seasonID, err := s.st.UpsertItem(&store.Item{
		LibraryID: lib.ID, Kind: "season", ParentID: showID, ShowID: showID, Title: seasonTitle,
		SeasonNumber: &season, MatchKey: fmt.Sprintf("%s:s%d", showKey, season),
	})
	if err != nil {
		return 0, err
	}
	epTitle := episodeTitle(rest)
	if epTitle == "" {
		epTitle = fmt.Sprintf("Episode %d", episode)
	}
	return s.st.UpsertItem(&store.Item{
		LibraryID: lib.ID, Kind: "episode", ParentID: seasonID, ShowID: showID, Title: epTitle,
		SeasonNumber: &season, EpisodeNumber: &episode, MatchKey: fmt.Sprintf("%s:s%d:e%d", showKey, season, episode),
	})
}

func (s *Scanner) fetchMetadata(ctx context.Context, lib *store.Library, st *Status, force bool) error {
	if !s.meta.Enabled() {
		return nil
	}
	s.update(st, func(st *Status) { st.Phase, st.Processed, st.Found = "metadata", 0, 0 })
	return s.meta.RefreshLibrary(ctx, lib.ID, force, func(done, total int) {
		s.update(st, func(st *Status) { st.Processed, st.Found = done, total })
	})
}
