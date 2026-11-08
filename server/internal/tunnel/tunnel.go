// Package tunnel gives a server remote access without port forwarding: it
// runs Cloudflare's cloudflared (a quick tunnel at a random
// *.trycloudflare.com address, or the admin's own named tunnel) or turns on
// Tailscale Funnel, pointed at a listener on 127.0.0.1 that serves only
// tunnel traffic. Requests through that listener are marked as coming from
// outside (see api.Server.TunnelHandler), so a tunnel never makes someone
// on the internet look like they're at home.
package tunnel

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

// Modes, the remote_tunnel setting.
const (
	Off        = ""
	Quick      = "cloudflare-quick" // a new trycloudflare.com address each start; for trying it out
	Cloudflare = "cloudflare"       // the admin's named tunnel, by its token; the address they gave it
	Tailscale  = "tailscale"        // Tailscale Funnel on the machine's own tailnet name
)

// Status is what the admin page shows.
type Status struct {
	Mode  string `json:"mode"`
	State string `json:"state"` // off | starting | running | failed
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
	Since int64  `json:"since,omitempty"`
	Port  int    `json:"port,omitempty"` // the 127.0.0.1 port a named tunnel's public hostname points at
}

// Manager runs the tunnel the settings ask for.
type Manager struct {
	st      *store.Store
	dataDir string
	handler http.Handler
	// OnURL is called when the tunnel's public address changes ("" when it
	// stops), so the server can use it as its public URL.
	OnURL func(string)

	applyMu sync.Mutex // one Apply at a time
	mu      sync.Mutex
	status  Status
	cancel  context.CancelFunc
	done    chan struct{}
	ln      net.Listener
	srv     *http.Server
	applied string
}

// downloadBase is where cloudflared is fetched from when it isn't installed;
// a variable so tests can point it at a fake.
var downloadBase = "https://github.com/cloudflare/cloudflared/releases/latest/download/"

// New makes a manager that serves handler to the tunnel.
func New(st *store.Store, dataDir string, handler http.Handler) *Manager {
	return &Manager{st: st, dataDir: dataDir, handler: handler, status: Status{State: "off"}}
}

// Status is the tunnel's current state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// URL is the tunnel's public address while it runs, else "".
func (m *Manager) URL() string {
	s := m.Status()
	if s.State != "running" {
		return ""
	}
	return s.URL
}

func (m *Manager) set(f func(*Status)) {
	m.mu.Lock()
	before := m.status.URL
	if m.status.State != "running" {
		before = ""
	}
	f(&m.status)
	after := m.status.URL
	if m.status.State != "running" {
		after = ""
	}
	cb := m.OnURL
	m.mu.Unlock()
	if before != after && cb != nil {
		cb(after)
	}
}

// key is what the running tunnel depends on; a change restarts it.
func (m *Manager) key() string {
	return strings.Join([]string{m.st.Setting(store.SettingTunnel), m.st.Setting(store.SettingTunnelToken),
		m.st.Setting(store.SettingTunnelHostname), m.st.Setting(store.SettingTunnelPort)}, "\x00")
}

// Run starts the tunnel the settings ask for and stops it when ctx ends.
func (m *Manager) Run(ctx context.Context) {
	m.Apply(ctx)
	<-ctx.Done()
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	m.stop()
}

// Apply (re)starts or stops the tunnel after the settings changed.
func (m *Manager) Apply(ctx context.Context) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	key := m.key()
	m.mu.Lock()
	same := key == m.applied && m.cancel != nil
	m.mu.Unlock()
	if same {
		return
	}
	m.stop()
	m.mu.Lock()
	m.applied = key
	m.mu.Unlock()
	mode := m.st.Setting(store.SettingTunnel)
	if mode == Off {
		m.set(func(s *Status) { *s = Status{State: "off"} })
		return
	}
	port, _ := strconv.Atoi(m.st.Setting(store.SettingTunnelPort))
	if port <= 0 || port > 65535 {
		port = 8485
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		m.set(func(s *Status) {
			*s = Status{Mode: mode, State: "failed", Error: fmt.Sprintf("can't listen on 127.0.0.1:%d for the tunnel: %v", port, err)}
		})
		return
	}
	srv := &http.Server{Handler: m.handler, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m.mu.Lock()
	m.ln, m.srv, m.cancel, m.done = ln, srv, cancel, done
	m.mu.Unlock()
	m.set(func(s *Status) { *s = Status{Mode: mode, State: "starting", Port: port, Since: time.Now().Unix()} })
	go func() {
		defer close(done)
		m.supervise(rctx, mode, port)
	}()
}

func (m *Manager) stop() {
	m.mu.Lock()
	cancel, done, srv := m.cancel, m.done, m.srv
	m.cancel, m.done, m.srv, m.ln = nil, nil, nil, nil
	m.applied = ""
	m.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	if srv != nil {
		sctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		srv.Shutdown(sctx)
		c()
	}
}

// supervise keeps the tunnel up, restarting it with growing pauses when it exits.
func (m *Manager) supervise(ctx context.Context, mode string, port int) {
	wait := 5 * time.Second
	for ctx.Err() == nil {
		started := time.Now()
		var err error
		switch mode {
		case Quick, Cloudflare:
			err = m.cloudflared(ctx, mode, port)
		case Tailscale:
			err = m.tailscale(ctx, port)
		default:
			err = fmt.Errorf("unknown tunnel %q", mode)
		}
		if ctx.Err() != nil {
			break
		}
		msg := "the tunnel stopped"
		if err != nil {
			msg = err.Error()
		}
		log.Printf("tunnel: %s", msg)
		m.set(func(s *Status) { s.State, s.Error = "failed", msg })
		if time.Since(started) > 5*time.Minute {
			wait = 5 * time.Second
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
		wait = min(wait*2, 5*time.Minute)
		m.set(func(s *Status) { s.State = "starting" })
	}
	if mode == Tailscale {
		m.tailscaleOff(port)
	}
	m.set(func(s *Status) { s.State, s.URL = "off", "" })
}

var quickURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// cloudflared runs one cloudflared process until it exits or ctx ends.
func (m *Manager) cloudflared(ctx context.Context, mode string, port int) error {
	bin, err := m.cloudflaredBinary(ctx)
	if err != nil {
		return err
	}
	local := "http://127.0.0.1:" + strconv.Itoa(port)
	var args []string
	if mode == Quick {
		args = []string{"tunnel", "--no-autoupdate", "--url", local}
	} else {
		token := strings.TrimSpace(m.st.Setting(store.SettingTunnelToken))
		if token == "" {
			return errors.New("paste the tunnel's token from the Cloudflare dashboard (Networks → Tunnels → your tunnel → Install connector)")
		}
		args = []string{"tunnel", "--no-autoupdate", "run", "--token", token}
		host := strings.TrimRight(strings.TrimSpace(m.st.Setting(store.SettingTunnelHostname)), "/")
		if host != "" && !strings.Contains(host, "://") {
			host = "https://" + host
		}
		m.set(func(s *Status) { s.URL = host })
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	media.HideWindow(cmd)
	out, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	cmd.Stdout = cmd.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start cloudflared: %w", err)
	}
	sc := bufio.NewScanner(out)
	var last string
	for sc.Scan() {
		line := sc.Text()
		if mode == Quick {
			if u := quickURL.FindString(line); u != "" {
				m.set(func(s *Status) { s.URL = u })
			}
		}
		if strings.Contains(line, "Registered tunnel connection") {
			m.set(func(s *Status) { s.State, s.Error, s.Since = "running", "", time.Now().Unix() })
		}
		if strings.Contains(line, " ERR ") {
			last = line
		}
	}
	err = cmd.Wait()
	if last != "" {
		return fmt.Errorf("cloudflared: %s", last[strings.Index(last, " ERR ")+5:])
	}
	if err != nil {
		return fmt.Errorf("cloudflared: %w", err)
	}
	return nil
}

// cloudflaredBinary finds cloudflared (BB_CLOUDFLARED, the PATH, or a copy
// in the data folder) and otherwise downloads Cloudflare's own build.
func (m *Manager) cloudflaredBinary(ctx context.Context) (string, error) {
	if p := os.Getenv("BB_CLOUDFLARED"); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p, nil
	}
	name := "cloudflared"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dest := filepath.Join(m.dataDir, "bin", name)
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		return dest, nil
	}
	asset, err := cloudflaredAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	log.Printf("tunnel: downloading cloudflared (%s)", asset)
	if err := download(ctx, downloadBase+asset, dest, strings.HasSuffix(asset, ".tgz")); err != nil {
		return "", fmt.Errorf("download cloudflared: %w (or install it yourself and restart BingeBandit)", err)
	}
	return dest, nil
}

// cloudflaredAsset is the name of Cloudflare's release file for a platform.
func cloudflaredAsset(goos, arch string) (string, error) {
	switch goos {
	case "linux":
		switch arch {
		case "amd64", "arm64", "386":
			return "cloudflared-linux-" + arch, nil
		case "arm":
			return "cloudflared-linux-arm", nil
		}
	case "windows":
		if arch == "amd64" || arch == "386" {
			return "cloudflared-windows-" + arch + ".exe", nil
		}
	case "darwin":
		if arch == "amd64" || arch == "arm64" {
			return "cloudflared-darwin-" + arch + ".tgz", nil
		}
	}
	return "", fmt.Errorf("there's no cloudflared download for %s/%s; install it yourself and restart BingeBandit", goos, arch)
}

func download(ctx context.Context, url, dest string, tgz bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", url, resp.Status)
	}
	var body io.Reader = resp.Body
	if tgz {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err != nil {
				return fmt.Errorf("no cloudflared in the archive: %w", err)
			}
			if filepath.Base(h.Name) == "cloudflared" && h.Typeflag == tar.TypeReg {
				body = tr
				break
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(body, 300<<20)); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// tailscale turns on Funnel for the tunnel port and watches that it stays on.
func (m *Manager) tailscale(ctx context.Context, port int) error {
	bin, err := exec.LookPath("tailscale")
	if err != nil {
		return errors.New("Tailscale isn't installed on this machine (install it, sign in, and allow Funnel in the tailnet's access controls)")
	}
	run := func(args ...string) ([]byte, error) {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(c, bin, args...)
		media.HideWindow(cmd)
		return cmd.CombinedOutput()
	}
	if out, err := run("funnel", "--bg", "--yes", strconv.Itoa(port)); err != nil {
		return fmt.Errorf("tailscale funnel: %s", strings.TrimSpace(string(out)))
	}
	out, err := run("status", "--json")
	if err != nil {
		return fmt.Errorf("tailscale status: %s", strings.TrimSpace(string(out)))
	}
	var st struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &st); err != nil || st.Self.DNSName == "" {
		return errors.New("tailscale status didn't say this machine's name; is it signed in?")
	}
	u := "https://" + strings.TrimSuffix(st.Self.DNSName, ".")
	m.set(func(s *Status) { s.URL, s.State, s.Error, s.Since = u, "running", "", time.Now().Unix() })
	// Funnel runs inside tailscaled; check now and then that it's still up.
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if out, err := run("funnel", "status"); err != nil || !strings.Contains(string(out), strconv.Itoa(port)) {
				return errors.New("Funnel was turned off outside BingeBandit")
			}
		}
	}
}

func (m *Manager) tailscaleOff(port int) {
	bin, err := exec.LookPath("tailscale")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "funnel", "--https=443", "off")
	media.HideWindow(cmd)
	cmd.Run()
}
