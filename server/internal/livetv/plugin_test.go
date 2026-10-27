package livetv

import (
	"path/filepath"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func TestSyncPluginChannels(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// An existing channel keeps its number.
	st.CreateChannel(&store.Channel{Number: 5, Name: "News", Enabled: true}, nil)
	a := PluginChannel{Key: "UCa", Name: "Alice", URL: "https://www.youtube.com/channel/UCa/live", Live: true, Title: "Speedrun"}
	b := PluginChannel{Key: "UCb", Name: "Bob", URL: "https://www.youtube.com/channel/UCb/live"}
	if err := SyncPluginChannels(st, "youtube-live", "YouTube Live", []PluginChannel{a, b}); err != nil {
		t.Fatal(err)
	}
	byName := func() map[string]*store.Channel {
		chans, _ := st.Channels(0, true)
		out := map[string]*store.Channel{}
		for _, c := range chans {
			out[c.Name] = c
		}
		return out
	}
	ch := byName()
	if len(ch) != 3 || !ch["Alice"].Enabled || ch["Bob"].Enabled || ch["Alice"].Group != "YouTube Live" || ch["Alice"].Number <= 5 {
		t.Fatalf("channels: %+v %+v", ch["Alice"], ch["Bob"])
	}
	now := time.Now().Unix()
	progs, _ := st.Programs([]string{ch["Alice"].EPGID, ch["Bob"].EPGID}, now, now+60)
	if len(progs) != 1 || progs[0].Title != "Speedrun" {
		t.Fatalf("guide: %+v", progs)
	}
	number := ch["Alice"].Number

	// Alice stops, Bob goes live; then Bob is taken off the list.
	a.Live, b.Live = false, true
	SyncPluginChannels(st, "youtube-live", "YouTube Live", []PluginChannel{a, b})
	if ch = byName(); ch["Alice"].Enabled || !ch["Bob"].Enabled || ch["Alice"].Number != number {
		t.Fatalf("after switching: %+v %+v", ch["Alice"], ch["Bob"])
	}
	SyncPluginChannels(st, "youtube-live", "YouTube Live", []PluginChannel{a})
	if ch = byName(); len(ch) != 2 || ch["Bob"] != nil || ch["News"] == nil {
		t.Fatalf("after removing Bob: %v", ch)
	}
	srcs, _ := st.LiveSources("m3u")
	if len(srcs) != 1 {
		t.Fatalf("sources: %+v", srcs)
	}
	if n, err := refresh(t.Context(), st, srcs[0]); err != nil || n != 1 {
		t.Fatalf("refreshing a plugin source fetches nothing: %d %v", n, err)
	}
}

func TestIsYouTube(t *testing.T) {
	for u, want := range map[string]bool{
		"https://www.youtube.com/channel/UCa/live": true, "https://youtu.be/abc": true,
		"http://example.com/youtube.com/live.m3u8": false, "https://notyoutube.com/x": false,
	} {
		if isYouTube(u) != want {
			t.Errorf("%s: %v", u, !want)
		}
	}
}
