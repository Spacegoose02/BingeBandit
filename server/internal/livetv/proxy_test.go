package livetv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestParseProxy(t *testing.T) {
	for raw, ok := range map[string]bool{
		"":                      true,
		"http://10.0.0.2:8888":  true,
		"socks5://u:p@vpn:1080": true,
		"ftp://host:21":         false,
		"10.0.0.2:8888":         false,
		"http://":               false,
	} {
		if _, err := ParseProxy(raw); (err == nil) != ok {
			t.Errorf("ParseProxy(%q) err = %v, want ok=%v", raw, err, ok)
		}
	}
}

func TestStreamsGoThroughProxy(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 188))
	}))
	defer provider.Close()
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1) // a plain HTTP proxy gets the absolute URL; forward it
		resp, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	defer proxy.Close()

	if err := SetProxy(proxy.URL); err != nil {
		t.Fatal(err)
	}
	defer SetProxy("")
	m := &Manager{}
	body, err := m.openStream(context.Background(), provider.URL+"/live/1.ts", "")
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(body)
	body.Close()
	if proxied.Load() != 1 {
		t.Errorf("proxy saw %d requests, want 1", proxied.Load())
	}
}
