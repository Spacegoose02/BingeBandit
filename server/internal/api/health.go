package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/store"
	"bingebandit/internal/tasks"
)

// Server health: the admin Health page shows each storage folder's drive
// (online, size, free space), the newest server version and recent alerts.
// RunHealth checks the folders every few minutes and the version once a day,
// and sends an admin alert when something changes for the worse (or better).

const (
	healthInterval = 5 * time.Minute
	updateInterval = 24 * time.Hour
	// settingUpdateNotified is the newest version admins were told about.
	// It is internal, so it is not in SettingDefaults.
	settingUpdateNotified = "update_notified_version"
)

type healthState struct {
	started time.Time

	mu      sync.Mutex
	offline map[string]bool // folder path → offline at the last check
	low     map[string]bool // folder path → nearly full at the last check

	releaseMu  sync.Mutex
	release    *serverRelease
	releaseErr string
	releaseAt  time.Time
}

// serverRelease is the newest release on GitHub.
type serverRelease struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	PublishedAt string `json:"publishedAt"`
	Notes       string `json:"notes,omitempty"`
}

// storageFolder is a folder the server reads or writes, with its drive.
type storageFolder struct {
	Label   string `json:"label"`
	Kind    string `json:"kind"` // data | library | recordings | backups | transcode
	Path    string `json:"path"`
	Online  bool   `json:"online"`
	Error   string `json:"error,omitempty"`
	Total   uint64 `json:"total"`
	Free    uint64 `json:"free"`
	Low     bool   `json:"low"`
	library bool   // has items, so an empty folder means a drive that isn't mounted
}

// storageFolders lists the folders to watch: the data folder, every library
// folder, and the recordings, backup and transcode folders when they are
// set somewhere else.
func (s *Server) storageFolders() []*storageFolder {
	out := []*storageFolder{{Label: "Data folder", Kind: "data", Path: s.DataDir}}
	if libs, err := s.Store.Libraries(); err == nil {
		for _, l := range libs {
			for _, p := range l.Paths {
				out = append(out, &storageFolder{Label: l.Name, Kind: "library", Path: filepath.Clean(p), library: l.ItemCount > 0})
			}
		}
	}
	for _, f := range []struct{ key, label, kind string }{
		{store.SettingDVRFolder, "Recordings", "recordings"},
		{store.SettingBackupDir, "Backups", "backups"},
		{store.SettingTranscodeDir, "Transcoding", "transcode"},
	} {
		if dir := strings.TrimSpace(s.Store.Setting(f.key)); dir != "" {
			out = append(out, &storageFolder{Label: f.label, Kind: f.kind, Path: filepath.Clean(dir)})
		}
	}
	return out
}

// check fills in whether f is reachable and its drive's space.
func (f *storageFolder) check(lowPercent int) {
	info, err := os.Stat(f.Path)
	switch {
	case err != nil:
		f.Error = "can't be reached: " + plainPathErr(err)
		return
	case !info.IsDir():
		f.Error = "isn't a folder"
		return
	}
	if f.library {
		// An unmounted drive often leaves its mount point behind, empty.
		d, err := os.Open(f.Path)
		if err != nil {
			f.Error = "can't be read: " + plainPathErr(err)
			return
		}
		names, _ := d.Readdirnames(1)
		d.Close()
		if len(names) == 0 {
			f.Error = "is empty, so its drive may not be mounted"
			return
		}
	}
	f.Online = true
	if total, free, err := diskSpace(f.Path); err == nil && total > 0 {
		f.Total, f.Free = total, free
		f.Low = free*100 < total*uint64(lowPercent)
	}
}

// folderCheckTimeout is how long a folder check may take. A stuck network
// share (NFS or SMB whose server went away) can block file calls for good;
// without a limit the Health page would never load and the offline alert
// would never be sent.
var folderCheckTimeout = 10 * time.Second

// folderCheck is storageFolder.check, replaceable in tests.
var folderCheck = (*storageFolder).check

// stuckChecks holds the paths whose last check never returned, so a stuck
// share doesn't collect another blocked goroutine every few minutes.
var stuckChecks sync.Map

const notResponding = "isn't responding, so its drive or network share may be stuck"

// checkWithTimeout is check, giving up after folderCheckTimeout.
func (f *storageFolder) checkWithTimeout(lowPercent int) {
	if _, stuck := stuckChecks.Load(f.Path); stuck {
		f.Error = notResponding
		return
	}
	res := *f
	done := make(chan struct{})
	go func() {
		defer close(done)
		folderCheck(&res, lowPercent)
	}()
	select {
	case <-done:
		*f = res
	case <-time.After(folderCheckTimeout):
		f.Online, f.Error = false, notResponding
		stuckChecks.Store(f.Path, true)
		go func(path string) { <-done; stuckChecks.Delete(path) }(f.Path)
	}
}

func plainPathErr(err error) string {
	switch {
	case os.IsNotExist(err):
		return "it doesn't exist"
	case os.IsPermission(err):
		return "permission denied"
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func (s *Server) diskLowPercent() int {
	n, err := strconv.Atoi(s.Store.Setting(store.SettingDiskLowPercent))
	if err != nil {
		n = 5
	}
	return min(max(n, 1), 50)
}

// checkFolders checks every storage folder and alerts admins about folders
// that went offline or came back, and drives that became nearly full.
func (s *Server) checkFolders() []*storageFolder {
	folders := s.storageFolders()
	pct := s.diskLowPercent()
	var wg sync.WaitGroup
	for _, f := range folders {
		wg.Add(1)
		go func() { defer wg.Done(); f.checkWithTimeout(pct) }()
	}
	wg.Wait()

	h := &s.health
	h.mu.Lock()
	first := h.offline == nil
	if first {
		h.offline, h.low = map[string]bool{}, map[string]bool{}
	}
	var gone, back, full []string
	seen := map[string]bool{}
	for _, f := range folders {
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		line := fmt.Sprintf("%s (%s)", f.Label, f.Path)
		if !f.Online && !h.offline[f.Path] {
			gone = append(gone, line+" "+f.Error)
		}
		if f.Online && h.offline[f.Path] {
			back = append(back, line)
		}
		if f.Low && !h.low[f.Path] {
			full = append(full, fmt.Sprintf("%s: %s free of %s", line, humanBytes(f.Free), humanBytes(f.Total)))
		}
	}
	// Folders no longer watched (a library removed) are forgotten.
	h.offline, h.low = map[string]bool{}, map[string]bool{}
	for _, f := range folders {
		h.offline[f.Path] = h.offline[f.Path] || !f.Online
		h.low[f.Path] = h.low[f.Path] || f.Low
	}
	h.mu.Unlock()

	if len(gone) > 0 {
		title := "A storage folder is offline"
		if len(gone) > 1 {
			title = fmt.Sprintf("%d storage folders are offline", len(gone))
		}
		s.sendAlert(alert{Kind: "drive", Title: title, Body: strings.Join(gone, "\n") +
			"\n\nItems on it stay in the library but won't play until it's back.", Link: "/admin/health", Problem: true})
	}
	if len(back) > 0 {
		title := "A storage folder is back online"
		if len(back) > 1 {
			title = fmt.Sprintf("%d storage folders are back online", len(back))
		}
		s.sendAlert(alert{Kind: "drive", Title: title, Body: strings.Join(back, "\n"), Link: "/admin/health"})
	}
	if len(full) > 0 {
		s.sendAlert(alert{Kind: "disk", Title: "A drive is nearly full", Body: strings.Join(full, "\n") +
			fmt.Sprintf("\n\nThat's under %d%% free.", pct), Link: "/admin/health", Problem: true})
	}
	return folders
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}

// RunHealth watches the storage folders and checks for server updates until
// ctx ends.
func (s *Server) RunHealth(ctx context.Context) {
	// Give drives and the network a moment after boot.
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Minute):
	}
	t := time.NewTicker(healthInterval)
	defer t.Stop()
	var updates time.Time
	for {
		s.checkFolders()
		if time.Since(updates) >= updateInterval && s.Store.Setting(store.SettingUpdateCheck) != "false" {
			updates = time.Now()
			s.checkUpdate(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// checkUpdate looks for a newer server version and tells admins about it
// once per version.
func (s *Server) checkUpdate(ctx context.Context) {
	rel, err := s.latestRelease(ctx, true)
	if err != nil || rel == nil || !s.isNewer(rel.Version) {
		return
	}
	if s.Store.Setting(settingUpdateNotified) == rel.Version {
		return
	}
	s.Store.SetSettings(map[string]string{settingUpdateNotified: rel.Version})
	body := fmt.Sprintf("BingeBandit %s is out (this server runs %s).", rel.Version, s.Version)
	if notes := firstLines(rel.Notes, 6); notes != "" {
		body += "\n\n" + notes
	}
	body += "\n\n" + rel.URL
	s.sendAlert(alert{Kind: "update", Title: "BingeBandit " + rel.Version + " is available", Body: body, Link: "/admin/health"})
}

func firstLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	out := strings.Join(lines, "\n")
	if len(out) > 600 {
		out = strings.ToValidUTF8(out[:600], "") + "…"
	}
	return out
}

// isNewer reports whether version is newer than the running one. Builds
// without a version number ("dev") are never told to update.
func (s *Server) isNewer(version string) bool {
	cur := strings.TrimPrefix(s.Version, "v")
	if cur == "" || cur[0] < '0' || cur[0] > '9' {
		return false
	}
	return versionCode(version) > versionCode(cur)
}

// latestRelease returns the newest server release, fetched at most every
// hour unless refresh is set.
func (s *Server) latestRelease(ctx context.Context, refresh bool) (*serverRelease, error) {
	h := &s.health
	h.releaseMu.Lock()
	defer h.releaseMu.Unlock()
	if !refresh && time.Since(h.releaseAt) < time.Hour && !h.releaseAt.IsZero() {
		if h.releaseErr != "" {
			return h.release, fmt.Errorf("%s", h.releaseErr)
		}
		return h.release, nil
	}
	if s.UpdateRepo == "" {
		return nil, nil
	}
	rel, err := fetchServerRelease(ctx, s.UpdateRepo)
	h.releaseAt = time.Now()
	h.releaseErr = ""
	if err != nil {
		h.releaseErr = err.Error()
		h.releaseAt = time.Now().Add(-55 * time.Minute) // try again in 5 minutes
		slog.Warn("update check", "err", err)
		return h.release, err
	}
	h.release = rel
	return rel, nil
}

// cachedRelease is the last release fetched, without asking GitHub.
func (s *Server) cachedRelease() *serverRelease {
	s.health.releaseMu.Lock()
	defer s.health.releaseMu.Unlock()
	return s.health.release
}

var releasesURL = "https://api.github.com/repos/"

func fetchServerRelease(ctx context.Context, repo string) (*serverRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL+repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "BingeBandit")
	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no releases yet
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var rel struct {
		TagName     string `json:"tag_name"`
		HTMLURL     string `json:"html_url"`
		Body        string `json:"body"`
		PublishedAt string `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	notes := rel.Body
	if len(notes) > 4000 {
		notes = strings.ToValidUTF8(notes[:4000], "") + "…"
	}
	return &serverRelease{Version: strings.TrimPrefix(rel.TagName, "v"), URL: rel.HTMLURL, PublishedAt: rel.PublishedAt, Notes: notes}, nil
}

// TaskDone is the scheduler's hook: a failed backup is an alert.
func (s *Server) TaskDone(t *tasks.Task, result string, err error) {
	if t.ID != "backup_database" || err == nil {
		return
	}
	s.sendAlert(alert{Kind: "backup", Title: "The database backup failed", Body: err.Error(), Link: "/admin/tasks", Problem: true})
}

// alertEvent turns server events into admin alerts.
func (s *Server) alertEvent(event string, data any) {
	if event != "recording.failed" {
		return
	}
	r, ok := data.(*store.Recording)
	if !ok {
		return
	}
	body := r.ChannelName
	if r.Username != "" {
		body += " · for " + r.Username
	}
	if r.Error != "" {
		body += "\n" + r.Error
	}
	s.sendAlert(alert{Kind: "recording", Title: "Recording failed: " + r.Title, Body: body, Link: "/recordings", Problem: true})
}

// adminHealth is the admin Health page.
func (s *Server) adminHealth(w http.ResponseWriter, r *http.Request) {
	folders := s.storageFolders()
	pct := s.diskLowPercent()
	var wg sync.WaitGroup
	for _, f := range folders {
		wg.Add(1)
		go func() { defer wg.Done(); f.checkWithTimeout(pct) }()
	}
	var rel *serverRelease
	var relErr error
	if s.Store.Setting(store.SettingUpdateCheck) != "false" || r.URL.Query().Get("refresh") == "1" {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		rel, relErr = s.latestRelease(ctx, r.URL.Query().Get("refresh") == "1")
		cancel()
	}
	wg.Wait()

	update := map[string]any{"current": s.Version, "checking": s.Store.Setting(store.SettingUpdateCheck) != "false"}
	if rel != nil {
		update["latest"] = rel
		update["available"] = s.isNewer(rel.Version)
	}
	if relErr != nil {
		update["error"] = relErr.Error()
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	var dbSize int64
	for _, suffix := range []string{"", "-wal"} {
		if info, err := os.Stat(filepath.Join(s.DataDir, "bingebandit.db"+suffix)); err == nil {
			dbSize += info.Size()
		}
	}
	var taskList []tasks.Status
	if s.Tasks != nil {
		taskList = s.Tasks.List()
	}
	streams, transcodes := 0, 0
	if s.Activity != nil {
		streams = len(s.Activity.List())
	}
	if s.Transcoder != nil {
		transcodes = len(s.Transcoder.List())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"update":     update,
		"folders":    folders,
		"lowPercent": pct,
		"system": map[string]any{
			"version": s.Version, "os": runtime.GOOS + "/" + runtime.GOARCH, "started": s.health.started.Unix(),
			"cpus": runtime.NumCPU(), "memory": mem.Sys, "goroutines": runtime.NumGoroutine(), "database": dbSize,
			"streams": streams, "transcodes": transcodes,
		},
		"tasks":    taskList,
		"alerts":   s.alerts.recent(),
		"channels": alertChannelsFrom(s.Store.Setting).names(),
	})
}

// names lists the channels that are set up.
func (c alertChannels) names() []string {
	out := []string{}
	if len(c.EmailTo) > 0 {
		out = append(out, "email")
	}
	if c.DiscordURL != "" {
		out = append(out, "discord")
	}
	if c.NtfyURL != "" {
		out = append(out, "ntfy")
	}
	return out
}
