package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/plugins"
	"bingebandit/internal/store"
	"bingebandit/pkg/bbplugin"
)

// TestPluginRoundTrip pairs a remote plugin written with bbplugin, then
// checks each direction: events to the plugin, users through the proxy, and
// the plugin calling the plugin API as a user.
func TestPluginRoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("admin", "x", true)
	ann, _ := st.CreateUser("ann", "x", false)
	st.CreateSession("admin-token", admin.ID, "test", "127.0.0.1")
	st.CreateSession("ann-token", ann.ID, "test", "127.0.0.1")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	movie, err := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Parasite", Year: 2019, TMDBID: 496243, MatchKey: "movie:parasite:2019"})
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{Store: st, Version: "test"}
	s.Plugins = plugins.NewHost(st, nil, filepath.Join(dir, "plugin-data"), "")
	srv := httptest.NewServer(s.Router(ctx))
	defer srv.Close()
	go s.Plugins.Start(ctx)

	// The plugin: records events and the headers of proxied requests.
	events := make(chan bbplugin.Event, 8)
	seen := make(chan *http.Request, 1)
	p := &bbplugin.Plugin{
		Manifest: bbplugin.Manifest{
			ID: "fake", Name: "Fake", Version: "1", Events: []string{"playback.finished"},
			Settings: []bbplugin.Setting{{Key: "colour", Label: "Colour", Default: "teal"}, {Key: "token", Label: "Token", Type: "password"}},
			Pages:    []bbplugin.Page{{Title: "Home", Path: "/"}, {Title: "Admin", Path: "/admin", Admin: true}},
		},
		OnEvent: func(_ context.Context, e bbplugin.Event) { events <- e },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen <- r
			u, _ := bbplugin.UserOf(r)
			io.WriteString(w, u.Username)
		}),
		DataDir: filepath.Join(dir, "fake-data"),
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go p.Serve(ctx, l)
	pluginURL := "http://" + l.Addr().String()

	call := func(method, path, token string, body any) (int, string) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	// Pair it from Admin → Plugins.
	if code, body := call("POST", "/api/admin/plugins", "ann-token", map[string]string{"url": pluginURL}); code != http.StatusForbidden {
		t.Fatalf("non-admin add: %d %s", code, body)
	}
	if code, body := call("POST", "/api/admin/plugins", "admin-token", map[string]string{"url": pluginURL, "serverUrl": srv.URL}); code != http.StatusOK || !strings.Contains(body, `"state":"running"`) {
		t.Fatalf("add: %d %s", code, body)
	}

	// Pairing a second time without the key is refused by the plugin.
	req, _ := http.NewRequest("POST", pluginURL+"/bingebandit/pair", strings.NewReader(`{"key":"bbp_x","serverUrl":"http://evil"}`))
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusConflict {
		t.Errorf("second pairing: %d", resp.StatusCode)
	}
	// And the plugin's routes refuse callers without the key.
	if resp, _ := http.Get(pluginURL + "/anything"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("direct call: %d", resp.StatusCode)
	}

	// Proxy: the user arrives in headers; their token doesn't.
	if code, body := call("GET", "/api/plugins/fake/hello?token=ann-token&x=1", "", nil); code != http.StatusOK || body != "ann" {
		t.Fatalf("proxy: %d %s", code, body)
	}
	r := <-seen
	if r.URL.Path != "/hello" || r.URL.Query().Get("token") != "" || r.URL.Query().Get("x") != "1" ||
		r.Header.Get("X-BingeBandit-User-Id") != strconv.FormatInt(ann.ID, 10) || r.Header.Get("X-BingeBandit-Admin") != "false" ||
		r.Header.Get("X-BingeBandit-Prefix") != "/api/plugins/fake" || r.Header.Get("Cookie") != "" {
		t.Errorf("proxied request: %s %v", r.URL, r.Header)
	}
	for _, path := range []string{"/api/plugins/fake/bingebandit/events", "/api/plugins/fake/a/../BingeBandit/manifest"} {
		if code, _ := call("POST", path, "ann-token", map[string]string{}); code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, code)
		}
	}
	if code, _ := call("GET", "/api/plugins/fake/hello", "", nil); code != http.StatusUnauthorized {
		t.Errorf("signed-out proxy: %d", code)
	}

	// Pages: admins see admin pages too.
	if _, body := call("GET", "/api/plugins", "ann-token", nil); !strings.Contains(body, `"Home"`) || strings.Contains(body, `"Admin"`) {
		t.Errorf("ann's pages: %s", body)
	}
	if _, body := call("GET", "/api/plugins", "admin-token", nil); !strings.Contains(body, `"Admin"`) {
		t.Errorf("admin's pages: %s", body)
	}

	// Events reach only plugins subscribed to them.
	s.Emit("playback.start", map[string]any{"itemId": movie})
	s.Emit("playback.finished", map[string]any{"itemId": movie, "userId": ann.ID})
	select {
	case e := <-events:
		if e.Event != "playback.finished" || !strings.Contains(string(e.Data), `"userId":`) {
			t.Errorf("event: %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}

	// Settings: defaults filled in, secrets never sent back, and the plugin is told.
	if code, body := call("PUT", "/api/admin/plugins/fake/settings", "admin-token", map[string]string{"token": "s3cret"}); code != http.StatusOK ||
		strings.Contains(body, "s3cret") || !strings.Contains(body, `"secretsSet":["token"]`) {
		t.Fatalf("settings: %d %s", code, body)
	}
	select {
	case e := <-events:
		if e.Event != "plugin.settings" || !strings.Contains(string(e.Data), `"colour":"teal"`) || !strings.Contains(string(e.Data), `"token":"s3cret"`) {
			t.Errorf("settings event: %s %s", e.Event, e.Data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no settings event")
	}
	call("PUT", "/api/admin/plugins/fake/settings", "admin-token", map[string]string{"token": "", "colour": "red"})
	if conf, err := p.Settings(ctx); err != nil || conf["token"] != "s3cret" || conf["colour"] != "red" {
		t.Errorf("settings kept: %v %v", conf, err)
	}

	// The plugin API, acting as ann.
	uid := strconv.FormatInt(ann.ID, 10)
	var found []store.Item
	if err := p.Call(ctx, "GET", "/users/"+uid+"/lookup?tmdb=496243", nil, &found); err != nil || len(found) != 1 || found[0].ID != movie {
		t.Fatalf("lookup by tmdb: %v %+v", err, found)
	}
	if err := p.Call(ctx, "GET", "/users/"+uid+"/lookup?title=parasite&year=2020", nil, &found); err != nil || len(found) != 1 {
		t.Fatalf("lookup by title: %v %+v", err, found)
	}
	if err := p.Call(ctx, "POST", "/users/"+uid+"/watchlist/"+strconv.FormatInt(movie, 10), nil, nil); err != nil {
		t.Fatal(err)
	}
	if wl, _ := st.Watchlist(ann.ID); len(wl) != 1 || wl[0].ID != movie {
		t.Errorf("ann's watchlist: %+v", wl)
	}
	if wl, _ := st.Watchlist(admin.ID); len(wl) != 0 {
		t.Errorf("admin's watchlist should be empty: %+v", wl)
	}
	var users []map[string]any
	if err := p.Call(ctx, "GET", "/users", nil, &users); err != nil || len(users) != 2 {
		t.Errorf("users: %v %v", err, users)
	}
	// A plugin key isn't a user token, and a user token isn't a plugin key.
	if code, _ := call("GET", "/api/plugin/v1/users", "ann-token", nil); code != http.StatusUnauthorized {
		t.Errorf("user token on plugin API: %d", code)
	}
	pl, _ := st.Plugin("fake")
	if code, _ := call("GET", "/api/me", pl.Key, nil); code != http.StatusUnauthorized {
		t.Errorf("plugin key on user API: %d", code)
	}

	// Turned off: its key stops working and the proxy stops.
	if code, body := call("PUT", "/api/admin/plugins/fake", "admin-token", map[string]bool{"enabled": false}); code != http.StatusOK || !strings.Contains(body, `"state":"stopped"`) {
		t.Fatalf("disable: %d %s", code, body)
	}
	if err := p.Call(ctx, "GET", "/users", nil, nil); err == nil {
		t.Error("a disabled plugin's key should be refused")
	}
	if code, _ := call("GET", "/api/plugins/fake/hello", "ann-token", nil); code != http.StatusServiceUnavailable {
		t.Errorf("proxy to a disabled plugin: %d", code)
	}

	// Turned back on and added again: pairs again with a new key, sent with the old one.
	call("PUT", "/api/admin/plugins/fake", "admin-token", map[string]bool{"enabled": true})
	if code, body := call("POST", "/api/admin/plugins", "admin-token", map[string]string{"url": pluginURL, "serverUrl": srv.URL}); code != http.StatusOK {
		t.Fatalf("re-add: %d %s", code, body)
	}
	if pl2, _ := st.Plugin("fake"); pl2.Key == pl.Key {
		t.Error("re-adding should rotate the key")
	}
	if err := p.Call(ctx, "GET", "/users", nil, nil); err != nil {
		t.Errorf("after re-pairing: %v", err)
	}

	// Removed: the plugin forgets the server, so it can be added again.
	if code, _ := call("DELETE", "/api/admin/plugins/fake", "admin-token", nil); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if err := p.Call(ctx, "GET", "/users", nil, nil); err != bbplugin.ErrNotPaired {
		t.Errorf("after removal the plugin should be unpaired: %v", err)
	}
	if code, body := call("POST", "/api/admin/plugins", "admin-token", map[string]string{"url": pluginURL, "serverUrl": srv.URL}); code != http.StatusOK {
		t.Errorf("add after removal: %d %s", code, body)
	}
}
