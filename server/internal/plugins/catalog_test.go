package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func TestCatalogInstall(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	exe, _ := os.Executable()
	t.Setenv("BB_TEST_PLUGIN", "1")

	// A zip like the release workflow makes: helper/plugin.json (+ a file).
	makeZip := func(files map[string]string) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for name, body := range files {
			f, _ := zw.Create(name)
			f.Write([]byte(body))
		}
		zw.Close()
		return buf.Bytes()
	}
	manifest := func(id, version string) string {
		b, _ := json.Marshal(map[string]any{"id": id, "name": "Helper", "version": version, "command": []string{exe}})
		return string(b)
	}
	zips := map[string][]byte{
		"v1":       makeZip(map[string]string{"helper/plugin.json": manifest("helper", "1.0.0"), "helper/README": "one"}),
		"v2":       makeZip(map[string]string{"helper/plugin.json": manifest("helper", "1.1.0"), "helper/README": "two"}),
		"evil":     makeZip(map[string]string{"../evil/plugin.json": manifest("helper", "1.0.0")}),
		"wrong-id": makeZip(map[string]string{"other/plugin.json": manifest("other", "1.0.0")}),
	}
	version := "v1"
	sum := func(name string) string { s := sha256.Sum256(zips[name]); return hex.EncodeToString(s[:]) }
	var srv *httptest.Server
	entry := func(id, zipName, checksum string) CatalogEntry {
		v := map[string]string{"v1": "1.0.0", "v2": "1.1.0"}[zipName]
		return CatalogEntry{ID: id, Name: "Helper", Version: v, Downloads: map[string]Download{
			Platform(): {URL: srv.URL + "/" + zipName + ".zip", SHA256: checksum},
		}}
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog.json":
			json.NewEncoder(w).Encode(Catalog{Plugins: []CatalogEntry{
				entry("helper", version, sum(version)),
				entry("bad-sum", "v1", strings.Repeat("0", 64)),
				entry("evil", "evil", sum("evil")),
				entry("wrong-id", "wrong-id", sum("wrong-id")),
				{ID: "elsewhere", Name: "Other OS", Version: "1.0.0", Downloads: map[string]Download{"plan9-mips": {}}},
			}})
		default:
			b, ok := zips[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".zip")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write(b)
		}
	}))
	defer srv.Close()
	cat := srv.URL + "/catalog.json"

	installDir := filepath.Join(dir, "plugins")
	h := NewHost(st, []string{filepath.Join(dir, "bundled"), installDir}, filepath.Join(dir, "data"), "http://127.0.0.1:1")
	h.InstallDir = installDir
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); h.Wait(10 * time.Second) }()
	go h.Start(ctx)

	list, errs := h.Catalogs([]string{cat, srv.URL + "/missing.json"}, false)
	if len(list) != 5 || len(errs) != 1 || list[0].Installed != "" || !list[0].Available || list[4].Available {
		t.Fatalf("catalogue: %+v %+v", list, errs)
	}

	for id, want := range map[string]string{"bad-sum": "checksum", "evil": "unsafe path", "wrong-id": "not \"wrong-id\"", "elsewhere": "no download"} {
		if err := h.Install(cat, id); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("install %s: %v, want an error about %q", id, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Fatal("the zip escaped the plugins folder")
	}

	if err := h.Install(cat, "helper"); err != nil {
		t.Fatal(err)
	}
	s, err := h.Get("helper")
	if err != nil || s.State != StateStopped || !s.Installed || s.Enabled {
		t.Fatalf("after install: %+v %v", s, err)
	}
	st.SetPluginEnabled("helper", true)
	h.Sync()
	waitFor(t, func() bool { s, _ := h.Get("helper"); return s.State == StateRunning })

	// A newer version in the catalogue is offered and replaces the running one.
	version = "v2"
	list, _ = h.Catalogs([]string{cat}, true)
	if list[0].Installed != "installed" || list[0].Current != "1.0.0" || !list[0].Update {
		t.Fatalf("update not offered: %+v", list[0])
	}
	if err := h.Install(cat, "helper"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(installDir, "helper", "README")); string(b) != "two" {
		t.Fatalf("README after update: %q", b)
	}
	waitFor(t, func() bool { s, _ := h.Get("helper"); return s.State == StateRunning && s.Enabled })
	list, _ = h.Catalogs([]string{cat}, false)
	if list[0].Update || list[0].Current != "1.1.0" {
		t.Fatalf("after update: %+v", list[0])
	}
	if entries, _ := os.ReadDir(installDir); len(entries) != 1 {
		t.Fatalf("leftovers in the plugins folder: %v", entries)
	}

	if err := h.Uninstall("helper"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "helper")); !os.IsNotExist(err) {
		t.Fatal("folder still there")
	}
	if _, err := h.Get("helper"); err == nil {
		t.Fatal("still listed")
	}

	// Bundled plugins can't be uninstalled.
	os.MkdirAll(filepath.Join(dir, "bundled", "helper"), 0o755)
	os.WriteFile(filepath.Join(dir, "bundled", "helper", "plugin.json"), []byte(manifest("helper", "1.0.0")), 0o644)
	h.Sync()
	if err := h.Uninstall("helper"); err == nil {
		t.Fatal("uninstalled a bundled plugin")
	}
	list, _ = h.Catalogs([]string{cat}, false)
	if list[0].Installed != "bundled" || !list[0].Update {
		t.Fatalf("bundled: %+v", list[0])
	}
}

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v1.2.0", "1.1.9", true}, {"1.2", "1.2.0", false}, {"0.7.0", "", true}, {"0.7.0", "dev", true}, {"dev", "0.1.0", false}, {"1.0.0", "1.0.1", false}} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
