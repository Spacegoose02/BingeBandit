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
