package library

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"bingebandit/internal/store"
)

// Location is where a movie or show sits on disk: the folders (or, for a
// movie loose in a library folder, the files) that would move with it.
type Location struct {
	LibraryID int64    `json:"libraryId"`
	Library   string   `json:"library"`
	Root      string   `json:"root"`  // the library folder it's in
	Paths     []string `json:"paths"` // what moves
	// Problem says why it can't be moved, if it can't.
	Problem string `json:"problem,omitempty"`
}

// MoveTarget is a library folder a title could move to.
type MoveTarget struct {
	LibraryID int64  `json:"libraryId"`
	Library   string `json:"library"`
	Path      string `json:"path"`
}

var ErrCannotMove = errors.New("cannot move")

func cannot(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrCannotMove}, a...)...)
}

// Locate finds a movie or show's folder. A show is its folder below the
// library folder; a movie is its own folder, or its file (with subtitles and
// extras named after it) when it shares a folder with other movies.
func (s *Scanner) Locate(it *store.Item) (*Location, error) {
	if it.Kind != "movie" && it.Kind != "show" {
		return nil, cannot("only movies and shows can move")
	}
	it, err := s.st.Item(0, it.ID) // its library may have changed since it was loaded
	if err != nil {
		return nil, err
	}
	lib, err := s.st.Library(it.LibraryID)
	if err != nil {
		return nil, err
	}
	files, err := s.st.TitlePaths(it.ID)
	if err != nil {
		return nil, err
	}
	loc := &Location{LibraryID: lib.ID, Library: lib.Name, Paths: []string{}}
	seen := map[string]bool{}
	sep := string(filepath.Separator)
	for _, f := range files {
		root := rootFor(lib, f)
		if root == "" {
			loc.Paths = append(loc.Paths, f)
			loc.Problem = "Some of its files aren't in this library's folders any more. Scan the library first."
			continue
		}
		if loc.Root != "" && loc.Root != root {
			loc.Problem = "Its files are spread over more than one of the library's folders."
		}
		loc.Root = root
		rel, _ := filepath.Rel(root, f)
		parts := strings.Split(rel, sep)
		unit := f
		switch {
		case it.Kind == "show" && len(parts) > 1:
			unit = filepath.Join(root, parts[0])
		case it.Kind == "show":
			loc.Problem = "Its episodes sit straight in the library folder, not in a folder of their own, so there's no show folder to move."
		case len(parts) > 1:
			dir := filepath.Dir(f)
			others, err := s.st.OthersUnder(it.ID, dir+sep)
			if err != nil {
				return nil, err
			}
			if !others {
				unit = dir
			}
		}
		if !seen[unit] {
			seen[unit] = true
			loc.Paths = append(loc.Paths, unit)
		}
	}
	if len(files) == 0 {
		loc.Problem = "It has no files."
	}
	if it.Kind == "show" && len(loc.Paths) > 1 && loc.Problem == "" {
		loc.Problem = "Its episodes are in more than one folder. Scan the library so each folder becomes its own show, then move them one at a time."
	}
	return loc, nil
}

// MoveTargets lists the folders of the other libraries of the same kind
// (and the library's own other folders) a title could move to.
func (s *Scanner) MoveTargets(loc *Location) ([]MoveTarget, error) {
	from, err := s.st.Library(loc.LibraryID)
	if err != nil {
		return nil, err
	}
	libs, err := s.st.Libraries()
	if err != nil {
		return nil, err
	}
	out := []MoveTarget{}
	for _, l := range libs {
		if l.Kind != from.Kind {
			continue
		}
		for _, p := range l.Paths {
			if p = filepath.Clean(p); p != loc.Root {
				out = append(out, MoveTarget{LibraryID: l.ID, Library: l.Name, Path: p})
			}
		}
	}
	return out, nil
}

type moveStep struct{ from, to string }

// Move moves a movie or show's folder into another library folder and the
// title along with it, keeping its watch state and everything else. A
// rename is instant and done before Move returns; between drives the files
// are copied in the background (copying is true) and the title switches
// libraries once they're all there. Both libraries count as busy meanwhile,
// so no scan sees the title half moved.
func (s *Scanner) Move(it *store.Item, toLibrary int64, toRoot string) (copying bool, err error) {
	loc, err := s.Locate(it)
	if err != nil {
		return false, err
	}
	if loc.Problem != "" {
		return false, cannot("%s", loc.Problem)
	}
	from, err := s.st.Library(loc.LibraryID)
	if err != nil {
		return false, err
	}
	to, err := s.st.Library(toLibrary)
	if err != nil {
		return false, err
	}
	if to.Kind != from.Kind {
		return false, cannot("%s is a %s library", to.Name, strings.TrimSuffix(to.Kind, "s"))
	}
	toRoot = filepath.Clean(toRoot)
	if !inPaths(to, toRoot) {
		return false, cannot("%s isn't one of %s's folders", toRoot, to.Name)
	}
	if toRoot == loc.Root {
		return false, cannot("it's already in %s", toRoot)
	}
	if !reachable(toRoot) {
		return false, cannot("%s can't be reached", toRoot)
	}

	var steps []moveStep
	for _, p := range loc.Paths {
		steps = append(steps, moveStep{p, filepath.Join(toRoot, filepath.Base(p))})
		if it.Kind == "movie" && !isDir(p) {
			// A loose movie file takes its subtitles and extras along.
			for _, side := range namedAfter(p) {
				steps = append(steps, moveStep{side, filepath.Join(toRoot, filepath.Base(side))})
			}
		}
	}
	for _, st := range steps {
		if _, err := os.Lstat(st.to); err == nil {
			return false, cannot("%s already exists", st.to)
		}
	}

	oldKey, newKey := "", ""
	if it.Kind == "show" {
		name := filepath.Base(loc.Paths[0])
		title, year, _ := parseTitleYear(name)
		newKey = plainShowKey(title, year)
		if k, ok := folderKeys(to.Paths, name)[name]; ok {
			newKey = k
		}
		if oldKey, err = s.st.MatchKey(it.ID); err != nil {
			return false, err
		}
	} else if newKey, err = s.st.MatchKey(it.ID); err != nil {
		return false, err
	} else {
		oldKey = newKey // a movie's key comes from its name, which stays
	}
	if to.ID != from.ID {
		if taken, err := s.st.KeyTaken(to.ID, newKey); err != nil {
			return false, err
		} else if taken {
			return false, cannot("%s already has %s", to.Name, it.Title)
		}
	} else {
		oldKey, newKey = "", ""
	}

	release, err := s.reserve(from.ID, to.ID)
	if err != nil {
		return false, err
	}
	var done []moveStep
	for _, st := range steps {
		err = os.Rename(st.from, st.to)
		if err != nil {
			break
		}
		done = append(done, st)
	}
	if err == nil {
		err = s.finishMove(it, to.ID, oldKey, newKey, loc.Paths, steps)
		if err != nil {
			undo(done)
		}
		release(err)
		return false, err
	}
	if !crossDevice(err) {
		undo(done)
		release(err)
		return false, fmt.Errorf("move %s: %w", steps[len(done)].from, err)
	}
	// Another drive: copy what's left, then the title follows.
	go func() {
		err := s.copySteps(steps[len(done):], from.ID, to.ID)
		if err == nil {
			err = s.finishMove(it, to.ID, oldKey, newKey, loc.Paths, steps)
		}
		if err != nil {
			undo(done)
			log.Printf("move %q to %s: %v", it.Title, to.Name, err)
		} else {
			for _, st := range steps[len(done):] {
				if err := os.RemoveAll(st.from); err != nil {
					log.Printf("move %q: remove %s: %v", it.Title, st.from, err)
				}
			}
			log.Printf("moved %q to %s", it.Title, to.Name)
		}
		release(err)
	}()
	return true, nil
}

// finishMove points the title's files at their new places.
func (s *Scanner) finishMove(it *store.Item, toLibrary int64, oldKey, newKey string, units []string, steps []moveStep) error {
	paths := map[string]string{}
	for _, st := range steps {
		for _, u := range units {
			if st.from == u {
				paths[st.from] = st.to
			}
		}
	}
	return s.st.MoveTitle(it.ID, toLibrary, oldKey, newKey, paths)
}

func undo(done []moveStep) {
	for i := len(done) - 1; i >= 0; i-- {
		if err := os.Rename(done[i].to, done[i].from); err != nil {
			log.Printf("move: put %s back: %v", done[i].from, err)
		}
	}
}

// reserve marks libraries busy, as a running scan would, until release.
func (s *Scanner) reserve(ids ...int64) (release func(error), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if cur := s.status[id]; cur != nil && cur.Running {
			return nil, ErrRunning
		}
	}
	var held []*Status
	for _, id := range ids {
		st := &Status{LibraryID: id, Running: true, Phase: "moving", Started: time.Now().Unix()}
		s.status[id] = st
		held = append(held, st)
	}
	return func(err error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, st := range held {
			st.Running, st.Phase, st.Finished = false, "done", time.Now().Unix()
			if err != nil {
				st.Error = err.Error()
			}
		}
	}, nil
}

// copySteps copies each folder or file next to its destination under a
// hidden name (scans and folder watching skip those), then renames it into
// place, which is instant on the same drive.
func (s *Scanner) copySteps(steps []moveStep, libs ...int64) error {
	total := 0
	for _, st := range steps {
		filepath.WalkDir(st.from, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				total++
			}
			return nil
		})
	}
	n := 0
	progress := func() {
		n++
		s.mu.Lock()
		for _, id := range libs {
			if st := s.status[id]; st != nil {
				st.Found, st.Processed = total, n
			}
		}
		s.mu.Unlock()
	}
	var placed []string
	for _, st := range steps {
		tmp := filepath.Join(filepath.Dir(st.to), ".bingebandit-moving-"+filepath.Base(st.to))
		os.RemoveAll(tmp)
		err := copyTree(st.from, tmp, progress)
		if err == nil {
			err = os.Rename(tmp, st.to)
		}
		if err != nil {
			os.RemoveAll(tmp)
			for _, p := range placed {
				os.RemoveAll(p)
			}
			return fmt.Errorf("copy %s: %w", st.from, err)
		}
		placed = append(placed, st.to)
	}
	return nil
}

// copyTree copies a file or folder, keeping modification times so the
// next scan sees the files as unchanged.
func copyTree(from, to string, progress func()) error {
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm()|0o700)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := copyFile(p, dst, info); err != nil {
			return err
		}
		progress()
		return nil
	})
}

func copyFile(from, to string, info fs.FileInfo) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(to, info.ModTime(), info.ModTime())
}

// crossDevice says whether a rename failed because the two places are on
// different drives (or different Docker mounts).
func crossDevice(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	if runtime.GOOS == "windows" {
		return errno == 17 // ERROR_NOT_SAME_DEVICE
	}
	return errno == syscall.EXDEV
}

// namedAfter lists the files next to a video that are named after it
// ("Movie.en.srt", "Movie-trailer.mp4", "Movie.nfo").
func namedAfter(video string) []string {
	dir := filepath.Dir(video)
	base := filepath.Base(video)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || n == base || isVideo(n) && !isExtraFile(n) {
			continue // another movie's file
		}
		if strings.HasPrefix(n, stem+".") || strings.HasPrefix(n, stem+"-") {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func inPaths(lib *store.Library, p string) bool {
	for _, r := range lib.Paths {
		if filepath.Clean(r) == p {
			return true
		}
	}
	return false
}
