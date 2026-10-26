package library

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestExtraFiles(t *testing.T) {
	for name, want := range map[string]bool{
		"Heat (1995)-trailer.mkv":           true,
		"Heat (1995).trailer.mp4":           true,
		"trailer.mp4":                       true,
		"Heat (1995)-behindthescenes.mkv":   true,
		"Heat (1995).mkv":                   false,
		"The Other (1972).mkv":              false,
		"Show - S01E02 - The Other.mkv":     false,
		"Show.S01E02.The.Other.mkv":         false,
		"Show - S01E03 - Deleted.mkv":       false,
		"Trailer Park Boys - S01E01.mkv":    false,
		"Short Circuit (1986).mkv":          false,
		"Show - S02E01 - Lights -Other.mkv": false, // a space before the dash: part of a title
	} {
		if got := isExtraFile(name); got != want {
			t.Errorf("isExtraFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestLocalTrailers(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"Heat (1995).mkv", "Heat (1995)-trailer.mp4", "Heat (1995)-featurette.mkv", "notes.txt", "Trailers/Teaser.mkv", "Trailers/poster.jpg"} {
		p := filepath.Join(dir, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}
	got := LocalTrailers(filepath.Join(dir, "Heat (1995).mkv"))
	want := []string{filepath.Join(dir, "Heat (1995)-trailer.mp4"), filepath.Join(dir, "Trailers", "Teaser.mkv")}
	if !slices.Equal(got, want) {
		t.Errorf("LocalTrailers = %v, want %v", got, want)
	}
}
