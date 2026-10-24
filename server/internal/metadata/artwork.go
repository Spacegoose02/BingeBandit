package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// MaxArtworkBytes caps uploads and URL downloads.
const MaxArtworkBytes = 20 << 20

var (
	reLocalFile = regexp.MustCompile(`^[0-9a-f]{32}\.(jpg|png|webp)$`)
	reTMDBRef   = regexp.MustCompile(`^tmdb:/[A-Za-z0-9_-]+\.(jpg|jpeg|png|svg|webp)$`)
	artworkExt  = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}
)

// ValidTMDBRef reports whether ref is a well-formed "tmdb:/<file>" reference.
func ValidTMDBRef(ref string) bool { return reTMDBRef.MatchString(ref) }

// Artwork stores admin-supplied images (uploads, URLs) in the data folder.
// Files are content-addressed, so the same image is stored once.
type Artwork struct {
	dir string
	hc  *http.Client
}

func NewArtwork(dir string) *Artwork {
	return &Artwork{dir: dir, hc: &http.Client{Timeout: 30 * time.Second}}
}

// Save stores an image and returns its "local:/<file>" reference.
func (a *Artwork) Save(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxArtworkBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxArtworkBytes {
		return "", fmt.Errorf("image is larger than %d MB", MaxArtworkBytes>>20)
	}
	ext, ok := artworkExt[http.DetectContentType(data)]
	if !ok {
		return "", errors.New("only JPEG, PNG and WebP images are supported")
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:16]) + ext
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(a.dir, name)
	if _, err := os.Stat(path); err != nil {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return "", err
		}
	}
	return "local:/" + name, nil
}

// Download fetches an image from an http(s) URL and stores it.
func (a *Artwork) Download(ctx context.Context, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("enter a full http:// or https:// image URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "BingeBandit")
	resp, err := a.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(resp.Body, MaxArtworkBytes+1)); err != nil {
		return "", err
	}
	return a.Save(&buf)
}

// Path resolves a local artwork file name to its path on disk.
func (a *Artwork) Path(file string) (string, error) {
	if !reLocalFile.MatchString(file) {
		return "", os.ErrNotExist
	}
	return filepath.Join(a.dir, file), nil
}
