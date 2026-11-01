package api

import (
	"net/http"
	"sync"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

// chapterCacheSize bounds the files whose chapters are kept in memory.
// Reading them again costs one quick ffprobe of the file's header.
const chapterCacheSize = 2000

// chapterCache keeps each file's chapters, keyed by file id and checked
// against the file's size and mtime, so a replaced file is read again.
type chapterCache struct {
	mu      sync.Mutex
	entries map[int64]chapterEntry
}

type chapterEntry struct {
	size, mtime int64
	chapters    []media.Chapter
}

func (c *chapterCache) get(f *store.MediaFile) ([]media.Chapter, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[f.ID]
	if !ok || e.size != f.Size || e.mtime != f.MTime {
		return nil, false
	}
	return e.chapters, true
}

func (c *chapterCache) put(f *store.MediaFile, chapters []media.Chapter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[int64]chapterEntry{}
	}
	if len(c.entries) >= chapterCacheSize {
		// Dropping everything is crude, but chapters are cheap to read
		// again and a household rarely plays this many files between
		// restarts.
		c.entries = map[int64]chapterEntry{}
	}
	c.entries[f.ID] = chapterEntry{size: f.Size, mtime: f.MTime, chapters: chapters}
}

// fileChapters lists the chapter marks in a file, for the players' chapter
// menu and the marks on their seek bars. Times are seconds in the original
// file, the same clock as progress and markers. A file without chapters
// answers an empty list.
func (s *Server) fileChapters(w http.ResponseWriter, r *http.Request) {
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	chapters, ok := s.chapters.get(mf)
	if !ok {
		if s.FFprobe == "" {
			writeJSON(w, http.StatusOK, map[string]any{"chapters": []media.Chapter{}})
			return
		}
		var err error
		chapters, err = media.Chapters(r.Context(), s.FFprobe, mf.Path)
		if err != nil {
			// An unreadable file (a drive that's offline) isn't cached,
			// so the chapters show up once it's back.
			writeJSON(w, http.StatusOK, map[string]any{"chapters": []media.Chapter{}})
			return
		}
		s.chapters.put(mf, chapters)
	}
	writeJSON(w, http.StatusOK, map[string]any{"chapters": chapters})
}
