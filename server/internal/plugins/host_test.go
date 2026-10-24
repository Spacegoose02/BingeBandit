package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bingebandit/internal/store"
	"bingebandit/pkg/bbplugin"
)

// TestMain doubles as a managed plugin: the host test starts this test
// binary with BB_TEST_PLUGIN set.
func TestMain(m *testing.M) {
	if os.Getenv("BB_TEST_PLUGIN") != "" {
		p := &bbplugin.Plugin{Manifest: bbplugin.Manifest{ID: "helper", Name: "Helper", Events: []string{"*"}}}
		p.OnEvent = func(ctx context.Context, e bbplugin.Event) {
			os.WriteFile(filepath.Join(p.DataDir, "event"), []byte(e.Event), 0o644)
		}
		if err := p.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestManagedPlugin(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	pluginDir := filepath.Join(dir, "plugins", "helper")
	os.MkdirAll(pluginDir, 0o755)
	exe, _ := os.Executable()
	pj, _ := json.Marshal(map[string]any{"id": "helper", "name": "Helper", "command": []string{exe}})
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), pj, 0o644)
	t.Setenv("BB_TEST_PLUGIN", "1")

	h := NewHost(st, []string{filepath.Join(dir, "plugins")}, filepath.Join(dir, "data"), "http://127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	go h.Start(ctx)

	// Found on disk, but off until an admin turns it on.
	waitFor(t, func() bool { s, err := h.Get("helper"); return err == nil && s.State == StateStopped })
	st.SetPluginEnabled("helper", true)
	h.Sync()
	waitFor(t, func() bool { s, _ := h.Get("helper"); return s.State == StateRunning })

	h.Emit("recording.completed", map[string]any{"id": 1})
	waitFor(t, func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "data", "helper", "event"))
		return string(b) == "recording.completed"
	})

	if err := h.Remove("helper"); err == nil {
		t.Error("a plugin in a plugins folder can't be removed, only turned off")
	}
	st.SetPluginEnabled("helper", false)
	h.Sync()
	waitFor(t, func() bool { s, _ := h.Get("helper"); return s.State == StateStopped })

	cancel()
	h.Wait(10 * time.Second)
}

func TestParseManifest(t *testing.T) {
	for _, bad := range []string{`{}`, `{"id":"Has Spaces"}`, `{"id":"ok","settings":[{"label":"no key"}]}`, `nope`} {
		if _, err := parseManifest([]byte(bad)); err == nil {
			t.Errorf("%s should be refused", bad)
		}
	}
	m, err := parseManifest([]byte(`{"id":"hue-sync","events":["playback.start"],"settings":[{"key":"ip","default":"1.2.3.4"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "hue-sync" || m.Settings[0].Type != "text" || !m.Wants("playback.start") || m.Wants("playback.stop") {
		t.Errorf("manifest: %+v", m)
	}
	if v := m.Values(map[string]string{"extra": "x"}); v["ip"] != "1.2.3.4" || v["extra"] != "x" {
		t.Errorf("values: %v", v)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("timed out")
}
