package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRewritePlaylistAddsTokenToURIs(t *testing.T) {
	in := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXT-X-PLAYLIST-TYPE:EVENT\n#EXTINF:4.0,\nseg00000.ts\n#EXTINF:4.0,\nseg00001.ts?v=2\n"
	got := string(rewritePlaylist([]byte(in), "token=abc", true))
	for _, want := range []string{"\nseg00000.ts?token=abc\n", "\nseg00001.ts?v=2&token=abc\n", "#EXTM3U\n#EXT-X-START:TIME-OFFSET=0,PRECISE=YES\n#EXT-X-VERSION:3"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestRewritePlaylistMasterAndAttributes(t *testing.T) {
	in := "#EXTM3U\r\n#EXT-X-STREAM-INF:BANDWIDTH=800000\r\nv1_index.m3u8\r\n#EXT-X-MAP:URI=\"init.mp4\"\r\n"
	got := string(rewritePlaylist([]byte(in), "token=abc&fromStart=1", true))
	if strings.Contains(got, "EXT-X-START") {
		t.Errorf("a master playlist shouldn't get EXT-X-START:\n%s", got)
	}
	if !strings.Contains(got, "\nv1_index.m3u8?token=abc&fromStart=1\n") || !strings.Contains(got, `URI="init.mp4?token=abc&fromStart=1"`) {
		t.Errorf("URIs not rewritten:\n%s", got)
	}
}

func TestPlaylistQueryOnlyForTokenRequests(t *testing.T) {
	if q := playlistQuery(httptest.NewRequest("GET", "/api/transcode/x/index.m3u8", nil)); q != "" {
		t.Errorf("no token: got %q", q)
	}
	if q := playlistQuery(httptest.NewRequest("GET", "/api/transcode/x/index.m3u8?token=a+b&fromStart=1", nil)); q != "fromStart=1&token=a+b" {
		t.Errorf("got %q", q)
	}
}
