package livetv

import (
	"strings"
	"testing"
)

func TestMarkDiscontinuities(t *testing.T) {
	// What ffmpeg writes after two appends: stray tags at the top and none
	// counting what slid out.
	in := `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:5
#EXT-X-DISCONTINUITY
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-DISCONTINUITY
#EXTINF:2.000000,
s000005.ts
#EXTINF:2.000000,
s000006.ts
#EXT-X-DISCONTINUITY
#EXTINF:2.000000,
s000007.ts
`
	got := string(markDiscontinuities([]byte(in), []int{3, 6}))
	want := `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:5
#EXT-X-DISCONTINUITY-SEQUENCE:1
#EXT-X-INDEPENDENT-SEGMENTS
#EXTINF:2.000000,
s000005.ts
#EXT-X-DISCONTINUITY
#EXTINF:2.000000,
s000006.ts
#EXTINF:2.000000,
s000007.ts
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// Once the break reaches the top it is counted instead of tagged.
	slid := strings.Replace(strings.Replace(in, "SEQUENCE:5", "SEQUENCE:6", 1), "#EXTINF:2.000000,\ns000005.ts\n", "", 1)
	got = string(markDiscontinuities([]byte(slid), []int{3, 6}))
	if !strings.Contains(got, "DISCONTINUITY-SEQUENCE:2") || strings.Contains(got, "#EXT-X-DISCONTINUITY\n") {
		t.Errorf("break at the first segment:\n%s", got)
	}
}
