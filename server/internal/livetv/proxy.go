package livetv

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// proxyURL is the admin's proxy for provider traffic (streams, playlists,
// guides and logos); nil falls back to the HTTP(S)_PROXY environment.
var proxyURL atomic.Pointer[url.URL]

// SetProxy sets (or with "" clears) the proxy used to reach providers.
func SetProxy(raw string) error {
	u, err := ParseProxy(raw)
	if err != nil {
		return err
	}
	proxyURL.Store(u)
	return nil
}

// ParseProxy checks a proxy address: http://, https:// or socks5://, with an
// optional user:password.
func ParseProxy(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("proxy must look like http://host:port")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("proxy must start with http://, https:// or socks5://")
	}
	return u, nil
}

func providerProxy(r *http.Request) (*url.URL, error) {
	if u := proxyURL.Load(); u != nil {
		return u, nil
	}
	return http.ProxyFromEnvironment(r)
}

// providerTransport is the default transport, but through providerProxy.
func providerTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = providerProxy
	return t
}

// ffmpegProxyArgs passes the proxy to ffmpeg for HLS upstreams. ffmpeg only
// speaks HTTP proxies; others are left to the environment.
func ffmpegProxyArgs() []string {
	if u := proxyURL.Load(); u != nil && u.Scheme == "http" {
		return []string{"-http_proxy", u.String()}
	}
	return nil
}
