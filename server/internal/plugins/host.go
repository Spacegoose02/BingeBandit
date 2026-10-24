package plugins

import (
	"bufio"
	"bytes"
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
	"runtime"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

// Plugin states shown in Admin → Plugins.
const (
	StateStopped  = "stopped"
	StateStarting = "starting"
	StateRunning  = "running"
	StateError    = "error"
	StateMissing  = "missing" // a managed plugin whose folder is gone
)

// Host starts, watches and talks to plugins.
type Host struct {
	Store *store.Store
	// Dirs hold managed plugins, one folder each with a plugin.json. Later
	// folders win when two hold the same id (so <data>/plugins can override
	// a bundled plugin).
	Dirs []string
	// DataDir gets one folder per plugin for its own files.
	DataDir string
	// ServerURL is how managed plugins reach BingeBandit (loopback).
	ServerURL string
	// Env is added to managed plugins' environment (BINGEBANDIT_FFMPEG…).
	Env []string
	// ServerName and Version go into event payloads.
	ServerName func() string

	client *http.Client
	ctx    context.Context
	wg     sync.WaitGroup

	mu    sync.Mutex
	found map[string]*bundle   // managed plugins on disk
	inst  map[string]*instance // started plugins
}

// bundle is a managed plugin's folder.
type bundle struct {
	dir      string
	manifest *Manifest
}

type instance struct {
	id       string
	kind     string
	base     string // where the plugin listens, no trailing slash
	key      string
	manifest *Manifest
	state    string
	err      string
	cancel   context.CancelFunc
	done     chan struct{}
}

// Status is a plugin as Admin → Plugins shows it.
type Status struct {
	*store.Plugin
	Manifest *Manifest `json:"manifest"`
	State    string    `json:"state"`
	Error    string    `json:"error,omitempty"`
	// SecretsSet lists password settings that have a value; their values are never sent back.
	SecretsSet []string          `json:"secretsSet"`
	Values     map[string]string `json:"values"`
}

func NewHost(st *store.Store, dirs []string, dataDir, serverURL string) *Host {
	return &Host{
		Store: st, Dirs: dirs, DataDir: dataDir, ServerURL: serverURL,
		client: &http.Client{Timeout: 10 * time.Second},
		found:  map[string]*bundle{},
		inst:   map[string]*instance{},
	}
}

// Start finds managed plugins, starts every enabled plugin, and then checks
// on them every minute until ctx ends.
func (h *Host) Start(ctx context.Context) {
	h.mu.Lock()
	h.ctx = ctx
	h.mu.Unlock()
	h.Sync()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Sync()
			h.checkHealth()
		}
	}
}

// Wait blocks until every managed plugin has exited after shutdown, or the timeout passes.
func (h *Host) Wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// discover reads plugin.json from each subfolder of Dirs and records new
// managed plugins (disabled until an admin turns them on).
func (h *Host) discover() {
	found := map[string]*bundle{}
	for _, root := range h.Dirs {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
			if err != nil {
				continue
			}
			m, err := parseManifest(b)
			if err != nil {
				log.Printf("plugins: %s: %v", dir, err)
				continue
			}
			if len(m.Command) == 0 {
				log.Printf("plugins: %s: plugin.json has no command", dir)
				continue
			}
			found[m.ID] = &bundle{dir: dir, manifest: m}
		}
	}
	for id, b := range found {
		raw, _ := json.Marshal(b.manifest)
		if err := h.Store.AddPlugin(&store.Plugin{ID: id, Kind: "managed", Manifest: raw}); err != nil {
			log.Printf("plugins: %s: %v", id, err)
		}
	}
	h.mu.Lock()
	h.found = found
	h.mu.Unlock()
}

// Sync makes the running plugins match the database: enabled ones start,
// disabled or removed ones stop.
func (h *Host) Sync() {
	h.discover()
	rows, err := h.Store.Plugins()
	if err != nil {
		log.Printf("plugins: %v", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctx == nil || h.ctx.Err() != nil {
		return
	}
	want := map[string]bool{}
	for _, p := range rows {
		if !p.Enabled {
			continue
		}
		if p.Kind == "managed" && h.found[p.ID] == nil {
			continue
		}
		want[p.ID] = true
		if in := h.inst[p.ID]; in != nil && in.key == p.Key && (p.Kind != "remote" || in.base == p.URL) {
			continue
		}
		h.stopLocked(p.ID)
		h.startLocked(p)
	}
	for id := range h.inst {
		if !want[id] {
			h.stopLocked(id)
		}
	}
}

// Restart stops a plugin and starts it again (remote plugins are checked again).
func (h *Host) Restart(id string) {
	h.mu.Lock()
	h.stopLocked(id)
	h.mu.Unlock()
	h.Sync()
}

func (h *Host) stopLocked(id string) {
	in := h.inst[id]
	if in == nil {
		return
	}
	delete(h.inst, id)
	in.cancel()
}

func (h *Host) startLocked(p *store.Plugin) {
	ctx, cancel := context.WithCancel(h.ctx)
	in := &instance{id: p.ID, kind: p.Kind, key: p.Key, state: StateStarting, cancel: cancel, done: make(chan struct{})}
	h.inst[p.ID] = in
	if p.Kind == "remote" {
		in.base = strings.TrimRight(p.URL, "/")
		go func() {
			defer close(in.done)
			h.check(in)
		}()
		return
	}
	b := h.found[p.ID]
	in.manifest = b.manifest
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer close(in.done)
		h.supervise(ctx, in, b)
	}()
}

func (h *Host) setState(in *instance, state, msg string) {
	h.mu.Lock()
	in.state, in.err = state, msg
	h.mu.Unlock()
}

// supervise keeps a managed plugin running, restarting it with a growing
// delay (5 s up to 5 minutes) when it exits.
func (h *Host) supervise(ctx context.Context, in *instance, b *bundle) {
	delay := 5 * time.Second
	for {
		started := time.Now()
		err := h.run(ctx, in, b)
		if ctx.Err() != nil {
			h.setState(in, StateStopped, "")
			return
		}
		if time.Since(started) > 2*time.Minute {
			delay = 5 * time.Second
		}
		msg := "exited"
		if err != nil {
			msg = err.Error()
		}
		log.Printf("plugin %s: %s; restarting in %s", in.id, msg, delay)
		h.setState(in, StateError, fmt.Sprintf("%s (restarting in %s)", msg, delay))
		select {
		case <-ctx.Done():
			h.setState(in, StateStopped, "")
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Minute)
	}
}

// run starts the plugin's process and waits for it to exit.
func (h *Host) run(ctx context.Context, in *instance, b *bundle) error {
	addr, err := freeAddr()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(h.DataDir, in.id)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	name := b.manifest.Command[0]
	if !filepath.IsAbs(name) && (strings.ContainsAny(name, `/\`) || fileExists(filepath.Join(b.dir, name)) || fileExists(filepath.Join(b.dir, name+".exe"))) {
		name = filepath.Join(b.dir, name)
	}
	if runtime.GOOS == "windows" && !fileExists(name) && fileExists(name+".exe") {
		name += ".exe"
	}
	cmd := exec.CommandContext(ctx, name, b.manifest.Command[1:]...)
	cmd.Dir = b.dir
	cmd.Env = append(os.Environ(),
		"BINGEBANDIT_URL="+h.ServerURL,
		"BINGEBANDIT_PLUGIN_ID="+in.id,
		"BINGEBANDIT_PLUGIN_KEY="+in.key,
		"BINGEBANDIT_PLUGIN_ADDR="+addr,
		"BINGEBANDIT_PLUGIN_DATA="+dataDir,
	)
	cmd.Env = append(cmd.Env, h.Env...)
	// Ask nicely first; Windows has no SIGTERM, so it's killed there.
	cmd.Cancel = func() error {
		if runtime.GOOS == "windows" {
			return cmd.Process.Kill()
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second
	media.HideWindow(cmd)
	out := &lineLogger{prefix: "plugin " + in.id + ": "}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	h.mu.Lock()
	in.base = "http://" + addr
	h.mu.Unlock()
	// Wait for it to answer before calling it running.
	deadline := time.After(20 * time.Second)
	for ready := false; !ready; {
		select {
		case err := <-exited:
			out.Flush()
			return orExit(err)
		case <-deadline:
			h.setState(in, StateError, "didn't answer on "+addr+" within 20 s")
			ready = true
		case <-time.After(250 * time.Millisecond):
			ready = h.check(in)
		}
	}
	err = <-exited
	out.Flush()
	return orExit(err)
}

func orExit(err error) error {
	if err == nil {
		return errors.New("exited")
	}
	return err
}

// check fetches the manifest, which doubles as the health check, and keeps
// the stored copy current.
func (h *Host) check(in *instance) bool {
	h.mu.Lock()
	base, key := in.base, in.key
	h.mu.Unlock()
	m, raw, err := h.fetchManifest(base, key)
	if err == nil && m.ID != in.id {
		err = fmt.Errorf("the plugin at %s says it is %q, not %q", base, m.ID, in.id)
	}
	if err != nil {
		h.setState(in, StateError, err.Error())
		return false
	}
	h.mu.Lock()
	in.manifest, in.state, in.err = m, StateRunning, ""
	h.mu.Unlock()
	h.Store.SetPluginManifest(in.id, raw)
	return true
}

func (h *Host) checkHealth() {
	h.mu.Lock()
	list := make([]*instance, 0, len(h.inst))
	for _, in := range h.inst {
		if in.state == StateRunning || in.state == StateError {
			list = append(list, in)
		}
	}
	h.mu.Unlock()
	for _, in := range list {
		go h.check(in)
	}
}

var errUnpaired = errors.New("the plugin didn't accept BingeBandit's key; remove it and add it again to pair")

func (h *Host) fetchManifest(base, key string) (*Manifest, json.RawMessage, error) {
	req, err := http.NewRequest(http.MethodGet, base+"/bingebandit/manifest", nil)
	if err != nil {
		return nil, nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, nil, errUnpaired
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("manifest: %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, err
	}
	m, err := parseManifest(raw)
	if err != nil {
		return nil, nil, err
	}
	norm, _ := json.Marshal(m)
	return m, norm, nil
}

// AddRemote pairs with a plugin running as its own service. serverURL is
// BingeBandit's address as the plugin can reach it. Adding the same plugin
// again (a new URL, or after it lost its data) pairs it again with a new key.
func (h *Host) AddRemote(base, serverURL string) (*store.Plugin, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	m, raw, err := h.fetchManifest(base, "")
	if err != nil {
		return nil, fmt.Errorf("couldn't read the plugin at %s: %w", base, err)
	}
	old, err := h.Store.Plugin(m.ID)
	if err == nil && old.Kind != "remote" {
		return nil, fmt.Errorf("a plugin with the id %q is already installed in a plugins folder", m.ID)
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	key := store.NewPluginKey()
	body, _ := json.Marshal(map[string]string{"key": key, "serverUrl": serverURL, "id": m.ID})
	req, _ := http.NewRequest(http.MethodPost, base+"/bingebandit/pair", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if old != nil {
		// Proves to an already-paired plugin that this is the same server.
		req.Header.Set("Authorization", "Bearer "+old.Key)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pairing: %w", err)
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return nil, errors.New("the plugin is already paired with another BingeBandit server; clear its data folder and try again")
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pairing: %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	if old != nil {
		err = h.Store.UpdateRemotePlugin(m.ID, base, serverURL, key, raw)
	} else {
		err = h.Store.AddPlugin(&store.Plugin{ID: m.ID, Kind: "remote", URL: base, ServerURL: serverURL, Enabled: true, Key: key, Manifest: raw})
	}
	if err != nil {
		return nil, err
	}
	h.Sync()
	h.Settle(m.ID, 10*time.Second)
	return h.Store.Plugin(m.ID)
}

// Settle waits until a plugin that's starting is up or has failed, so the
// admin sees the outcome rather than "starting".
func (h *Host) Settle(id string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		in := h.inst[id]
		starting := in != nil && in.state == StateStarting
		h.mu.Unlock()
		if !starting {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Remove deletes a remote plugin. Managed plugins are removed by deleting
// their folder; until then they can only be turned off.
func (h *Host) Remove(id string) error {
	p, err := h.Store.Plugin(id)
	if err != nil {
		return err
	}
	if p.Kind != "remote" {
		h.mu.Lock()
		onDisk := h.found[id] != nil
		h.mu.Unlock()
		if onDisk {
			return errors.New("this plugin is in a plugins folder; delete its folder to remove it, or turn it off")
		}
	}
	if err := h.Store.DeletePlugin(id); err != nil {
		return err
	}
	h.Sync()
	if p.Kind == "remote" {
		// Best effort: let it forget us, so it can be paired again later.
		req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(p.URL, "/")+"/bingebandit/unpair", nil)
		req.Header.Set("Authorization", "Bearer "+p.Key)
		if resp, err := h.client.Do(req); err == nil {
			resp.Body.Close()
		}
	}
	return nil
}

// manifestOf is the best manifest we have: the running plugin's, the folder's,
// or the last one stored.
func (h *Host) manifestOf(p *store.Plugin) *Manifest {
	h.mu.Lock()
	defer h.mu.Unlock()
	if in := h.inst[p.ID]; in != nil && in.manifest != nil {
		return in.manifest
	}
	if b := h.found[p.ID]; b != nil {
		return b.manifest
	}
	if m, err := parseManifest(p.Manifest); err == nil {
		return m
	}
	return &Manifest{ID: p.ID, Name: p.ID}
}

// List returns every installed plugin with its state.
func (h *Host) List() ([]*Status, error) {
	rows, err := h.Store.Plugins()
	if err != nil {
		return nil, err
	}
	out := make([]*Status, 0, len(rows))
	for _, p := range rows {
		out = append(out, h.status(p))
	}
	return out, nil
}

func (h *Host) Get(id string) (*Status, error) {
	p, err := h.Store.Plugin(id)
	if err != nil {
		return nil, err
	}
	return h.status(p), nil
}

func (h *Host) status(p *store.Plugin) *Status {
	m := h.manifestOf(p)
	st := &Status{Plugin: p, Manifest: m, State: StateStopped, SecretsSet: []string{}, Values: map[string]string{}}
	h.mu.Lock()
	if in := h.inst[p.ID]; in != nil {
		st.State, st.Error = in.state, in.err
	} else if p.Kind == "managed" && h.found[p.ID] == nil {
		st.State = StateMissing
	}
	h.mu.Unlock()
	values := m.Values(p.Settings)
	for _, s := range m.Settings {
		if s.Type == "password" {
			if values[s.Key] != "" {
				st.SecretsSet = append(st.SecretsSet, s.Key)
			}
			continue
		}
		st.Values[s.Key] = values[s.Key]
	}
	return st
}

// SaveSettings stores the admin's values and tells the plugin. A password
// setting left empty keeps its current value.
func (h *Host) SaveSettings(id string, values map[string]string) error {
	p, err := h.Store.Plugin(id)
	if err != nil {
		return err
	}
	m := h.manifestOf(p)
	next := map[string]string{}
	for _, s := range m.Settings {
		v, ok := values[s.Key]
		if !ok || (s.Type == "password" && v == "") {
			if old, had := p.Settings[s.Key]; had {
				next[s.Key] = old
			}
			continue
		}
		switch s.Type {
		case "bool":
			if v != "true" && v != "false" {
				return fmt.Errorf("%s must be true or false", s.Label)
			}
		case "number":
			if _, err := fmt.Sscanf(v, "%g", new(float64)); err != nil && v != "" {
				return fmt.Errorf("%s must be a number", s.Label)
			}
		case "select":
			if v != "" && !contains(s.Options, v) {
				return fmt.Errorf("%s must be one of %s", s.Label, strings.Join(s.Options, ", "))
			}
		}
		next[s.Key] = v
	}
	if err := h.Store.SetPluginSettings(id, next); err != nil {
		return err
	}
	h.mu.Lock()
	in := h.inst[id]
	h.mu.Unlock()
	if in != nil {
		go h.send(in, "plugin.settings", m.Values(next))
	}
	return nil
}

// Settings returns a plugin's settings with defaults filled in.
func (h *Host) Settings(p *store.Plugin) map[string]string {
	return h.manifestOf(p).Values(p.Settings)
}

// Emit sends an event to every running plugin subscribed to it.
func (h *Host) Emit(event string, data any) {
	h.mu.Lock()
	var to []*instance
	for _, in := range h.inst {
		if in.state == StateRunning && in.manifest != nil && in.manifest.Wants(event) {
			to = append(to, in)
		}
	}
	h.mu.Unlock()
	for _, in := range to {
		go h.send(in, event, data)
	}
}

func (h *Host) send(in *instance, event string, data any) {
	server := ""
	if h.ServerName != nil {
		server = h.ServerName()
	}
	body, _ := json.Marshal(map[string]any{
		"event": event, "timestamp": time.Now().UTC().Format(time.RFC3339), "server": server, "data": data,
	})
	h.mu.Lock()
	base := in.base
	h.mu.Unlock()
	req, err := http.NewRequest(http.MethodPost, base+"/bingebandit/events", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+in.key)
	req.Header.Set("X-BingeBandit-Event", event)
	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("plugin %s: %s: %v", in.id, event, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("plugin %s: %s: %s", in.id, event, resp.Status)
	}
}

// PageLinks are the pages of running plugins a user may open.
type PageLinks struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Pages []Page `json:"pages"`
}

func (h *Host) Pages(admin bool) []PageLinks {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []PageLinks{}
	for _, in := range h.inst {
		if in.state != StateRunning || in.manifest == nil {
			continue
		}
		pl := PageLinks{ID: in.id, Name: in.manifest.Name}
		for _, p := range in.manifest.Pages {
			if !p.Admin || admin {
				pl.Pages = append(pl.Pages, p)
			}
		}
		if len(pl.Pages) > 0 {
			out = append(out, pl)
		}
	}
	return out
}

// target returns where a running plugin listens and its key.
func (h *Host) target(id string) (base, key string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	in := h.inst[id]
	if in == nil || in.state != StateRunning {
		return "", "", false
	}
	return in.base, in.key, true
}

func freeAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// lineLogger writes a plugin's output to the server log a line at a time.
type lineLogger struct {
	prefix string
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (l *lineLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Write(p)
	for {
		line, err := l.buf.ReadString('\n')
		if err != nil {
			l.buf.WriteString(line) // incomplete; keep it for the next write
			break
		}
		log.Print(l.prefix + strings.TrimRight(line, "\r\n"))
	}
	if l.buf.Len() > 64<<10 {
		l.flushLocked()
	}
	return len(p), nil
}

func (l *lineLogger) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushLocked()
}

func (l *lineLogger) flushLocked() {
	if l.buf.Len() > 0 {
		sc := bufio.NewScanner(&l.buf)
		for sc.Scan() {
			log.Print(l.prefix + sc.Text())
		}
		l.buf.Reset()
	}
}
