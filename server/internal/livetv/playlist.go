package livetv

import (
	"bytes"
	"strconv"
	"strings"
)

// segmentNumber is the N of a playlist line "sNNNNNN.ts", or -1.
func segmentNumber(line string) int {
	if !strings.HasPrefix(line, "s") || !strings.HasSuffix(line, ".ts") {
		return -1
	}
	n, err := strconv.Atoi(line[1 : len(line)-3])
	if err != nil {
		return -1
	}
	return n
}

// lastSegment is the number of the last segment in a playlist file, or -1.
func lastSegment(path string) int {
	b, err := readShared(path)
	if err != nil {
		return -1
	}
	last := -1
	for _, line := range strings.Split(string(b), "\n") {
		if n := segmentNumber(strings.TrimSpace(line)); n >= 0 {
			last = n
		}
	}
	return last
}

// Playlist rewrites a channel's live playlist as ffmpeg wrote it so players
// are told where the stream's timestamps start over: after a switch to
// another upstream, each output carries on the same playlist, and its first
// segment is marked with EXT-X-DISCONTINUITY. ffmpeg's own marking on an
// appended playlist is unreliable and never counts the discontinuities that
// slid out of the window, which players need (EXT-X-DISCONTINUITY-SEQUENCE)
// to line segments up across reloads.
func (m *Manager) Playlist(channelID int64, data []byte) []byte {
	r := m.relay(channelID)
	if r == nil {
		return data
	}
	r.mu.Lock()
	breaks := append([]int(nil), r.hlsBreaks...)
	r.mu.Unlock()
	return markDiscontinuities(data, breaks)
}

func markDiscontinuities(data []byte, breaks []int) []byte {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	first := -1
	for _, l := range lines {
		if n := segmentNumber(strings.TrimSpace(l)); n >= 0 {
			first = n
			break
		}
	}
	isBreak := map[int]bool{}
	seq := 0
	for _, b := range breaks {
		if b <= first {
			seq++ // slid out of the window (or starts it): counted, not tagged
		} else {
			isBreak[b] = true
		}
	}
	var out bytes.Buffer
	var pending []string // tags between the last segment and the next one
	flush := func(seg int) {
		if isBreak[seg] {
			out.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		for _, p := range pending {
			out.WriteString(p + "\n")
		}
		pending = nil
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case t == "#EXT-X-DISCONTINUITY", strings.HasPrefix(t, "#EXT-X-DISCONTINUITY-SEQUENCE:"):
			// replaced by ours
		case strings.HasPrefix(t, "#EXT-X-MEDIA-SEQUENCE:"):
			out.WriteString(t + "\n")
			out.WriteString("#EXT-X-DISCONTINUITY-SEQUENCE:" + strconv.Itoa(seq) + "\n")
		case strings.HasPrefix(t, "#EXTINF"):
			pending = append(pending, t)
		case segmentNumber(t) >= 0:
			flush(segmentNumber(t))
			out.WriteString(t + "\n")
		case len(pending) > 0:
			pending = append(pending, t)
		default:
			out.WriteString(t + "\n")
		}
	}
	for _, p := range pending {
		out.WriteString(p + "\n")
	}
	return out.Bytes()
}
