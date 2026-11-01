package tasks

import (
	"path/filepath"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func TestMaintenanceWindow(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st)
	at := func(h int) time.Time { return time.Date(2026, 1, 1, h, 30, 0, 0, time.Local) }
	cases := []struct {
		start, end string
		hour       int
		want       bool
	}{
		{"2", "5", 1, false}, {"2", "5", 2, true}, {"2", "5", 4, true}, {"2", "5", 5, false},
		{"22", "4", 23, true}, {"22", "4", 3, true}, {"22", "4", 12, false},
		{"3", "3", 15, true}, // same hour = no window
	}
	for _, c := range cases {
		st.SetSettings(map[string]string{store.SettingMaintenanceStart: c.start, store.SettingMaintenanceEnd: c.end})
		if got := s.window(at(c.hour)); got != c.want {
			t.Errorf("window %s-%s at %d:30 = %v, want %v", c.start, c.end, c.hour, got, c.want)
		}
	}
}

func TestWindowEnd(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st)
	day := func(d, h, m int) time.Time { return time.Date(2026, 1, d, h, m, 0, 0, time.Local) }
	cases := []struct {
		start, end string
		now        time.Time
		want       time.Time
		ok         bool
	}{
		{"2", "5", day(1, 2, 0), day(1, 5, 0), true},
		{"2", "5", day(1, 4, 59), day(1, 5, 0), true},
		{"22", "4", day(1, 23, 10), day(2, 4, 0), true},
		{"22", "4", day(2, 1, 0), day(2, 4, 0), true},
		{"3", "3", day(1, 3, 0), time.Time{}, false},
	}
	for _, c := range cases {
		st.SetSettings(map[string]string{store.SettingMaintenanceStart: c.start, store.SettingMaintenanceEnd: c.end})
		got, ok := s.windowEnd(c.now)
		if ok != c.ok || !got.Equal(c.want) {
			t.Errorf("windowEnd %s-%s at %s = %s, %v; want %s, %v", c.start, c.end, c.now, got, ok, c.want, c.ok)
		}
	}
}
