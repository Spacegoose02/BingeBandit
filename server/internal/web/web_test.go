package web

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestInstallFilesHeaders(t *testing.T) {
	h := handlerFor(fstest.MapFS{
		"dist/index.html":           {Data: []byte("<!doctype html>")},
		"dist/manifest.webmanifest": {Data: []byte(`{"name":"BingeBandit"}`)},
		"dist/sw.js":                {Data: []byte("self.addEventListener('push', () => {})")},
	})
	for _, c := range []struct{ path, ctype, cache string }{
		{"/manifest.webmanifest", "application/manifest+json", "no-cache"},
		{"/sw.js", "text/javascript; charset=utf-8", "no-cache"},
		{"/some/route", "text/html; charset=utf-8", "no-cache"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", c.path, nil))
		if rec.Code != 200 || rec.Header().Get("Content-Type") != c.ctype || rec.Header().Get("Cache-Control") != c.cache {
			t.Errorf("%s: %d %q %q", c.path, rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
		}
	}
}
