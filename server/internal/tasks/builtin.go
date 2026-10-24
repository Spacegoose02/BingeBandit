package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bingebandit/internal/library"
	"bingebandit/internal/markers"
	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

type Deps struct {
	Store   *store.Store
	Scanner *library.Scanner
	Markers *markers.Detector
	DataDir string
	FFprobe string
}

// Builtin returns the standard task list.
func Builtin(d Deps) []*Task {
	return []*Task{
		{
			ID: "scan_libraries", Name: "Scan libraries",
			Description:    "Look for new, changed and removed files in every library.",
			DefaultEnabled: true, DefaultHours: 6,
			Run: d.scanLibraries,
		},
		{
			ID: "refresh_metadata", Name: "Refresh metadata",
			Description: "Re-download titles, artwork, ratings and cast from TMDB for everything (locked fields are kept).",
			Maintenance: true, DefaultEnabled: true, DefaultHours: 24 * 7,
			Run: d.refreshMetadata,
		},
		{
			ID: "backup_database", Name: "Back up database",
			Description: "Copy the database (users, watch history, edits) to the backup folder and keep the newest copies.",
			Maintenance: true, DefaultEnabled: true, DefaultHours: 24,
			Run: d.backupDatabase,
		},
		{
			ID: "optimize_database", Name: "Optimize database",
			Description: "Update query statistics and reclaim unused space.",
			Maintenance: true, DefaultEnabled: true, DefaultHours: 24 * 7,
			Run: d.optimizeDatabase,
		},
		{
			ID: "clean_cache", Name: "Clean up cache",
			Description: "Delete cached artwork and subtitles that nothing uses any more.",
			Maintenance: true, DefaultEnabled: true, DefaultHours: 24,
			Run: d.cleanCache,
		},
		{
			ID: "detect_markers", Name: "Detect intros and credits",
			Description: "Compare the audio of episodes in each season to find intros and end credits for the Skip buttons.",
			Maintenance: true, DefaultEnabled: true, DefaultHours: 24,
			Run: d.Markers.Pending,
		},
		{
			ID: "analyze_media", Name: "Analyze media",
			Description: "Retry codec and stream analysis for files that couldn't be read during a scan.",
			Maintenance: true, DefaultEnabled: true, DefaultHours: 24,
			Run: d.analyzeMedia,
		},
	}
}

func (d Deps) eachLibrary(fn func(id int64) error) (string, error) {
	libs, err := d.Store.Libraries()
	if err != nil {
		return "", err
	}
	var failed []string
	for _, l := range libs {
		if err := fn(l.ID); err != nil && !errors.Is(err, library.ErrRunning) {
			failed = append(failed, l.Name+": "+err.Error())
		}
	}
	if len(failed) > 0 {
		return "", errors.New(strings.Join(failed, "; "))
	}
	return fmt.Sprintf("%d libraries done", len(libs)), nil
}

func (d Deps) scanLibraries(ctx context.Context) (string, error) {
	return d.eachLibrary(d.Scanner.ScanWait)
}

func (d Deps) refreshMetadata(ctx context.Context) (string, error) {
	if d.Store.Setting(store.SettingTMDBKey) == "" {
		return "skipped: no TMDB key", nil
	}
	return d.eachLibrary(d.Scanner.RefreshMetadataWait)
}

// BackupDir is where database backups go: the setting, else <data>/backups.
func (d Deps) BackupDir() string {
	if dir := strings.TrimSpace(d.Store.Setting(store.SettingBackupDir)); dir != "" {
		return dir
	}
	return filepath.Join(d.DataDir, "backups")
}

func (d Deps) backupDatabase(ctx context.Context) (string, error) {
	dir := d.BackupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("backup folder: %w", err)
	}
	name := "bingebandit-" + time.Now().Format("20060102-150405") + ".db"
	path := filepath.Join(dir, name)
	if err := d.Store.Backup(path); err != nil {
		os.Remove(path)
		return "", err
	}
	keep, _ := strconv.Atoi(d.Store.Setting(store.SettingBackupKeep))
	if keep < 1 {
		keep = 1
	}
	// Names sort by time; delete all but the newest `keep`.
	matches, _ := filepath.Glob(filepath.Join(dir, "bingebandit-*.db"))
	sort.Strings(matches)
	removed := 0
	for len(matches) > keep {
		if os.Remove(matches[0]) == nil {
			removed++
		}
		matches = matches[1:]
	}
	st, _ := os.Stat(path)
	size := int64(0)
	if st != nil {
		size = st.Size()
	}
	return fmt.Sprintf("saved %s (%.1f MB), removed %d old", name, float64(size)/(1<<20), removed), nil
}

func (d Deps) optimizeDatabase(ctx context.Context) (string, error) {
	if err := d.Store.Optimize(); err != nil {
		return "", err
	}
	return "optimized", nil
}

func (d Deps) cleanCache(ctx context.Context) (string, error) {
	refs, err := d.Store.ReferencedImages()
	if err != nil {
		return "", err
	}
	removed, freed := 0, int64(0)
	drop := func(path string, info os.FileInfo) {
		if os.Remove(path) == nil {
			removed++
			freed += info.Size()
		}
	}

	// TMDB image cache: <data>/cache/images/tmdb/<size>/<file>
	filepath.Walk(filepath.Join(d.DataDir, "cache", "images", "tmdb"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !refs["tmdb:/"+info.Name()] {
			drop(p, info)
		}
		return nil
	})
	// Uploaded/downloaded artwork that was replaced.
	filepath.Walk(filepath.Join(d.DataDir, "artwork"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !refs["local:/"+info.Name()] {
			drop(p, info)
		}
		return nil
	})
	// Subtitles: <fileID>-<stream>-<size>.vtt (embedded) or
	// <fileID>-<stream>-<size>-<namehash>.vtt (sidecar); stale when the
	// video changed or is gone.
	sizes, err := d.Store.FileSizes()
	if err != nil {
		return "", err
	}
	filepath.Walk(filepath.Join(d.DataDir, "cache", "subtitles"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		parts := strings.Split(strings.TrimSuffix(info.Name(), ".vtt"), "-")
		if len(parts) != 3 && len(parts) != 4 {
			drop(p, info)
			return nil
		}
		id, _ := strconv.ParseInt(parts[0], 10, 64)
		index, _ := strconv.Atoi(parts[1])
		size, _ := strconv.ParseInt(parts[2], 10, 64)
		mediaSize, exists := sizes[id]
		// Sidecar caches carry the subtitle file's size, not the video's.
		if !exists || (index < store.ExternalIndexBase && mediaSize != size) {
			drop(p, info)
		}
		return nil
	})
	// Audio fingerprints: <fileID>-<size>-<part>-<seconds>.fp
	filepath.Walk(filepath.Join(d.DataDir, "cache", "fingerprints"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		parts := strings.Split(info.Name(), "-")
		id, _ := strconv.ParseInt(parts[0], 10, 64)
		size := int64(-1)
		if len(parts) > 1 {
			size, _ = strconv.ParseInt(parts[1], 10, 64)
		}
		if mediaSize, ok := sizes[id]; !ok || mediaSize != size {
			drop(p, info)
		}
		return nil
	})
	return fmt.Sprintf("removed %d files, freed %.1f MB", removed, float64(freed)/(1<<20)), nil
}

func (d Deps) analyzeMedia(ctx context.Context) (string, error) {
	files, err := d.Store.UnprobedFiles(500)
	if err != nil {
		return "", err
	}
	ok := 0
	for _, f := range files {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err := media.Probe(ctx, d.FFprobe, f); err != nil {
			continue
		}
		if err := d.Store.UpdateProbe(f); err == nil {
			ok++
		}
	}
	return fmt.Sprintf("analyzed %d of %d unreadable files", ok, len(files)), nil
}
