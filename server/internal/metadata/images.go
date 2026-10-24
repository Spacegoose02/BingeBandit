package metadata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	reImageFile = regexp.MustCompile(`^[A-Za-z0-9_-]+\.(jpg|jpeg|png|svg|webp)$`)
	imageSizes  = map[string]bool{"w92": true, "w185": true, "w342": true, "w500": true, "w780": true, "w1280": true, "original": true}
)

// ImageCache keeps a disk copy of every TMDB image a client has asked for, so
// artwork keeps loading when TMDB is slow or the server is offline.
type ImageCache struct {
	dir      string
	hc       *http.Client
	mu       sync.Mutex
	inflight map[string]*sync.WaitGroup
}

func NewImageCache(dir string) *ImageCache {
	return &ImageCache{dir: dir, hc: &http.Client{Timeout: 30 * time.Second}, inflight: map[string]*sync.WaitGroup{}}
}

// TMDB returns the local path for a TMDB image, downloading it if needed.
func (c *ImageCache) TMDB(ctx context.Context, size, file string) (string, error) {
	if !reImageFile.MatchString(file) {
		return "", fmt.Errorf("bad image name")
	}
	if !imageSizes[size] {
		size = "w500"
	}
	if strings.HasSuffix(file, ".svg") {
		size = "original"
	}
	local := filepath.Join(c.dir, "tmdb", size, file)
	if _, err := os.Stat(local); err == nil {
		return local, nil
	}

	// Collapse concurrent requests for the same image into one download.
	c.mu.Lock()
	if wg, ok := c.inflight[local]; ok {
		c.mu.Unlock()
		wg.Wait()
		if _, err := os.Stat(local); err == nil {
			return local, nil
		}
		return "", fmt.Errorf("image download failed")
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	c.inflight[local] = wg
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.inflight, local)
		c.mu.Unlock()
		wg.Done()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://image.tmdb.org/t/p/"+size+"/"+file, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tmdb image: %s", resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(local), ".dl-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), local); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return local, nil
}
