package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"bingebandit/internal/store"
)

// netPolicy is the parsed network settings, cached so every request doesn't
// hit the database. reloadNetwork refreshes it after settings change.
type netPolicy struct {
	trusted   []*net.IPNet
	lan       []*net.IPNet
	remote    bool
	publicURL string
}

type netHolder struct{ atomic.Pointer[netPolicy] }

// parseNets reads comma/space/newline separated IPs and CIDRs.
func parseNets(s string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == ';' }) {
		if !strings.Contains(f, "/") {
			ip := net.ParseIP(f)
			if ip == nil {
				return nil, fmt.Errorf("%q is not an IP address or CIDR range", f)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			f = fmt.Sprintf("%s/%d", f, bits)
		}
		_, n, err := net.ParseCIDR(f)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IP address or CIDR range", f)
		}
		out = append(out, n)
	}
	return out, nil
}

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// validPublicURL accepts "" or an absolute http(s) URL without a query.
func validPublicURL(s string) error {
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" {
		return fmt.Errorf("public URL must look like https://tv.example.com")
	}
	return nil
}

func (s *Server) reloadNetwork() {
	p := &netPolicy{remote: s.Store.Setting(store.SettingRemoteAccess) != "false", publicURL: strings.TrimRight(s.Store.Setting(store.SettingPublicURL), "/")}
	p.trusted, _ = parseNets(s.Store.Setting(store.SettingTrustedProxies))
	p.lan, _ = parseNets(s.Store.Setting(store.SettingLANNetworks))
	s.netp.Store(p)
}

func (s *Server) policy() *netPolicy {
	if p := s.netp.Load(); p != nil {
		return p
	}
	s.reloadNetwork()
	return s.netp.Load()
}

// isLocal reports whether ip is on the home network: private, loopback or
// link-local ranges, plus any LAN networks the admin listed.
func (s *Server) isLocal(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	return p.IsPrivate() || p.IsLoopback() || p.IsLinkLocalUnicast() || inNets(p, s.policy().lan)
}

const forwardedProtoKey ctxKey = 2

// network resolves the real client address behind trusted reverse proxies
// and enforces "local networks only" when remote access is off.
func (s *Server) network(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := s.policy()
		peer := clientIP(r)
		if ip := net.ParseIP(peer); ip != nil && inNets(ip, p.trusted) {
			client := ""
			// Walk X-Forwarded-For from the right: the last entry not added by
			// one of our own proxies is the real client.
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				hops := strings.Split(xff, ",")
				for i := len(hops) - 1; i >= 0; i-- {
					h := strings.TrimSpace(hops[i])
					hip := net.ParseIP(h)
					if hip == nil {
						break
					}
					client = h
					if !inNets(hip, p.trusted) {
						break
					}
				}
			} else if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(xr) != nil {
				client = xr
			}
			if client != "" {
				r.RemoteAddr = net.JoinHostPort(client, "0")
			}
			if proto := strings.ToLower(r.Header.Get("X-Forwarded-Proto")); proto == "https" || proto == "http" {
				r = r.WithContext(context.WithValue(r.Context(), forwardedProtoKey, proto))
			}
			if host := r.Header.Get("X-Forwarded-Host"); host != "" && !strings.ContainsAny(host, "/ ,") {
				r.Host = host
			}
		}
		if !p.remote && !s.isLocal(clientIP(r)) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintln(w, "Remote access to this BingeBandit server is turned off. Connect from the home network.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isHTTPS(r *http.Request) bool {
	if proto, ok := r.Context().Value(forwardedProtoKey).(string); ok {
		return proto == "https"
	}
	return r.TLS != nil
}

// externalURL is where browsers outside should be sent back to (OIDC
// redirects): the configured public URL, else the URL this request came in on.
func (s *Server) externalURL(r *http.Request) string {
	if u := s.policy().publicURL; u != "" {
		return u
	}
	return baseURL(r)
}
