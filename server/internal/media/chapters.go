package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Chapter is one chapter mark of a file, in seconds from the file's start.
type Chapter struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Title string  `json:"title,omitempty"`
}

type chapterOutput struct {
	Chapters []struct {
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

// Chapters reads the chapter marks of the file at path. ffprobe only reads
// the container's header for these, so it's quick even for large files.
func Chapters(ctx context.Context, ffprobe, path string) ([]Chapter, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "quiet", "-print_format", "json", "-show_chapters", path)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe chapters %s: %w", path, err)
	}
	return parseChapters(out)
}

func parseChapters(out []byte) ([]Chapter, error) {
	var p chapterOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("ffprobe output: %w", err)
	}
	chapters := []Chapter{}
	for _, c := range p.Chapters {
		start, err1 := strconv.ParseFloat(c.StartTime, 64)
		end, err2 := strconv.ParseFloat(c.EndTime, 64)
		if err1 != nil || err2 != nil || end <= start {
			continue
		}
		if start < 0 {
			start = 0
		}
		chapters = append(chapters, Chapter{Start: start, End: end, Title: chapterTitle(c.Tags)})
	}
	// A single chapter covering the whole file tells the viewer nothing.
	if len(chapters) < 2 {
		return []Chapter{}, nil
	}
	return chapters, nil
}

// chapterTitle is the chapter's title tag, unless it's a bare timestamp
// ("00:05:00.000") or number, which some muxers write in place of a name;
// players number unnamed chapters themselves.
func chapterTitle(tags map[string]string) string {
	t := strings.TrimSpace(tags["title"])
	if t == "" {
		for k, v := range tags {
			if strings.EqualFold(k, "title") {
				t = strings.TrimSpace(v)
			}
		}
	}
	if t == "" {
		return ""
	}
	if strings.Trim(t, "0123456789:.,") == "" {
		return ""
	}
	return t
}
