package plugins

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OfficialCatalog lists the plugins built with each BingeBandit release. The
// release workflow publishes it next to the plugin zips.
const OfficialCatalog = "https://github.com/Spacegoose02/BingeBandit/releases/latest/download/plugin-catalog.json"

// Catalog is a list of plugins that can be installed from Admin → Plugins.
type Catalog struct {
	Name    string         `json:"name,omitempty"`
	Plugins []CatalogEntry `json:"plugins"`
}

// CatalogEntry is one plugin in a catalogue. Downloads are zips keyed by
// "<os>-<arch>" (Go's names, e.g. linux-amd64), each holding the plugin's
// folder with its plugin.json.
type CatalogEntry struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description,omitempty"`
	Author      string              `json:"author,omitempty"`
	Homepage    string              `json:"homepage,omitempty"`
	Downloads   map[string]Download `json:"downloads"`
}

type Download struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// CatalogPlugin is a catalogue entry as Admin → Plugins shows it.
type CatalogPlugin struct {
	CatalogEntry
	Catalog   string `json:"catalog"`             // the catalogue's URL
	Available bool   `json:"available"`           // has a download for this server's platform
	Installed string `json:"installed,omitempty"` // "", "bundled" or "installed"
	Current   string `json:"current,omitempty"`   // installed version
	Update    bool   `json:"update"`              // the catalogue has a newer version
}

// CatalogError is a catalogue that couldn't be read.
type CatalogError struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

// Platform is the download key for this server.
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

const (
	catalogTTL      = 10 * time.Minute
	maxCatalogBytes = 2 << 20
	maxPluginBytes  = 256 << 20
)

type catalogCache struct {
	mu   sync.Mutex
	at   map[string]time.Time
	data map[string]*Catalog
}

func (h *Host) fetchCatalog(url string, fresh bool) (*Catalog, error) {
	c := &h.catalogs
	c.mu.Lock()
	if c.data == nil {
		c.data, c.at = map[string]*Catalog{}, map[string]time.Time{}
	}
	if cat := c.data[url]; cat != nil && !fresh && time.Since(c.at[url]) < catalogTTL {
		c.mu.Unlock()
		return cat, nil
	}
	c.mu.Unlock()
	resp, err := h.downloader().Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	var cat Catalog
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxCatalogBytes)).Decode(&cat); err != nil {
		return nil, fmt.Errorf("not a plugin catalogue: %w", err)
	}
	c.mu.Lock()
	c.data[url], c.at[url] = &cat, time.Now()
	c.mu.Unlock()
	return &cat, nil
}

// downloader follows redirects (GitHub release downloads redirect) and allows
// time for large files.
func (h *Host) downloader() *http.Client {
	if h.Download != nil {
		return h.Download
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// Catalogs reads every catalogue and marks what is installed. A catalogue
// that can't be read is reported and the rest still listed.
func (h *Host) Catalogs(urls []string, fresh bool) ([]CatalogPlugin, []CatalogError) {
	h.mu.Lock()
	found := h.found
	h.mu.Unlock()
	out := []CatalogPlugin{}
	errs := []CatalogError{}
	seen := map[string]bool{}
	for _, url := range urls {
		cat, err := h.fetchCatalog(url, fresh)
		if err != nil {
			errs = append(errs, CatalogError{URL: url, Error: err.Error()})
			continue
		}
		for _, e := range cat.Plugins {
			if !idPattern.MatchString(e.ID) || seen[e.ID] {
				continue // the first catalogue to list an id wins
			}
			seen[e.ID] = true
			cp := CatalogPlugin{CatalogEntry: e, Catalog: url}
			if cp.Name == "" {
				cp.Name = e.ID
			}
			_, cp.Available = e.Downloads[Platform()]
			if b := found[e.ID]; b != nil {
				cp.Installed = "bundled"
				if h.inInstallDir(b.dir) {
					cp.Installed = "installed"
				}
				cp.Current = b.manifest.Version
				cp.Update = newerVersion(e.Version, b.manifest.Version)
			}
			out = append(out, cp)
		}
	}
	return out, errs
}

func (h *Host) inInstallDir(dir string) bool {
	if h.InstallDir == "" {
		return false
	}
	rel, err := filepath.Rel(h.InstallDir, dir)
	return err == nil && !strings.HasPrefix(rel, "..") && rel != "."
}

// Install downloads a plugin from a catalogue into InstallDir, replacing an
// earlier copy there, and restarts it if it was on. New plugins start off.
func (h *Host) Install(catalogURL, id string) error {
	if h.InstallDir == "" {
		return errors.New("this server has no folder to install plugins into")
	}
	h.installMu.Lock()
	defer h.installMu.Unlock()
	cat, err := h.fetchCatalog(catalogURL, false)
	if err != nil {
		return err
	}
	var entry *CatalogEntry
	for i := range cat.Plugins {
		if cat.Plugins[i].ID == id {
			entry = &cat.Plugins[i]
			break
		}
	}
	if entry == nil || !idPattern.MatchString(id) {
		return fmt.Errorf("the catalogue has no plugin %q", id)
	}
	dl, ok := entry.Downloads[Platform()]
	if !ok {
		return fmt.Errorf("%s has no download for %s", entry.Name, Platform())
	}
	want, err := hex.DecodeString(strings.ToLower(dl.SHA256))
	if err != nil || len(want) != sha256.Size {
		return errors.New("the catalogue gives no valid sha256 for this download, so it can't be checked")
	}
	if err := os.MkdirAll(h.InstallDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(h.InstallDir, ".download-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	resp, err := h.downloader().Get(dl.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", resp.Status)
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(resp.Body, maxPluginBytes+1))
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if n > maxPluginBytes {
		return errors.New("the download is too big")
	}
	if got := sum.Sum(nil); !bytes.Equal(got, want) {
		return errors.New("the download doesn't match the catalogue's checksum")
	}

	staging, err := os.MkdirTemp(h.InstallDir, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	root, err := unzip(tmp.Name(), staging)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(root, "plugin.json"))
	if err != nil {
		return errors.New("the download has no plugin.json")
	}
	m, err := parseManifest(b)
	if err != nil {
		return err
	}
	if m.ID != id {
		return fmt.Errorf("the download is plugin %q, not %q", m.ID, id)
	}
	if len(m.Command) == 0 {
		return errors.New("the download's plugin.json has no command")
	}

	// Swap folders with the plugin stopped, so no process runs from a
	// folder being replaced.
	dest := filepath.Join(h.InstallDir, id)
	h.stopAndWait(id)
	old := ""
	if _, err := os.Stat(dest); err == nil {
		old = dest + ".old-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		if err := os.Rename(dest, old); err != nil {
			h.Sync()
			return err
		}
	}
	if err := os.Rename(root, dest); err != nil {
		if old != "" {
			os.Rename(old, dest)
		}
		h.Sync()
		return err
	}
	if old != "" {
		os.RemoveAll(old)
	}
	h.Sync()
	return nil
}

// Uninstall deletes a plugin installed from a catalogue. A bundled copy with
// the same id, if any, takes its place (off until turned on again).
func (h *Host) Uninstall(id string) error {
	h.installMu.Lock()
	defer h.installMu.Unlock()
	h.mu.Lock()
	b := h.found[id]
	h.mu.Unlock()
	if b == nil || !h.inInstallDir(b.dir) {
		return errors.New("only plugins installed from a catalogue can be uninstalled here")
	}
	h.stopAndWait(id)
	if err := os.RemoveAll(b.dir); err != nil {
		return err
	}
	h.Store.DeletePlugin(id)
	h.Sync()
	return nil
}

// stopAndWait stops a plugin and gives its process a moment to exit.
func (h *Host) stopAndWait(id string) {
	h.mu.Lock()
	in := h.inst[id]
	h.stopLocked(id)
	h.mu.Unlock()
	if in == nil {
		return
	}
	select {
	case <-in.done:
	case <-time.After(10 * time.Second):
	}
}

// unzip extracts a plugin zip into dir and returns the folder holding
// plugin.json: dir itself, or the zip's single top-level folder.
func unzip(zipPath, dir string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("not a zip: %w", err)
	}
	defer zr.Close()
	var total int64
	for _, f := range zr.File {
		name := filepath.FromSlash(f.Name)
		if !filepath.IsLocal(name) {
			return "", fmt.Errorf("the zip has an unsafe path %q", f.Name)
		}
		target := filepath.Join(dir, name)
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
			continue
		case !mode.IsRegular():
			return "", fmt.Errorf("the zip has a link or special file %q", f.Name)
		}
		total += int64(f.UncompressedSize64)
		if total > 4*maxPluginBytes {
			return "", errors.New("the zip is too big unpacked")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		perm := os.FileMode(0o644)
		if mode&0o111 != 0 {
			perm = 0o755
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
		if err == nil {
			_, err = io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
			if cerr := out.Close(); err == nil {
				err = cerr
			}
		}
		rc.Close()
		if err != nil {
			return "", err
		}
	}
	if fileExists(filepath.Join(dir, "plugin.json")) {
		return dir, nil
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) == 1 && entries[0].IsDir() {
		root := filepath.Join(dir, entries[0].Name())
		// The command may not have kept its executable bit (zips made on Windows).
		if b, err := os.ReadFile(filepath.Join(root, "plugin.json")); err == nil {
			if m, err := parseManifest(b); err == nil && len(m.Command) > 0 && !filepath.IsAbs(m.Command[0]) {
				for _, c := range []string{m.Command[0], m.Command[0] + ".exe"} {
					p := filepath.Join(root, filepath.FromSlash(c))
					if filepath.IsLocal(filepath.FromSlash(c)) && fileExists(p) {
						os.Chmod(p, 0o755)
					}
				}
			}
		}
		return root, nil
	}
	return dir, nil
}

// newerVersion reports whether version a is newer than b ("v1.2.0" style;
// anything unparsable counts as older, so an unknown installed version can
// always be updated).
func newerVersion(a, b string) bool {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if !oka {
		return false
	}
	if !okb {
		return true
	}
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if v == "" || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
