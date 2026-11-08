package livetv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// LogoLibrary stores logos admins upload. Files are content-addressed, so the
// same image is stored once however many times it's uploaded.
type LogoLibrary struct{ dir string }

func NewLogoLibrary(dir string) *LogoLibrary { return &LogoLibrary{dir: dir} }

var reLibraryFile = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|svg|webp|gif)$`)

// Save stores an image (PNG, JPEG, SVG, WebP or GIF, up to 2 MB) and returns its file name.
func (l *LogoLibrary) Save(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxLogoBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxLogoBytes {
		return "", fmt.Errorf("logos can be up to %d MB", maxLogoBytes>>20)
	}
	ext := extOf("", data)
	if ext == "" {
		return "", fmt.Errorf("only PNG, JPEG, SVG, WebP and GIF images are supported")
	}
	if ext == ".png" {
		data = trimTransparent(data)
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:16]) + ext
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(l.dir, name)
	if _, err := os.Stat(path); err != nil {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return "", err
		}
	}
	return name, nil
}

// trimTransparent crops the see-through margin many logo packs pad their PNGs
// with, so the logo itself fills whatever box it's shown in instead of sitting
// small in the middle of it. Anything it can't decode, or with nothing worth
// trimming, comes back unchanged.
func trimTransparent(data []byte) []byte {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	b := img.Bounds()
	const solid = 0x0800 // alpha (of 0xffff) below this is invisible antialiasing dust
	box := image.Rectangle{Min: b.Max, Max: b.Min}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a >= solid {
				box.Min.X, box.Min.Y = min(box.Min.X, x), min(box.Min.Y, y)
				box.Max.X, box.Max.Y = max(box.Max.X, x+1), max(box.Max.Y, y+1)
			}
		}
	}
	if box.Empty() {
		return data // fully transparent: nothing to fit
	}
	// Keep a hair of breathing room so edges aren't flush with the crop.
	pad := max(box.Dx(), box.Dy()) / 50
	box = image.Rect(box.Min.X-pad, box.Min.Y-pad, box.Max.X+pad, box.Max.Y+pad).Intersect(b)
	// Only bother when it removes a noticeable margin.
	if box.Dx()*box.Dy() > b.Dx()*b.Dy()*9/10 {
		return data
	}
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return data
	}
	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, sub.SubImage(box)); err != nil || out.Len() > maxLogoBytes {
		return data
	}
	return out.Bytes()
}

// Path resolves a library file name to its path and content type.
func (l *LogoLibrary) Path(file string) (string, string, error) {
	if !reLibraryFile.MatchString(file) {
		return "", "", os.ErrNotExist
	}
	return filepath.Join(l.dir, file), typeOf(filepath.Ext(file)), nil
}

func (l *LogoLibrary) Remove(file string) {
	if reLibraryFile.MatchString(file) {
		os.Remove(filepath.Join(l.dir, file))
	}
}

var (
	reNamePrefix  = regexp.MustCompile(`^\s*[A-Za-z]{2,4}\s*[:|]\s*`) // "US: ", "CA | "
	reNameBracket = regexp.MustCompile(`[(\[{][^)\]}]*[)\]}]`)        // "(East)", "[HD]"
	reNameQuality = regexp.MustCompile(`(?i)\b(hd|fhd|uhd|sd|4k|hevc|h265|1080p?|720p?)\b`)
	reNameJunk    = regexp.MustCompile(`[^a-z0-9]+`)
)

// MatchName reduces a channel or logo file name to what identifies the channel,
// so "US: CNN HD", "CNN (East)" and "cnn.png" all become "cnn".
func MatchName(s string) string {
	s = strings.TrimSuffix(s, filepath.Ext(s))
	s = reNamePrefix.ReplaceAllString(s, "")
	s = reNameBracket.ReplaceAllString(s, " ")
	s = reNameQuality.ReplaceAllString(s, " ")
	return reNameJunk.ReplaceAllString(strings.ToLower(s), "")
}
