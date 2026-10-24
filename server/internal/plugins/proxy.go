package plugins

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"

	"bingebandit/internal/store"
)

// Caller is who a proxied request comes from.
type Caller struct {
	User        *store.User
	Prefix      string // where the plugin is mounted, e.g. /api/plugins/watchlist-rss
	ExternalURL string // BingeBandit's public address, for absolute links
}

// Proxy forwards a signed-in user's request to the plugin's own routes. The
// user's credentials (cookie, Authorization, ?token=) are stripped and the
// plugin's key is sent instead, with the user in X-BingeBandit-User-* headers.
// The protocol routes under /bingebandit/ can't be reached this way.
func (h *Host) Proxy(w http.ResponseWriter, r *http.Request, id, rest string, c Caller) {
	base, key, ok := h.target(id)
	if !ok {
		http.Error(w, `{"error":"plugin not running"}`, http.StatusServiceUnavailable)
		return
	}
	clean := path.Clean("/" + rest)
	if clean == "/bingebandit" || strings.HasPrefix(strings.ToLower(clean), "/bingebandit/") {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	if strings.HasSuffix(rest, "/") && clean != "/" {
		clean += "/"
	}
	target, err := url.Parse(base)
	if err != nil {
		http.Error(w, `{"error":"bad plugin address"}`, http.StatusBadGateway)
		return
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			q := pr.In.URL.Query()
			q.Del("token")
			pr.Out.URL = &url.URL{Scheme: target.Scheme, Host: target.Host, Path: strings.TrimRight(target.Path, "/") + clean, RawQuery: q.Encode()}
			pr.Out.Host = target.Host
			pr.SetXForwarded()
			for k := range pr.Out.Header {
				if strings.HasPrefix(http.CanonicalHeaderKey(k), "X-Bingebandit-") {
					pr.Out.Header.Del(k)
				}
			}
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Set("Authorization", "Bearer "+key)
			pr.Out.Header.Set("X-BingeBandit-User-Id", strconv.FormatInt(c.User.ID, 10))
			pr.Out.Header.Set("X-BingeBandit-User", c.User.Username)
			pr.Out.Header.Set("X-BingeBandit-Admin", strconv.FormatBool(c.User.IsAdmin))
			pr.Out.Header.Set("X-BingeBandit-Prefix", c.Prefix)
			pr.Out.Header.Set("X-BingeBandit-External-URL", strings.TrimRight(c.ExternalURL, "/"))
		},
		// A plugin must not set cookies on BingeBandit's origin (it could
		// replace the session cookie).
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, `{"error":"plugin unavailable"}`, http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r)
}
