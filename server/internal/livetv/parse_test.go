package livetv

import (
	"strings"
	"testing"
	"time"
)

func TestParseM3U(t *testing.T) {
	src := "\ufeff#EXTM3U x-tvg-url=\"http://x/epg.xml\"\n" +
		`#EXTINF:-1 tvg-id="espn.us" tvg-name="ESPN" tvg-logo="http://logo/espn.png" group-title="Sports, US" tvg-chno="206",ESPN HD` + "\n" +
		"#EXTVLCOPT:http-user-agent=VLC\n" +
		"http://provider/live/1.ts\n" +
		"#EXTINF:-1,Local News\n#EXTGRP:Local\n" +
		"http://provider/live/2.ts\n" +
		"http://provider/bare.ts\n"
	var got []Entry
	if err := ParseM3U(strings.NewReader(src), func(e Entry) error { got = append(got, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	e := got[0]
	if e.Name != "ESPN HD" || e.TvgID != "espn.us" || e.Group != "Sports, US" || e.TvgChno != "206" || e.Logo != "http://logo/espn.png" || e.URL != "http://provider/live/1.ts" {
		t.Errorf("entry 0 = %+v", e)
	}
	if got[1].Name != "Local News" || got[1].Group != "Local" {
		t.Errorf("entry 1 = %+v", got[1])
	}
	if got[2].URL != "http://provider/bare.ts" || got[2].Name == "" {
		t.Errorf("entry 2 = %+v", got[2])
	}
}

func TestParseXMLTV(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="espn.us"><display-name>ESPN</display-name><icon src="http://logo/espn.png"/></channel>
  <programme start="20261023180000 +0000" stop="20261023190000 +0000" channel="espn.us">
    <title lang="en">SportsCenter</title><sub-title>Evening</sub-title><desc>Highlights.</desc>
    <category>Sports</category><episode-num system="xmltv_ns">0.1.</episode-num><episode-num system="onscreen">S1E2</episode-num>
  </programme>
  <programme start="20200101000000 +0000" stop="20200101010000 +0000" channel="espn.us"><title>Old</title></programme>
</tv>`
	from := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	var chans []GuideChannel
	var progs []Program
	err := ParseXMLTV(strings.NewReader(src), from, from.AddDate(0, 0, 7),
		func(c GuideChannel) error { chans = append(chans, c); return nil },
		func(p Program) error { progs = append(progs, p); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(chans) != 1 || chans[0].DisplayName != "ESPN" || chans[0].Icon != "http://logo/espn.png" {
		t.Errorf("channels = %+v", chans)
	}
	if len(progs) != 1 {
		t.Fatalf("programs = %+v (old program should be skipped)", progs)
	}
	p := progs[0]
	if p.Title != "SportsCenter" || p.Subtitle != "Evening" || p.Episode != "S1E2" || p.Category != "Sports" ||
		!p.Start.Equal(time.Date(2026, 10, 23, 18, 0, 0, 0, time.UTC)) || p.Stop.Sub(p.Start) != time.Hour {
		t.Errorf("program = %+v", p)
	}
}
