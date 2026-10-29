package library

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"bingebandit/internal/store"
)

// DeletePlan is what deleting a movie, show, season or episode from the
// server removes from disk, for the admin to check before saying yes.
type DeletePlan struct {
	Library string   `json:"library"`
	Paths   []string `json:"paths"` // folders and files that go
	Files   int      `json:"files"`
	Size    int64    `json:"size"`
	// Also names other episodes that play a multi-episode file going with it.
	Also []string `json:"also,omitempty"`
	// Problem says why it can't be deleted, if it can't.
	Problem string `json:"problem,omitempty"`
}

var ErrCannotDelete = errors.New("cannot delete")

// PlanDelete works out what deleting a title removes. A movie or show goes
// with its own folder when nothing else shares it, and so does a season
// with its season folder; otherwise just its files go, with the subtitles
// and extras named after them. Nothing outside the library's folders, and
// never a library folder itself, is ever on the list.
func (s *Scanner) PlanDelete(it *store.Item) (*DeletePlan, error) {
	it, err := s.st.Item(0, it.ID)
	if err != nil {
		return nil, err
	}
	lib, err := s.st.Library(it.LibraryID)
	if err != nil {
		return nil, err
	}
	files, err := s.st.ItemPaths(it.ID)
	if err != nil {
		return nil, err
	}
	plan := &DeletePlan{Library: lib.Name, Paths: []string{}}
	if len(files) == 0 {
		plan.Problem = "It has no files on the server."
		return plan, nil
	}
	sep := string(filepath.Separator)
	others := func(dir string) (bool, error) { return s.st.OthersUnderTree(it.ID, dir+sep) }

	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			plan.Paths = append(plan.Paths, p)
		}
	}
	// A season's own folder, when all its episodes are in one below the show's.
	seasonDir := ""
	if it.Kind == "season" {
		for i, f := range files {
			root := rootFor(lib, f)
			rel, _ := filepath.Rel(root, f)
			d := ""
			if parts := strings.Split(rel, sep); root != "" && len(parts) >= 3 {
				d = filepath.Join(root, parts[0], parts[1])
			}
			if i > 0 && d != seasonDir {
				d = ""
			}
			seasonDir = d
			if d == "" {
				break
			}
		}
		if seasonDir != "" {
			if o, err := others(seasonDir); err != nil {
				return nil, err
			} else if o {
				seasonDir = ""
			}
		}
	}
	for _, f := range files {
		root := rootFor(lib, f)
		if root == "" {
			plan.Problem = "Some of its files aren't in this library's folders any more. Scan the library first."
			plan.Paths = append(plan.Paths, f)
			continue
		}
		if !reachable(root) {
			plan.Problem = fmt.Sprintf("%s can't be reached right now.", root)
			continue
		}
		rel, _ := filepath.Rel(root, f)
		parts := strings.Split(rel, sep)
		unit := ""
		switch {
		case it.Kind == "movie" && len(parts) > 1:
			if o, err := others(filepath.Dir(f)); err != nil {
				return nil, err
			} else if !o {
				unit = filepath.Dir(f)
			}
		case it.Kind == "show" && len(parts) > 1:
			top := filepath.Join(root, parts[0])
			if o, err := others(top); err != nil {
				return nil, err
			} else if !o {
				unit = top
			}
		case seasonDir != "":
			unit = seasonDir
		}
		if unit != "" {
			add(unit)
			continue
		}
		add(f)
		for _, side := range namedAfter(f) {
			add(side)
		}
	}
	sort.Strings(plan.Paths)
	// A file inside a folder that goes anyway isn't listed twice.
	kept := plan.Paths[:0]
	for _, p := range plan.Paths {
		if n := len(kept); n > 0 && strings.HasPrefix(p, kept[n-1]+sep) {
			continue
		}
		kept = append(kept, p)
	}
	plan.Paths = kept
	for _, p := range plan.Paths {
		filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				plan.Files++
				if info, err := d.Info(); err == nil {
					plan.Size += info.Size()
				}
			}
			return nil
		})
	}
	if it.Kind == "episode" {
		ids, err := s.st.SharedFileEpisodes(it.ID)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if ep, err := s.st.Item(0, id); err == nil {
				plan.Also = append(plan.Also, episodeName(ep))
			}
		}
	}
	return plan, nil
}

func episodeName(ep *store.Item) string {
	if ep.SeasonNumber != nil && ep.EpisodeNumber != nil {
		return fmt.Sprintf("S%02dE%02d %s", *ep.SeasonNumber, *ep.EpisodeNumber, ep.Title)
	}
	return ep.Title
}

// Delete removes a title's files from disk for good, as PlanDelete lists
// them, and then the title from the library. The library counts as busy
// meanwhile, so no scan runs into it half gone. When a file can't be
// removed, what did go is still dropped from the library and the error
// says what stayed.
func (s *Scanner) Delete(it *store.Item) error {
	plan, err := s.PlanDelete(it)
	if err != nil {
		return err
	}
	if plan.Problem != "" {
		return fmt.Errorf("%w: %s", ErrCannotDelete, plan.Problem)
	}
	release, err := s.reserve(it.LibraryID)
	if err != nil {
		return err
	}
	lib, err := s.st.Library(it.LibraryID)
	if err != nil {
		release(err)
		return err
	}
	var gone []string
	var failed error
	for _, p := range plan.Paths {
		if err := os.RemoveAll(p); err != nil {
			failed = fmt.Errorf("remove %s: %w", p, err)
			break
		}
		gone = append(gone, p)
		removeEmptyParents(lib, filepath.Dir(p))
	}
	err = s.st.ForgetPaths(it.LibraryID, gone)
	if failed != nil {
		err = failed
	}
	release(err)
	return err
}

// removeEmptyParents takes away folders left empty (a season folder after
// its last episode), up to but never including the library folder.
func removeEmptyParents(lib *store.Library, dir string) {
	root := rootFor(lib, dir)
	for root != "" && dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		if os.Remove(dir) != nil { // fails unless empty
			return
		}
		dir = filepath.Dir(dir)
	}
}
