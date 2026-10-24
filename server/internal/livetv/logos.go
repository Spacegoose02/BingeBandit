package livetv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxLogoBytes = 2 << 20

var logoClient = &http.Client{Timeout: 15 * time.Second, Transport: providerTransport()}

// LogoCache keeps provider channel logos on disk so the guide loads quickly,
// works over HTTPS when the provider's logos are HTTP-only, and survives the
// provider's logo server being down.
type LogoCache struct {
	dir string
	mu  sync.Mutex
}

func NewLogoCache(dir string) *LogoCache { return &LogoCache{dir: dir} }

// Get returns the cached file and its content type, downloading it on first use.
func (c *LogoCache) Get(ctx context.Context, url string) (string, string, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", "", fmt.Errorf("unsupported logo URL")
	}
	sum := sha256.Sum256([]byte(url))
	base := filepath.Join(c.dir, hex.EncodeToString(sum[:16]))
	for _, ext := range []string{".png", ".jpg", ".svg", ".webp", ".gif"} {
		if _, err := os.Stat(base + ext); err == nil {
			return base + ext, typeOf(ext), nil
		}
	}
	c.mu.Lock() // one download at a time is plenty for logos
	defer c.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "BingeBandit")
	resp, err := logoClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("logo download failed: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLogoBytes+1))
	if err != nil || len(data) > maxLogoBytes {
		return "", "", fmt.Errorf("logo too large or unreadable")
	}
	ext := extOf(resp.Header.Get("Content-Type"), data)
	if ext == "" {
		return "", "", fmt.Errorf("logo is not an image")
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(base+ext, data, 0o644); err != nil {
		return "", "", err
	}
	return base + ext, typeOf(ext), nil
}

func extOf(contentType string, data []byte) string {
	head := string(data[:min(len(data), 512)])
	if strings.Contains(strings.ToLower(contentType), "svg") || strings.Contains(head, "<svg") {
		return ".svg"
	}
	switch http.DetectContentType(data) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	return ""
}

func typeOf(ext string) string {
	return map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".svg": "image/svg+xml", ".webp": "image/webp", ".gif": "image/gif"}[ext]
}
