package livetv

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/store"
)

// Recorder runs DVR recordings. A recording is just another viewer of the
// channel's shared relay, so recording and watching the same channel use
// one provider connection.
type Recorder struct {
	st      *store.Store
	live    *Manager
	dataDir string

	mu     sync.Mutex
	active map[int64]context.CancelFunc

	// OnEvent, if set, is told about recording.started / completed / failed.
	OnEvent func(event string, r *store.Recording)
}

func (rec *Recorder) event(name string, id int64) {
	if rec.OnEvent == nil {
		return
	}
	if r, err := rec.st.Recording(id); err == nil {
		r.Path = "" // don't leak server paths to third parties
		rec.OnEvent(name, r)
	}
}

func NewRecorder(st *store.Store, live *Manager, dataDir string) *Recorder {
	st.ResetInterrupted()
	return &Recorder{st: st, live: live, dataDir: dataDir, active: map[int64]context.CancelFunc{}}
}

// Folder is where recordings are written.
func (rec *Recorder) Folder() string {
	if d := strings.TrimSpace(rec.st.Setting(store.SettingDVRFolder)); d != "" {
		return d
	}
	return filepath.Join(rec.dataDir, "recordings")
}

// Padding returns the configured head and tail padding.
func (rec *Recorder) Padding() (before, after time.Duration) {
	b, _ := strconv.Atoi(rec.st.Setting(store.SettingDVRPadBefore))
	a, _ := strconv.Atoi(rec.st.Setting(store.SettingDVRPadAfter))
	return time.Duration(max(b, 0)) * time.Minute, time.Duration(max(a, 0)) * time.Minute
}

// Run starts due recordings and expands series rules until ctx ends.
func (rec *Recorder) Run(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	lastRules := time.Time{}
	for {
		now := time.Now()
		if now.Sub(lastRules) > 5*time.Minute {
			rec.ApplyRules()
			lastRules = now
		}
		rec.st.MissRecordings(now.Unix())
		if due, err := rec.st.DueRecordings(now.Unix()); err == nil {
			for _, r := range due {
				rec.start(ctx, r)
			}
		}
		select {
		case <-ctx.Done():
			rec.mu.Lock()
			for _, cancel := range rec.active {
				cancel()
			}
			rec.mu.Unlock()
			return
		case <-t.C:
		}
	}
}

// ApplyRules schedules the next three days of airings for every active series rule.
func (rec *Recorder) ApplyRules() {
	rules, err := rec.st.Rules(0, "active")
	if err != nil {
		return
	}
	now := time.Now()
	before, after := rec.Padding()
	for _, rule := range rules {
		airings, err := rec.st.UpcomingAirings(rule.Title, rule.ChannelID, now.Add(-time.Minute).Unix(), now.Add(72*time.Hour).Unix())
		if err != nil {
			continue
		}
		for _, a := range airings {
			rec.st.CreateRecording(fromProgram(&a.Program, a.ChannelID, a.ChannelName, rule.UserID, rule.ID, "scheduled", before, after))
		}
	}
}

func fromProgram(p *store.GuideProgram, channelID int64, channelName string, userID, ruleID int64, status string, before, after time.Duration) *store.Recording {
	return &store.Recording{
		UserID: userID, RuleID: ruleID, ChannelID: channelID, ChannelName: channelName, EPGID: p.EPGID,
		Title: p.Title, Subtitle: p.Subtitle, Description: p.Description, Episode: p.Episode,
		ProgramStart: p.Start, ProgramStop: p.Stop,
		StartAt: p.Start - int64(before.Seconds()), StopAt: p.Stop + int64(after.Seconds()), Status: status,
	}
}

// Schedule creates a recording for a guide program (and optionally a series rule).
func (rec *Recorder) Schedule(p *store.GuideProgram, ch *store.Channel, userID int64, series, needsApproval bool) (*store.Recording, error) {
	status := "scheduled"
	if needsApproval {
		status = "pending"
	}
	var ruleID int64
	if series {
		ruleStatus := "active"
		if needsApproval {
			ruleStatus = "pending"
		}
		rule, err := rec.st.CreateRule(&store.RecordingRule{UserID: userID, ChannelID: ch.ID, Title: p.Title, Status: ruleStatus})
		if err != nil {
			return nil, err
		}
		ruleID = rule.ID
	}
	before, after := rec.Padding()
	r, err := rec.st.CreateRecording(fromProgram(p, ch.ID, ch.Name, userID, ruleID, status, before, after))
	if err == nil && series && !needsApproval {
		go rec.ApplyRules() // pick up the rest of the series right away
	}
	return r, err
}

// Cancel stops a running recording (the partial file is kept).
func (rec *Recorder) Cancel(id int64) {
	rec.mu.Lock()
	cancel := rec.active[id]
	rec.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (rec *Recorder) start(parent context.Context, r *store.Recording) {
	rec.mu.Lock()
	if _, running := rec.active[r.ID]; running {
		rec.mu.Unlock()
		return
	}
	ctx, cancel := context.WithDeadline(parent, time.Unix(r.StopAt, 0))
	rec.active[r.ID] = cancel
	rec.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			rec.mu.Lock()
			delete(rec.active, r.ID)
			rec.mu.Unlock()
		}()
		rec.record(ctx, r)
	}()
}

// record writes the channel to disk until the stop time, rejoining the relay
// if the upstream drops (the relay itself already fails over between streams).
func (rec *Recorder) record(ctx context.Context, r *store.Recording) {
	path := filepath.Join(rec.Folder(), rec.relPath(r))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		rec.st.SetRecordingState(r.ID, "failed", "", 0, err.Error())
		return
	}
	f, err := os.Create(path)
	if err != nil {
		rec.st.SetRecordingState(r.ID, "failed", "", 0, err.Error())
		return
	}
	rec.st.SetRecordingState(r.ID, "recording", path, 0, "")
	rec.event("recording.started", r.ID)
	log.Printf("dvr: recording %q on %s → %s", r.Title, r.ChannelName, path)

	var written int64
	var lastErr string
	user := &store.User{ID: r.UserID, Username: "DVR"}
	for ctx.Err() == nil {
		ch, err := rec.st.Channel(0, r.ChannelID)
		if err != nil {
			lastErr = "the channel was deleted"
			break
		}
		v, err := rec.live.Join(ch, user, "dvr", "ts")
		if err != nil {
			lastErr = err.Error()
			sleepCtx(ctx, 5*time.Second)
			continue
		}
	copyLoop:
		for {
			select {
			case <-ctx.Done():
				break copyLoop
			case <-v.Done():
				lastErr = "the stream dropped; reconnecting"
				break copyLoop
			case chunk := <-v.Chunks():
				n, err := f.Write(chunk)
				written += int64(n)
				if err != nil {
					lastErr = "disk write failed: " + err.Error()
					rec.live.Leave(v.ID)
					f.Close()
					rec.st.SetRecordingState(r.ID, "failed", path, written, lastErr)
					return
				}
			}
		}
		rec.live.Leave(v.ID)
		if ctx.Err() == nil {
			sleepCtx(ctx, 2*time.Second)
		}
	}
	f.Close()

	status, msg := "completed", ""
	switch {
	case written == 0:
		status, msg = "failed", firstNonEmpty(lastErr, "no data was received")
	case time.Now().Unix() < r.StopAt-30:
		status, msg = "completed", "stopped early" // cancelled by a user or the server
	}
	rec.st.SetRecordingState(r.ID, status, path, written, msg)
	rec.event("recording."+status, r.ID)
	log.Printf("dvr: %q finished (%s, %.1f MB)", r.Title, status, float64(written)/(1<<20))
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var (
	reUnsafe   = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)
	reOnscreen = regexp.MustCompile(`(?i)^s(\d{1,3})\s*e(\d{1,4})`)
	reXMLTVNS  = regexp.MustCompile(`^\s*(\d+)\s*\.\s*(\d+)`)
)

func safeName(s string) string {
	s = strings.TrimSpace(reUnsafe.ReplaceAllString(s, " "))
	s = strings.Trim(s, ". ")
	if s == "" {
		s = "Recording"
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// seasonEpisode reads "S01E02" or XMLTV's zero-based "0.1." numbering.
func seasonEpisode(ep string) (int, int, bool) {
	if m := reOnscreen.FindStringSubmatch(ep); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, true
	}
	if m := reXMLTVNS.FindStringSubmatch(ep); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s + 1, e + 1, true
	}
	return 0, 0, false
}

// relPath names a recording. "library" naming makes files a TV library can
// scan (Show/Season 01/Show - S01E02 - Title.ts, or dated when the guide has
// no episode numbers); "folder" puts everything in one folder.
func (rec *Recorder) relPath(r *store.Recording) string {
	show := safeName(r.Title)
	when := time.Unix(r.ProgramStart, 0).Format("2006-01-02 15.04")
	if rec.st.Setting(store.SettingDVRNaming) == "folder" {
		return fmt.Sprintf("%s - %s.ts", show, when)
	}
	if s, e, ok := seasonEpisode(r.Episode); ok {
		name := fmt.Sprintf("%s - S%02dE%02d", show, s, e)
		if r.Subtitle != "" {
			name += " - " + safeName(r.Subtitle)
		}
		return filepath.Join(show, fmt.Sprintf("Season %02d", s), name+".ts")
	}
	return filepath.Join(show, fmt.Sprintf("%s - %s.ts", show, when))
}
