package library

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Plex and Jellyfin name extras that sit next to the movie with a suffix:
// "Heat (1995)-trailer.mkv", "Heat (1995).trailer.mp4", or just "trailer.mp4".
var extraSuffixes = []string{"trailer", "behindthescenes", "deleted", "featurette", "interview", "scene", "short", "other", "extra"}

// extraKind reports whether a video file is an extra by its name, and which.
// Only Plex's exact "-<kind>" suffix counts, plus ".trailer" and "trailer"
// on their own, so an episode called "The Other" stays an episode.
func extraKind(name string) (string, bool) {
	base := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	if base == "trailer" || strings.HasSuffix(base, ".trailer") || strings.HasSuffix(base, "_trailer") {
		return "trailer", true
	}
	for _, k := range extraSuffixes {
		if strings.HasSuffix(base, "-"+k) && !strings.HasSuffix(base, " -"+k) {
			return k, true
		}
	}
	return "", false
}

// isExtraFile is true for trailers and other extras named like one, which
// must not become a movie version or an episode.
func isExtraFile(name string) bool {
	_, ok := extraKind(name)
	return ok
}

// LocalTrailers lists the trailer files for a movie file: "<name>-trailer.*"
// (and "trailer.*") beside it, and the videos in a "Trailers" folder next to
// it. Paths are sorted so an index into the list stays stable.
func LocalTrailers(movieFile string) []string {
	dir := filepath.Dir(movieFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if strings.EqualFold(name, "trailers") {
				sub, _ := os.ReadDir(filepath.Join(dir, name))
				for _, f := range sub {
					if !f.IsDir() && isVideo(f.Name()) {
						out = append(out, filepath.Join(dir, name, f.Name()))
					}
				}
			}
			continue
		}
		if k, ok := extraKind(name); ok && k == "trailer" && isVideo(name) {
			out = append(out, filepath.Join(dir, name))
		}
	}
	slices.Sort(out)
	return out
}
