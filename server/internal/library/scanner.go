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
	"strings"
	"sync"
	"sync/atomic"
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

// probeWorkers is how many files are probed at once. ffprobe mostly waits on
// disk/network, so a few in parallel speeds up big first scans a lot.
const probeWorkers = 4

type job func(context.Context, *store.Library, *Status) error

// Scan starts a background scan; metadata for new items is fetched afterwards.
func (s *Scanner) Scan(libraryID int64) error { return s.start(libraryID, false, s.fullScan) }

// ScanWait is Scan but blocks until it finishes (scheduled tasks).
func (s *Scanner) ScanWait(libraryID int64) error { return s.start(libraryID, true, s.fullScan) }

// RefreshMetadata re-fetches metadata for every item in a library, in the background.
func (s *Scanner) RefreshMetadata(libraryID int64) error {
	return s.start(libraryID, false, s.refreshAll)
}

// RefreshMetadataWait is RefreshMetadata but blocks until it finishes.
func (s *Scanner) RefreshMetadataWait(libraryID int64) error {
	return s.start(libraryID, true, s.refreshAll)
}

// ScanFolders rescans only the given folders of a library and blocks until
// done. Folder watching uses it so a new episode doesn't trigger a full scan.
func (s *Scanner) ScanFolders(libraryID int64, dirs []string) error {
	return s.start(libraryID, true, func(ctx context.Context, lib *store.Library, st *Status) error {
		if err := s.scanFolders(ctx, lib, st, dirs); err != nil {
			return err
		}
		return s.fetchMetadata(ctx, lib, st, false)
	})
}

func (s *Scanner) fullScan(ctx context.Context, lib *store.Library, st *Status) error {
	if err := s.scanFiles(ctx, lib, st); err != nil {
		return err
	}
	return s.fetchMetadata(ctx, lib, st, false)
}

func (s *Scanner) refreshAll(ctx context.Context, lib *store.Library, st *Status) error {
	return s.fetchMetadata(ctx, lib, st, true)
}

func (s *Scanner) start(libraryID int64, wait bool, fn job) error {
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

	run := func() error {
		err := fn(context.Background(), lib, st)
		s.mu.Lock()
		defer s.mu.Unlock()
		st.Running, st.Phase, st.Finished = false, "done", time.Now().Unix()
		if err != nil {
			st.Error = err.Error()
			log.Printf("scan %q: %v", lib.Name, err)
		}
		return err
	}
	if wait {
		return run()
	}
	go run()
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

type found struct{ root, path string }

// walk lists video files under start (root or a folder inside it).
// complete is false if any folder could not be read.
func walk(lib *store.Library, root, start string) (files []found, complete bool) {
	complete = true
	filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
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
	return files, complete
}

func reachable(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func (s *Scanner) scanFiles(ctx context.Context, lib *store.Library, st *Status) error {
	gen := time.Now().UnixNano()
	complete := true
	var files []found
	for _, root := range lib.Paths {
		root = filepath.Clean(root)
		if !reachable(root) {
			// An offline drive must not wipe the library, so skip pruning.
			log.Printf("scan %q: folder %s is unavailable; keeping its items", lib.Name, root)
			complete = false
			continue
		}
		f, ok := walk(lib, root, root)
		files, complete = append(files, f...), complete && ok
	}
	if err := s.processAll(ctx, lib, st, files, gen); err != nil {
		return err
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

// scanFolders processes files under dirs and prunes only within them.
func (s *Scanner) scanFolders(ctx context.Context, lib *store.Library, st *Status, dirs []string) error {
	gen := time.Now().UnixNano()
	var files []found
	var prefixes []string
	for _, d := range dirs {
		d = filepath.Clean(d)
		root := rootFor(lib, d)
		if root == "" || !reachable(root) {
			continue
		}
		complete := true
		if reachable(d) {
			var f []found
			f, complete = walk(lib, root, d)
			files = append(files, f...)
		}
		if complete {
			prefixes = append(prefixes, strings.TrimSuffix(d, string(filepath.Separator))+string(filepath.Separator))
		}
	}
	if err := s.processAll(ctx, lib, st, files, gen); err != nil {
		return err
	}
	removed, err := s.st.PruneUnder(lib.ID, gen, prefixes)
	if err != nil {
		return fmt.Errorf("prune: %w", err)
	}
	log.Printf("scan %q: rescanned %d changed folder(s): %d files, %d removed", lib.Name, len(dirs), len(files), removed)
	return nil
}

// rootFor returns the library folder containing path, or "".
func rootFor(lib *store.Library, path string) string {
	best := ""
	for _, r := range lib.Paths {
		r = filepath.Clean(r)
		if (path == r || strings.HasPrefix(path, r+string(filepath.Separator))) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

func (s *Scanner) processAll(ctx context.Context, lib *store.Library, st *Status, files []found, gen int64) error {
	s.update(st, func(st *Status) { st.Found, st.Processed = len(files), 0 })
	var done atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, probeWorkers)
	for _, f := range files {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(f found) {
			defer func() { <-sem; wg.Done() }()
			if err := s.processFile(ctx, lib, f, gen); err != nil {
				log.Printf("scan %q: %v", lib.Name, err)
			}
			n := int(done.Add(1))
			s.update(st, func(st *Status) { st.Processed = max(st.Processed, n) })
		}(f)
	}
	wg.Wait()
	return ctx.Err()
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

	side := sidecars(f.path)
	existing, err := s.st.FileByPath(f.path)
	if err == nil && existing.Size == info.Size() && existing.MTime == info.ModTime().Unix() && existing.ProbedAt > 0 {
		// The video is unchanged, but subtitle files next to it may not be.
		if !sameSidecars(existing.Streams, side) {
			if err := s.st.UpdateStreams(existing.ID, withSidecars(existing.Streams, side)); err != nil {
				return err
			}
		}
		return s.st.TouchFile(existing.ID, itemID, gen)
	}
	mf := &store.MediaFile{ItemID: itemID, Path: f.path, Size: info.Size(), MTime: info.ModTime().Unix()}
	if err := media.Probe(ctx, s.ffprobe, mf); err != nil {
		log.Printf("probe: %v", err)
		mf.Container = strings.TrimPrefix(strings.ToLower(filepath.Ext(f.path)), ".")
	}
	mf.Streams = withSidecars(mf.Streams, side)
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

	// A folder whose show was merged into another (same TMDB/TVDB show)
	// adds its episodes there.
	showID, aliasKey, merged := s.st.ShowAlias(lib.ID, showKey)
	if merged {
		showKey = aliasKey
	} else {
		showID, err = s.st.UpsertItem(&store.Item{
			LibraryID: lib.ID, Kind: "show", Title: showTitle, Year: showYear, TMDBID: tmdb, MatchKey: showKey,
		})
		if err != nil {
			return 0, err
		}
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
		if season >= 1900 { // date-based: season is the year, episode MMDDhhmm
			t := time.Date(season, time.Month(episode/1000000), episode/10000%100, episode/100%100, episode%100, 0, 0, time.Local)
			epTitle = t.Format("Jan 2, 2006")
			if episode%10000 != 0 {
				epTitle = t.Format("Jan 2, 2006 3:04 PM")
			}
		}
	}
	return s.st.UpsertItem(&store.Item{
		LibraryID: lib.ID, Kind: "episode", ParentID: seasonID, ShowID: showID, Title: epTitle,
		SeasonNumber: &season, EpisodeNumber: &episode, MatchKey: fmt.Sprintf("%s:s%d:e%d", showKey, season, episode),
	})
}

func (s *Scanner) fetchMetadata(ctx context.Context, lib *store.Library, st *Status, force bool) error {
	if !s.meta.AnyEnabled() {
		return nil
	}
	s.update(st, func(st *Status) { st.Phase, st.Processed, st.Found = "metadata", 0, 0 })
	progress := func(done, total int) {
		s.update(st, func(st *Status) { st.Processed, st.Found = done, total })
	}
	if err := s.meta.RefreshLibrary(ctx, lib.ID, force, progress); err != nil {
		return err
	}
	if lib.Kind != "shows" {
		return nil
	}
	merged, err := s.st.MergeDuplicateShows(lib.ID)
	if err != nil {
		return fmt.Errorf("merge duplicate shows: %w", err)
	}
	if len(merged) == 0 {
		return nil
	}
	for _, m := range merged {
		log.Printf("scan %q: merged a second folder of %q into show %d", lib.Name, m.Title, m.KeptID)
	}
	// Episodes new to the kept show still need their details.
	return s.meta.RefreshLibrary(ctx, lib.ID, false, progress)
}
