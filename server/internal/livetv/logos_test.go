package livetv

import (
	"bytes"
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
