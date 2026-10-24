package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

func netTestServer(t *testing.T, settings map[string]string) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st}
	s.reloadNetwork()
	return s
}

// serve runs one request through the network middleware and reports what the handler saw.
func serve(s *Server, remote string, hdr map[string]string) (status int, ip, base string) {
	req := httptest.NewRequest(http.MethodGet, "http://bb.local:8484/x", nil)
	req.RemoteAddr = remote
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.network(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, base = clientIP(r), baseURL(r)
	})).ServeHTTP(rec, req)
	return rec.Code, ip, base
}

func TestForwardedHeaders(t *testing.T) {
	s := netTestServer(t, map[string]string{store.SettingTrustedProxies: "10.0.0.2, 172.16.0.0/12"})
	fwd := map[string]string{"X-Forwarded-For": "6.6.6.6, 203.0.113.9, 172.17.0.5", "X-Forwarded-Proto": "https", "X-Forwarded-Host": "tv.example.com"}

	// From a trusted proxy: the rightmost untrusted hop is the client (6.6.6.6 was spoofable).
	if _, ip, base := serve(s, "10.0.0.2:5000", fwd); ip != "203.0.113.9" || base != "https://tv.example.com" {
		t.Errorf("trusted proxy: ip %q base %q", ip, base)
	}
	// From anyone else the headers are ignored.
	if _, ip, base := serve(s, "198.51.100.7:5000", fwd); ip != "198.51.100.7" || base != "http://bb.local:8484" {
		t.Errorf("untrusted peer: ip %q base %q", ip, base)
	}
	// X-Real-IP works when there's no X-Forwarded-For.
	if _, ip, _ := serve(s, "10.0.0.2:5000", map[string]string{"X-Real-IP": "203.0.113.4"}); ip != "203.0.113.4" {
		t.Errorf("x-real-ip: %q", ip)
	}
}

func TestRemoteAccessOff(t *testing.T) {
	s := netTestServer(t, map[string]string{store.SettingRemoteAccess: "false", store.SettingLANNetworks: "100.64.0.0/10", store.SettingTrustedProxies: "127.0.0.1"})
	for remote, want := range map[string]int{
		"192.168.1.20:1": 200, // private
		"127.0.0.1:1":    200, // loopback
		"100.70.1.2:1":   200, // listed LAN range (e.g. Tailscale)
		"203.0.113.9:1":  403,
	} {
		if code, _, _ := serve(s, remote, nil); code != want {
			t.Errorf("%s: got %d, want %d", remote, code, want)
		}
	}
	// A request relayed by a local proxy is judged by the real client.
	if code, _, _ := serve(s, "127.0.0.1:1", map[string]string{"X-Forwarded-For": "203.0.113.9"}); code != 403 {
		t.Errorf("proxied outside client: got %d, want 403", code)
	}
}

func TestParseNets(t *testing.T) {
	if n, err := parseNets("10.0.0.1\n192.168.0.0/16; fd00::/8"); err != nil || len(n) != 3 {
		t.Errorf("parseNets: %v %v", n, err)
	}
	if _, err := parseNets("10.0.0.300"); err == nil {
		t.Error("expected an error for a bad IP")
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{"/live": "/live", "//evil.com": "/", "https://evil.com": "/", "/\\evil.com": "/", "": "/", "/api/x": "/"} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
