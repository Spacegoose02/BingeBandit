package api

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
)

// Players that can't send headers (the Roku app) open HLS with ?token=, but
// the segment and variant URIs inside a playlist are relative, so their
// requests would arrive without it. For those requests the playlist is
// rewritten to carry the token on every URI.

// playlistQuery is what to append to the URIs in a playlist served for r, or
// "" when the request authenticated some other way. fromStart is passed on so
// a master playlist's variants also get EXT-X-START.
func playlistQuery(r *http.Request) string {
	token, _, _ := strings.Cut(r.URL.Query().Get("token"), "?")
	if token == "" {
		return ""
	}
	q := url.Values{"token": {token}}
	if r.URL.Query().Get("fromStart") == "1" {
		q.Set("fromStart", "1")
	}
	return q.Encode()
}

// rewritePlaylist appends query (if any) to every URI in an HLS playlist
// (plain URI lines and URI="..." attributes). With fromStart, a media playlist gets
// EXT-X-START at 0: a transcode's playlist is an EVENT playlist that grows
// while it plays, and some players otherwise join it near the end like a
// live stream, skipping what came before.
func rewritePlaylist(b []byte, query string, fromStart bool) []byte {
	add := func(uri string) string {
		if uri == "" || query == "" || strings.Contains(uri, "token=") {
			return uri
		}
		if strings.Contains(uri, "?") {
			return uri + "&" + query
		}
		return uri + "?" + query
	}
	media := bytes.Contains(b, []byte("#EXTINF")) || bytes.Contains(b, []byte("#EXT-X-TARGETDURATION"))
	hasStart := bytes.Contains(b, []byte("#EXT-X-START"))
	var out strings.Builder
	for i, line := range strings.Split(string(b), "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		trimmed := strings.TrimRight(line, "\r")
		switch {
		case trimmed == "":
			out.WriteString(line)
		case strings.HasPrefix(trimmed, "#"):
			if at := strings.Index(trimmed, `URI="`); at >= 0 {
				start := at + len(`URI="`)
				if end := strings.IndexByte(trimmed[start:], '"'); end >= 0 {
					trimmed = trimmed[:start] + add(trimmed[start:start+end]) + trimmed[start+end:]
				}
			}
			out.WriteString(trimmed)
			if trimmed == "#EXTM3U" && fromStart && media && !hasStart {
				out.WriteString("\n#EXT-X-START:TIME-OFFSET=0,PRECISE=YES")
			}
		default:
			out.WriteString(add(trimmed))
		}
	}
	return []byte(out.String())
}
