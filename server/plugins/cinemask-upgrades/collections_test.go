package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bingebandit/pkg/bbplugin"
)

func TestCollectionsAddMissing(t *testing.T) {
	cm := &fakeCinemask{profile: 1}
	cs := httptest.NewServer(cm)
	defer cs.Close()
	dir := t.TempDir()
	a := &app{p: &bbplugin.Plugin{Manifest: manifest}, cols: &colStore{dir: func() string { return dir }}}
	a.settings = map[string]string{"url": cs.URL, "api_key": "cmk_test", "collections": "false"}
	a.missing = func(context.Context, int64) ([]wanted, string, error) {
		return []wanted{{Kind: "movie", TMDBID: 949, Title: "Heat"}, {Kind: "movie", TMDBID: 329865, Title: "Arrival"}}, "kids movies", nil
	}
	h := a.routes()
	do := func(method, path, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-BingeBandit-User-Id", "1")
		req.Header.Set("X-BingeBandit-Admin", "true")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	// Off by default: the editor hides the switches and can't set them.
	if code, body := do("GET", "/collections/7", ""); code != 200 || !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("get: %d %s", code, body)
	}
	if code, _ := do("PUT", "/collections/7", `{"monitor":true}`); code != http.StatusConflict {
		t.Fatalf("put while off: %d", code)
	}
	a.settings["collections"] = "true"
	a.cols.update(7, func(c *colConfig) *colConfig { c.Monitor = true; return c })
	a.runCollection(context.Background(), 7)
	calls := strings.Join(cm.calls, "\n")
	// In the Cinemask folder named like the collection's library.
	if !strings.Contains(calls, `POST /api/monitored {"kind":"movie","rootId":"kids-movies","search":false,"tmdbId":329865}`) {
		t.Fatalf("Arrival wasn't monitored in Kids Movies: %s", calls)
	}
	if strings.Contains(calls, "/search") || strings.Count(calls, "POST /api/monitored ") != 1 {
		t.Fatalf("Heat is already monitored and Search is off: %s", calls)
	}
	c := a.cols.get(7)
	if len(c.Added) != 2 || c.Missing != 2 || c.Error != "" {
		t.Fatalf("config: %+v", c)
	}
	// A second run leaves what it handed over alone.
	cm.calls = nil
	a.runCollection(context.Background(), 7)
	if strings.Contains(strings.Join(cm.calls, "\n"), "POST") {
		t.Fatalf("second run: %v", cm.calls)
	}
	// Search on (which turns Monitor on) searches as it adds.
	if code, body := do("PUT", "/collections/8", `{"search":true}`); code != 200 || !strings.Contains(body, `"monitor":true`) {
		t.Fatalf("put: %d %s", code, body)
	}
	cm.calls = nil
	a.runCollection(context.Background(), 8)
	calls = strings.Join(cm.calls, "\n")
	if !strings.Contains(calls, `"search":true,"tmdbId":329865`) || !strings.Contains(calls, "POST /api/monitored/4/search") {
		t.Fatalf("search run: %s", calls)
	}
}
