// Package bbplugin is a small kit for writing BingeBandit plugins in Go. A
// plugin is an HTTP service; this package serves the protocol routes
// (/bingebandit/manifest, /pair, /events), checks that requests come from
// BingeBandit, and gives you a client for the plugin API. Plugins in other
// languages follow docs/PLUGINS.md directly.
//
//	p := &bbplugin.Plugin{Manifest: bbplugin.Manifest{ID: "hello", Name: "Hello"}}
//	p.OnEvent = func(ctx context.Context, e bbplugin.Event) { ... }
//	log.Fatal(p.Run())
package bbplugin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Manifest tells BingeBandit what the plugin is and wants. See docs/PLUGINS.md.
type Manifest struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version,omitempty"`
	Description string    `json:"description,omitempty"`
	Author      string    `json:"author,omitempty"`
	Homepage    string    `json:"homepage,omitempty"`
	Events      []string  `json:"events,omitempty"`
	Settings    []Setting `json:"settings,omitempty"`
	Pages       []Page    `json:"pages,omitempty"`
}

type Setting struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type,omitempty"` // text, password, number, bool, select
	Default string   `json:"default,omitempty"`
	Help    string   `json:"help,omitempty"`
	Options []string `json:"options,omitempty"`
}

type Page struct {
	Title string `json:"title"`
	Path  string `json:"path"`
	Admin bool   `json:"admin,omitempty"`
}

// Event is something that happened on the server ("playback.finished",
// "plugin.settings", …). Data's shape depends on the event.
type Event struct {
	Event     string          `json:"event"`
	Timestamp string          `json:"timestamp"`
	Server    string          `json:"server"`
	Data      json.RawMessage `json:"data"`
}

// Plugin is one plugin process.
type Plugin struct {
	Manifest Manifest
	// Handler serves the plugin's own routes (pages, feeds), which users
	// reach through /api/plugins/{id}/. Requests arrive only from BingeBandit,
	// with the user in UserOf(r).
	Handler http.Handler
	// OnEvent is called for each event the manifest subscribed to, plus
	// "plugin.settings" (Data is the new settings) whenever an admin saves them.
	OnEvent func(ctx context.Context, e Event)
	// Start, if set, runs in the background once the plugin is paired (at
	// once for plugins BingeBandit starts itself). ctx ends at shutdown.
	Start func(ctx context.Context)

	// DataDir is where the plugin may keep files. Set by Run.
	DataDir string

	mu      sync.Mutex
	pairing pairing
	fromEnv bool
	started bool
	ctx     context.Context
	client  *http.Client
}

type pairing struct {
	Key       string `json:"key"`
	ServerURL string `json:"serverUrl"`
}

// ErrNotPaired means BingeBandit hasn't paired with this plugin yet.
var ErrNotPaired = errors.New("not paired with BingeBandit yet")

// Run serves the plugin until SIGINT/SIGTERM. Configuration comes from the
// environment: BingeBandit sets these for plugins it starts; a plugin run
// as its own service only needs BINGEBANDIT_PLUGIN_ADDR and _DATA, and is
// paired from Admin → Plugins.
//
//	BINGEBANDIT_PLUGIN_ADDR  where to listen (default :8490)
//	BINGEBANDIT_PLUGIN_DATA  folder for the plugin's files (default ./data)
//	BINGEBANDIT_URL, BINGEBANDIT_PLUGIN_KEY  skip pairing
func (p *Plugin) Run() error {
	p.DataDir = envOr("BINGEBANDIT_PLUGIN_DATA", "data")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	l, err := net.Listen("tcp", envOr("BINGEBANDIT_PLUGIN_ADDR", ":8490"))
	if err != nil {
		return err
	}
	return p.Serve(ctx, l)
}

// Serve serves the plugin on l until ctx ends. Run is the usual entry point;
// Serve is for tests and for embedding. DataDir must be set.
func (p *Plugin) Serve(ctx context.Context, l net.Listener) error {
	if err := os.MkdirAll(p.DataDir, 0o755); err != nil {
		return err
	}
	p.client = &http.Client{Timeout: 30 * time.Second}
	if key := os.Getenv("BINGEBANDIT_PLUGIN_KEY"); key != "" {
		p.pairing = pairing{Key: key, ServerURL: strings.TrimRight(os.Getenv("BINGEBANDIT_URL"), "/")}
		p.fromEnv = true
	} else if b, err := os.ReadFile(p.pairingFile()); err == nil {
		json.Unmarshal(b, &p.pairing)
	}
	p.ctx = ctx
	srv := &http.Server{Handler: p.routes(), ReadHeaderTimeout: 10 * time.Second}
	p.maybeStart()
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	log.Printf("%s %s listening on %s", p.Manifest.ID, p.Manifest.Version, l.Addr())
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

func (p *Plugin) pairingFile() string { return filepath.Join(p.DataDir, "bingebandit-pairing.json") }

func (p *Plugin) maybeStart() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.pairing.Key == "" || p.Start == nil {
		return
	}
	p.started = true
	go p.Start(p.ctx)
}

func (p *Plugin) key() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pairing.Key
}

// fromServer checks the request carries the key BingeBandit paired with.
func (p *Plugin) fromServer(r *http.Request) bool {
	key := p.key()
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return key != "" && subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1
}

func (p *Plugin) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /bingebandit/manifest", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, p.Manifest)
	})
	mux.HandleFunc("POST /bingebandit/pair", p.pair)
	mux.HandleFunc("POST /bingebandit/unpair", p.unpair)
	mux.HandleFunc("POST /bingebandit/events", func(w http.ResponseWriter, r *http.Request) {
		if !p.fromServer(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad key"})
			return
		}
		var e Event
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&e); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad event"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		if p.OnEvent != nil {
			go p.OnEvent(p.ctx, e)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !p.fromServer(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "this plugin is only reachable through BingeBandit"})
			return
		}
		if p.Handler == nil {
			http.NotFound(w, r)
			return
		}
		p.Handler.ServeHTTP(w, r)
	})
	return mux
}

// pair accepts BingeBandit's key. The first server to pair wins; after that
// only a request signed with the current key can pair again.
func (p *Plugin) pair(w http.ResponseWriter, r *http.Request) {
	var req pairing
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || !strings.HasPrefix(req.Key, "bbp_") || req.ServerURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "send {key, serverUrl}"})
		return
	}
	if p.fromEnv {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this plugin's key is set by BINGEBANDIT_PLUGIN_KEY"})
		return
	}
	if p.key() != "" && !p.fromServer(r) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already paired"})
		return
	}
	req.ServerURL = strings.TrimRight(req.ServerURL, "/")
	b, _ := json.Marshal(req)
	if err := os.WriteFile(p.pairingFile(), b, 0o600); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	p.mu.Lock()
	p.pairing = req
	p.mu.Unlock()
	log.Printf("paired with BingeBandit at %s", req.ServerURL)
	p.maybeStart()
	w.WriteHeader(http.StatusNoContent)
}

// unpair forgets the server, when an admin removes the plugin.
func (p *Plugin) unpair(w http.ResponseWriter, r *http.Request) {
	if p.fromEnv || !p.fromServer(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad key"})
		return
	}
	os.Remove(p.pairingFile())
	p.mu.Lock()
	p.pairing = pairing{}
	p.mu.Unlock()
	log.Print("unpaired from BingeBandit")
	w.WriteHeader(http.StatusNoContent)
}

// ---- calling BingeBandit ----

// APIError is a non-2xx answer from the plugin API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("bingebandit: %d %s", e.Status, e.Message) }

// Call sends a request to the plugin API. path is relative to /api/plugin/v1
// ("/users", "/users/3/watchlist/12"); body and out are JSON (either may be nil).
func (p *Plugin) Call(ctx context.Context, method, path string, body, out any) error {
	p.mu.Lock()
	pr := p.pairing
	p.mu.Unlock()
	if pr.Key == "" {
		return ErrNotPaired
	}
	var rd *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, pr.ServerURL+"/api/plugin/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+pr.Key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return &APIError{Status: resp.StatusCode, Message: e.Error}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Connection returns BingeBandit's address and the plugin's key, for tools
// that make their own requests (such as ffmpeg reading /files/{id}/direct).
// ok is false until the plugin is paired.
func (p *Plugin) Connection() (serverURL, key string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pairing.ServerURL, p.pairing.Key, p.pairing.Key != ""
}

// Settings fetches the admin's settings for this plugin, with defaults filled in.
func (p *Plugin) Settings(ctx context.Context) (map[string]string, error) {
	var s map[string]string
	err := p.Call(ctx, http.MethodGet, "/settings", nil, &s)
	return s, err
}

// Log writes a line to BingeBandit's log (level is "info", "warn" or "error").
// Plugins BingeBandit starts can just print; their output is logged too.
func (p *Plugin) Log(ctx context.Context, level, msg string) error {
	return p.Call(ctx, http.MethodPost, "/log", map[string]string{"level": level, "message": msg}, nil)
}

// ---- requests from users ----

// User is the signed-in BingeBandit user a proxied request is for.
type User struct {
	ID       int64
	Username string
	Admin    bool
}

// UserOf returns the user BingeBandit forwarded the request for.
func UserOf(r *http.Request) (User, bool) {
	id, err := strconv.ParseInt(r.Header.Get("X-BingeBandit-User-Id"), 10, 64)
	if err != nil || id <= 0 {
		return User{}, false
	}
	return User{ID: id, Username: r.Header.Get("X-BingeBandit-User"), Admin: r.Header.Get("X-BingeBandit-Admin") == "true"}, true
}

// Prefix is the path the plugin is mounted at in the browser
// (/api/plugins/{id}); use it to build links in pages.
func Prefix(r *http.Request) string { return r.Header.Get("X-BingeBandit-Prefix") }

// ExternalURL is BingeBandit's public address, for absolute links (feeds).
func ExternalURL(r *http.Request) string { return r.Header.Get("X-BingeBandit-External-URL") }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
