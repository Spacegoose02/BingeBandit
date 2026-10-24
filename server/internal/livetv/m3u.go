// Package livetv imports IPTV playlists (M3U) and guides (XMLTV), and relays
// live channels to viewers over one shared upstream connection per channel.
package livetv

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

// Entry is one stream listed in an M3U playlist.
type Entry struct {
	Name    string
	URL     string
	Logo    string
	Group   string
	TvgID   string
	TvgChno string
}

var reAttr = regexp.MustCompile(`([A-Za-z0-9_-]+)="([^"]*)"`)

// ParseM3U reads an extended M3U playlist and calls fn for each entry.
// It streams, so provider playlists with tens of thousands of entries are fine.
func ParseM3U(r io.Reader, fn func(Entry) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var cur *Entry
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		switch {
		case line == "" || strings.HasPrefix(line, "#EXTM3U"):
		case strings.HasPrefix(line, "#EXTINF:"):
			e := Entry{}
			// The display name follows the last comma outside quoted attributes.
			info := line[len("#EXTINF:"):]
			inQuote, comma := false, -1
			for i, r := range info {
				switch {
				case r == '"':
					inQuote = !inQuote
				case r == ',' && !inQuote:
					comma = i
				}
			}
			if comma >= 0 {
				e.Name = strings.TrimSpace(info[comma+1:])
				info = info[:comma]
			}
			for _, m := range reAttr.FindAllStringSubmatch(info, -1) {
				switch strings.ToLower(m[1]) {
				case "tvg-id":
					e.TvgID = m[2]
				case "tvg-name":
					if e.Name == "" {
						e.Name = m[2]
					}
				case "tvg-logo":
					e.Logo = m[2]
				case "group-title":
					e.Group = m[2]
				case "tvg-chno", "channel-number":
					e.TvgChno = m[2]
				}
			}
			cur = &e
		case strings.HasPrefix(line, "#EXTGRP:") && cur != nil:
			if cur.Group == "" {
				cur.Group = strings.TrimSpace(line[len("#EXTGRP:"):])
			}
		case strings.HasPrefix(line, "#"):
			// Other directives (#EXTVLCOPT etc.) are ignored.
		default:
			if cur == nil {
				cur = &Entry{Name: line}
			}
			cur.URL = line
			if cur.Name == "" {
				cur.Name = line
			}
			if err := fn(*cur); err != nil {
				return err
			}
			cur = nil
		}
	}
	return sc.Err()
}
