package library

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"bingebandit/internal/store"
)

const (
	// settleDelay waits for copies to finish: each new event on a library
	// restarts its timer, and the folder scan runs once things are quiet.
	settleDelay = 30 * time.Second
	// rewatchEvery re-reads library folders and the watch setting so new
	// libraries and toggles take effect without a restart.
	rewatchEvery = 2 * time.Minute
)

// Watch keeps the library folders under file-system watch and rescans just
// the folders that change. It blocks until ctx ends. Network shares may not
// deliver events; the scheduled full scan still catches those.
func (s *Scanner) Watch(ctx context.Context) {
	w := &watcher{s: s, pending: map[int64]map[string]bool{}, timers: map[int64]*time.Timer{}}
	t := time.NewTicker(rewatchEvery)
	defer t.Stop()
	defer w.close()
	for {
		w.sync()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type watcher struct {
	s     *Scanner
	mu    sync.Mutex
	fw    *fsnotify.Watcher
	roots string // fingerprint of the watched library roots

	pending map[int64]map[string]bool // library → changed folders
	timers  map[int64]*time.Timer
}

func (w *watcher) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fw != nil {
		w.fw.Close()
		w.fw = nil
	}
	for _, t := range w.timers {
		t.Stop()
	}
}

// sync (re)builds the watch set when libraries or the setting changed.
func (w *watcher) sync() {
	enabled := w.s.st.Setting(store.SettingWatchFolders) == "true"
	libs, err := w.s.st.Libraries()
	if err != nil {
		return
	}
	var roots []string
	for _, l := range libs {
		for _, p := range l.Paths {
			roots = append(roots, filepath.Clean(p))
		}
	}
	sort.Strings(roots)
	fingerprint := strings.Join(roots, "\n")
	if !enabled {
		fingerprint = ""
	}

	w.mu.Lock()
	unchanged := fingerprint == w.roots && (w.fw != nil) == enabled
	w.mu.Unlock()
	if unchanged {
		return
	}
	w.close()
	w.mu.Lock()
	w.roots = fingerprint
	w.mu.Unlock()
	if !enabled || len(roots) == 0 {
		return
	}

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("watch: %v", err)
		return
	}
	n := 0
	for _, r := range roots {
		n += addTree(fw, r)
	}
	w.mu.Lock()
	w.fw = fw
	w.mu.Unlock()
	log.Printf("watch: watching %d folders in %d library locations for changes", n, len(roots))
	go w.loop(fw)
}

// addTree watches dir and every non-skipped folder below it.
func addTree(fw *fsnotify.Watcher, dir string) int {
	n := 0
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if p != dir && skipDir(d.Name()) {
			return fs.SkipDir
		}
		if err := fw.Add(p); err != nil {
			log.Printf("watch: %s: %v", p, err)
			return fs.SkipDir
		}
		n++
		return nil
	})
	return n
}

func (w *watcher) loop(fw *fsnotify.Watcher) {
	for {
		select {
		case ev, ok := <-fw.Events:
			if !ok {
				return
			}
			w.handle(fw, ev)
		case err, ok := <-fw.Errors:
			if !ok {
				return
			}
			log.Printf("watch: %v", err)
		}
	}
}

func (w *watcher) handle(fw *fsnotify.Watcher, ev fsnotify.Event) {
	if ev.Has(fsnotify.Chmod) && !ev.Has(fsnotify.Write) {
		return
	}
	info, statErr := os.Stat(ev.Name)
	isDir := statErr == nil && info.IsDir()
	if isDir && ev.Has(fsnotify.Create) {
		addTree(fw, ev.Name)
	}
	// Removed paths can't be stat'ed; a name without a video extension may
	// have been a folder, so rescan its parent either way.
	gone := errors.Is(statErr, os.ErrNotExist)
	relevant := isVideo(ev.Name) || isSubtitleFile(ev.Name)
	if !isDir && !gone && !relevant {
		return
	}
	if gone && !relevant && filepath.Ext(ev.Name) != "" {
		return // e.g. a deleted .nfo
	}
	dir := filepath.Dir(ev.Name)
	if isDir {
		dir = ev.Name
	}
	dir = videoDir(dir)

	libs, err := w.s.st.Libraries()
	if err != nil {
		return
	}
	for _, l := range libs {
		if root := rootFor(l, ev.Name); root != "" && hiddenBelow(root, ev.Name) {
			continue // e.g. a title being copied in by Move
		}
		switch {
		case rootFor(l, dir) != "":
			w.queue(l.ID, dir)
		case rootFor(l, ev.Name) != "": // the event is a library root itself
			w.queue(l.ID, ev.Name)
		}
	}
}

func (w *watcher) queue(libraryID int64, dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending[libraryID] == nil {
		w.pending[libraryID] = map[string]bool{}
	}
	w.pending[libraryID][dir] = true
	if t := w.timers[libraryID]; t != nil {
		t.Reset(settleDelay)
		return
	}
	w.timers[libraryID] = time.AfterFunc(settleDelay, func() { w.flush(libraryID) })
}

func (w *watcher) flush(libraryID int64) {
	w.mu.Lock()
	dirs := make([]string, 0, len(w.pending[libraryID]))
	for d := range w.pending[libraryID] {
		dirs = append(dirs, d)
	}
	delete(w.pending, libraryID)
	delete(w.timers, libraryID)
	w.mu.Unlock()
	if len(dirs) == 0 {
		return
	}
	if err := w.s.ScanFolders(libraryID, collapse(dirs)); errors.Is(err, ErrRunning) {
		// A scan is busy; try again once it has had time to finish.
		for _, d := range dirs {
			w.queue(libraryID, d)
		}
	}
}

// hiddenBelow says whether a path below root is in (or is) a dot folder,
// which scans skip.
func hiddenBelow(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// collapse drops folders already covered by a parent in the list.
func collapse(dirs []string) []string {
	sort.Strings(dirs)
	var out []string
	for _, d := range dirs {
		if len(out) > 0 {
			last := out[len(out)-1]
			if d == last || strings.HasPrefix(d, last+string(filepath.Separator)) {
				continue
			}
		}
		out = append(out, d)
	}
	return out
}

// videoDir maps a Subs/ folder (or Subs/<episode>/) to the folder holding the
// videos its subtitles belong to, so a new subtitle there rescans them.
func videoDir(dir string) string {
	if isSubsDir(filepath.Base(dir)) {
		return filepath.Dir(dir)
	}
	if parent := filepath.Dir(dir); isSubsDir(filepath.Base(parent)) {
		return filepath.Dir(parent)
	}
	return dir
}
