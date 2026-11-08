package livetv

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestMatchName(t *testing.T) {
	for in, want := range map[string]string{
		"US: CNN HD":      "cnn",
		"CNN (East)":      "cnn",
		"cnn.png":         "cnn",
		"CA | TSN 1 FHD":  "tsn1",
		"TSN1.svg":        "tsn1",
		"Sky Sports [4K]": "skysports",
		"HGTV":            "hgtv", // "hd" inside a word stays
	} {
		if got := MatchName(in); got != want {
			t.Errorf("MatchName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLogoLibrarySave(t *testing.T) {
	lib := NewLogoLibrary(t.TempDir())
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	a, err := lib.Save(bytes.NewReader(png))
	if err != nil || !strings.HasSuffix(a, ".png") {
		t.Fatalf("Save png = %q, %v", a, err)
	}
	if b, _ := lib.Save(bytes.NewReader(png)); b != a {
		t.Fatalf("same image saved twice as %q and %q", a, b)
	}
	if s, err := lib.Save(strings.NewReader(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`)); err != nil || !strings.HasSuffix(s, ".svg") {
		t.Fatalf("Save svg = %q, %v", s, err)
	}
	if _, err := lib.Save(strings.NewReader("not an image")); err == nil {
		t.Fatal("text accepted as a logo")
	}
	if _, _, err := lib.Path("../etc/passwd"); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestTrimTransparent(t *testing.T) {
	// A 10x4 logo in the middle of a 200x200 transparent canvas.
	src := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	for y := 98; y < 102; y++ {
		for x := 95; x < 105; x++ {
			src.Set(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	out, err := png.Decode(bytes.NewReader(trimTransparent(buf.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	if b := out.Bounds(); b.Dx() != 10 || b.Dy() != 4 {
		t.Fatalf("trimmed to %v, want 10x4", b)
	}

	// A logo that already fills its canvas is left byte-for-byte alone.
	full := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	for i := range full.Pix {
		full.Pix[i] = 255
	}
	buf.Reset()
	png.Encode(&buf, full)
	if got := trimTransparent(buf.Bytes()); !bytes.Equal(got, buf.Bytes()) {
		t.Fatal("a logo with no margin was re-encoded")
	}
}
