package api

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// Widths a local file is scaled down to; anything else (original) is sent as is.
var localWidths = map[string]int{"w92": 92, "w185": 185, "w342": 342, "w500": 500, "w780": 780, "w1280": 1280}

// sizedLocalImage returns a copy of a local artwork file scaled down to the
// asked width (?w=), kept in <data>/cache/local/<width>/. Local files are
// often full-size uploads (TPDb, Kometa, Plex), which a TV box would
// otherwise decode at full size for every poster in a grid. It returns the
// original path when there's nothing to scale or scaling fails.
func (s *Server) sizedLocalImage(r *http.Request, path, file string) string {
	width, ok := localWidths[r.URL.Query().Get("w")]
	if !ok || s.DataDir == "" {
		return path
	}
	ext := ".jpg"
	if strings.HasSuffix(file, ".png") {
		ext = ".png" // keeps the transparency of logos
	}
	dir := filepath.Join(s.DataDir, "cache", "local", strconv.Itoa(width))
	out := filepath.Join(dir, strings.TrimSuffix(file, filepath.Ext(file))+ext)
	if _, err := os.Stat(out); err == nil {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return path
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= width {
		return path
	}
	// Scaling is the slow part; share the overlay drawing slots so a page of
	// posters doesn't decode dozens of big files at once.
	s.overlays.init()
	select {
	case s.overlays.slots <- struct{}{}:
		defer func() { <-s.overlays.slots }()
	case <-r.Context().Done():
		return path
	}
	if _, err := os.Stat(out); err == nil {
		return out // drawn while this one waited
	}
	if _, err := f.Seek(0, 0); err != nil {
		return path
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return path
	}
	b := src.Bounds()
	h := max(1, b.Dy()*width/b.Dx())
	dst := image.NewRGBA(image.Rect(0, 0, width, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	var buf bytes.Buffer
	if ext == ".png" {
		err = png.Encode(&buf, dst)
	} else {
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 88})
	}
	if err != nil {
		return path
	}
	os.MkdirAll(dir, 0o755)
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil || os.Rename(tmp, out) != nil {
		os.Remove(tmp)
		return path
	}
	return out
}
