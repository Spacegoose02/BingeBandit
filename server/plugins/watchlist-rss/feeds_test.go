package main

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestParseFeed(t *testing.T) {
	feed := `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:letterboxd="https://letterboxd.com" xmlns:tmdb="https://themoviedb.org">
<channel><title>A list</title>
  <item>
    <title>Parasite, 2019 - ★★★★★</title>
    <link>https://letterboxd.com/x/film/parasite-2019/</link>
    <guid isPermaLink="false">letterboxd-review-1</guid>
    <letterboxd:filmTitle>Parasite</letterboxd:filmTitle>
    <letterboxd:filmYear>2019</letterboxd:filmYear>
    <tmdb:movieId>496243</tmdb:movieId>
  </item>
  <item><title>The Bear (2022)</title><link>https://example.com/the-bear</link></item>
  <item><title>Severance</title><tmdb:tvId>95396</tmdb:tvId></item>
  <item><title>Heat, 1995</title></item>
  <item><title></title></item>
</channel></rss>`
	got, err := parseFeed([]byte(feed))
	if err != nil {
		t.Fatal(err)
	}
	want := []entry{
		{Key: "letterboxd-review-1", Title: "Parasite", Year: 2019, TMDB: 496243, Kind: "movie"},
		{Key: "https://example.com/the-bear", Title: "The Bear", Year: 2022},
		{Key: "Severance", Title: "Severance", TMDB: 95396, Kind: "show"},
		{Key: "Heat, 1995", Title: "Heat", Year: 1995},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	for i, w := range want {
		g := got[i]
		g.Origin = ""
		if g != w {
			t.Errorf("entry %d: got %+v, want %+v", i, g, w)
		}
	}
	if _, err := parseFeed([]byte("<html>not a feed")); err == nil {
		t.Error("HTML should not parse as a feed")
	}
}

func TestBuildFeed(t *testing.T) {
	b, err := buildFeed("ann", "https://tv.example.com", []libItem{
		{ID: 7, Kind: "movie", Title: "Heat & Dust", Year: 1983, TMDBID: 42, Overview: "<b>x</b>"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"<title>ann&#39;s watchlist</title>",
		"<title>Heat &amp; Dust (1983)</title>",
		"<link>https://tv.example.com/item/7</link>",
		`<guid isPermaLink="false">bingebandit-item-7</guid>`,
		"<bingebandit:tmdbId>42</bingebandit:tmdbId>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("feed lacks %s:\n%s", want, s)
		}
	}
	// It reads back as RSS, and its own items parse as entries.
	if err := xml.Unmarshal(b, new(rssDoc)); err != nil {
		t.Fatal(err)
	}
	entries, _ := parseFeed(b)
	if len(entries) != 1 || entries[0].Title != "Heat & Dust" || entries[0].Year != 1983 {
		t.Errorf("round trip: %+v", entries)
	}
}
