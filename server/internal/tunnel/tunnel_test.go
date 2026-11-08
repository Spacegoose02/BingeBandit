package tunnel

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func TestQuickTunnel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake cloudflared is a shell script")
	}
	dir := t.TempDir()
	// A fake cloudflared: says where the quick tunnel is, that it connected, then waits.
	fake := filepath.Join(dir, "cloudflared")
	os.WriteFile(fake, []byte("#!/bin/sh\necho \"INF |  https://brave-otter-tunnel.trycloudflare.com  |\" >&2\necho \"INF Registered tunnel connection connIndex=0\" >&2\nexec sleep 60\n"), 0o755)
	t.Setenv("BB_CLOUDFLARED", fake)

	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	port := 18000 + int(time.Now().UnixNano()%1000)
	st.SetSettings(map[string]string{store.SettingTunnel: Quick, store.SettingTunnelPort: strconv.Itoa(port)})
	m := New(st, dir, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "through the tunnel") }))
	urls := make(chan string, 4)
	m.OnURL = func(u string) { urls <- u }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()

	select {
	case u := <-urls:
		if u != "https://brave-otter-tunnel.trycloudflare.com" {
			t.Fatalf("url: %q", u)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no url; status %+v", m.Status())
	}
	if s := m.Status(); s.State != "running" || s.Port != port {
		t.Fatalf("status: %+v", s)
	}
	// The tunnel's own listener serves the handler.
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "through the tunnel" {
		t.Fatalf("body: %q", b)
	}

	// Turning it off stops it and clears the address.
	st.SetSettings(map[string]string{store.SettingTunnel: Off})
	m.Apply(ctx)
	if u := <-urls; u != "" {
		t.Fatalf("url after off: %q", u)
	}
	if s := m.Status(); s.State != "off" {
		t.Fatalf("status after off: %+v", s)
	}
	cancel()
	<-done
}

func TestCloudflaredAsset(t *testing.T) {
	for _, c := range []struct{ goos, arch, want string }{
		{"linux", "amd64", "cloudflared-linux-amd64"},
		{"linux", "arm", "cloudflared-linux-arm"},
		{"windows", "amd64", "cloudflared-windows-amd64.exe"},
		{"darwin", "arm64", "cloudflared-darwin-arm64.tgz"},
	} {
		if got, err := cloudflaredAsset(c.goos, c.arch); err != nil || got != c.want {
			t.Errorf("%s/%s: %q %v", c.goos, c.arch, got, err)
		}
	}
	if _, err := cloudflaredAsset("plan9", "amd64"); err == nil {
		t.Error("plan9 should have no download")
	}
}
