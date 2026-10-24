// Package tasks runs scheduled background jobs (scans, backups, cleanup)
// on an interval, optionally only inside the nightly maintenance window.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"bingebandit/internal/store"
)

type Task struct {
	ID          string
	Name        string
	Description string
	// Maintenance tasks only start inside the maintenance window.
	Maintenance    bool
	DefaultEnabled bool
	DefaultHours   int
	// Run does the work and returns a one-line summary for the admin UI.
	Run func(ctx context.Context) (string, error)
}

type Status struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Maintenance bool   `json:"maintenance"`
	Running     bool   `json:"running"`
	NextRun     int64  `json:"nextRun"` // unix; 0 when disabled
	store.TaskState
}

type Scheduler struct {
	st    *store.Store
	tasks []*Task

	mu      sync.Mutex
	running map[string]bool
}

func New(st *store.Store, tasks ...*Task) *Scheduler {
	return &Scheduler{st: st, tasks: tasks, running: map[string]bool{}}
}

var ErrRunning = errors.New("this task is already running")

func (s *Scheduler) find(id string) *Task {
	for _, t := range s.tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// state returns the saved config, falling back to the task's defaults.
func (s *Scheduler) state(t *Task) store.TaskState {
	st, ok, err := s.st.TaskState(t.ID)
	if err != nil || !ok {
		st = store.TaskState{ID: t.ID, Enabled: t.DefaultEnabled, IntervalHours: t.DefaultHours}
	}
	return st
}

// window reports whether now is inside the maintenance window. Equal start
// and end hours mean "any time".
func (s *Scheduler) window(now time.Time) bool {
	start, _ := strconv.Atoi(s.st.Setting(store.SettingMaintenanceStart))
	end, _ := strconv.Atoi(s.st.Setting(store.SettingMaintenanceEnd))
	h := now.Hour()
	switch {
	case start == end:
		return true
	case start < end:
		return h >= start && h < end
	default: // wraps midnight, e.g. 22 → 4
		return h >= start || h < end
	}
}

// Start checks every minute for due tasks until ctx ends.
func (s *Scheduler) Start(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, task := range s.tasks {
				st := s.state(task)
				if !st.Enabled || st.IntervalHours <= 0 {
					continue
				}
				due := now.Sub(time.Unix(st.LastRun, 0)) >= time.Duration(st.IntervalHours)*time.Hour
				if due && (!task.Maintenance || s.window(now)) {
					s.launch(ctx, task)
				}
			}
		}
	}
}

// RunNow starts a task immediately, outside its schedule and window.
func (s *Scheduler) RunNow(ctx context.Context, id string) error {
	t := s.find(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if !s.launch(ctx, t) {
		return ErrRunning
	}
	return nil
}

func (s *Scheduler) launch(ctx context.Context, t *Task) bool {
	s.mu.Lock()
	if s.running[t.ID] {
		s.mu.Unlock()
		return false
	}
	s.running[t.ID] = true
	s.mu.Unlock()

	go func() {
		started := time.Now()
		// Make sure a row exists so the run can be recorded.
		if _, ok, _ := s.st.TaskState(t.ID); !ok {
			s.st.SetTaskConfig(t.ID, t.DefaultEnabled, t.DefaultHours)
		}
		log.Printf("task %q: starting", t.Name)
		result, err := t.Run(ctx)
		if err != nil {
			log.Printf("task %q failed after %s: %v", t.Name, time.Since(started).Round(time.Second), err)
		} else {
			log.Printf("task %q: %s (%s)", t.Name, result, time.Since(started).Round(time.Second))
		}
		s.st.RecordTaskRun(t.ID, started, result, err)
		s.mu.Lock()
		delete(s.running, t.ID)
		s.mu.Unlock()
	}()
	return true
}

func (s *Scheduler) Configure(id string, enabled bool, hours int) error {
	if s.find(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if hours < 1 || hours > 24*30 {
		return errors.New("interval must be between 1 hour and 30 days")
	}
	return s.st.SetTaskConfig(id, enabled, hours)
}

func (s *Scheduler) List() []Status {
	s.mu.Lock()
	running := map[string]bool{}
	for k, v := range s.running {
		running[k] = v
	}
	s.mu.Unlock()
	out := make([]Status, 0, len(s.tasks))
	for _, t := range s.tasks {
		st := s.state(t)
		next := int64(0)
		if st.Enabled {
			next = max(st.LastRun+int64(st.IntervalHours)*3600, time.Now().Unix())
		}
		out = append(out, Status{
			ID: t.ID, Name: t.Name, Description: t.Description, Maintenance: t.Maintenance,
			Running: running[t.ID], NextRun: next, TaskState: st,
		})
	}
	return out
}
