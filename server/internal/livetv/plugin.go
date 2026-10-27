package livetv

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/store"
)

// PluginChannel is a channel a plugin offers, such as a YouTuber's live
// stream. The plugin lists all of its channels each time; one that isn't
// live is kept but hidden, so its number and favourites survive.
type PluginChannel struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Logo        string `json:"logo,omitempty"`
	URL         string `json:"url"`
	Live        bool   `json:"live"`
	Title       string `json:"title,omitempty"`       // what's on, for the guide
	Description string `json:"description,omitempty"` // for the guide
	StartedAt   int64  `json:"startedAt,omitempty"`   // unix seconds
}

var pluginSync sync.Mutex

// pluginGuideAhead is how far past now a live stream's guide entry runs; the
// plugin's next report pushes it on.
const pluginGuideAhead = 2 * time.Hour

// SyncPluginChannels makes Live TV match a plugin's channels: a playlist
// source at plugin:<id> holds their streams, each gets a channel in group, shown
// while it's live, and channels the plugin stopped listing are removed.
func SyncPluginChannels(st *store.Store, pluginID, group string, list []PluginChannel) error {
	pluginSync.Lock()
	defer pluginSync.Unlock()
	src, err := pluginSource(st, pluginID, group)
	if err != nil {
		return err
	}
	before, err := st.SourceChannels(src.ID)
	if err != nil {
		return err
	}
	im, err := st.BeginStreamImport(src.ID)
	if err != nil {
		return err
	}
	for _, c := range list {
		if err := im.Add(store.StreamEntry{Name: c.Name, URL: c.URL, Logo: c.Logo, Group: group, TvgID: pluginEPGID(pluginID, c.Key)}); err != nil {
			im.Abort()
			return err
		}
	}
	count, err := im.Finish()
	if err != nil {
		return err
	}
	st.SetSourceRefreshed(src.ID, count, nil)

	// Channels whose stream went away with the plugin's list go too.
	for _, c := range before {
		if n, err := st.ChannelStreamCount(c.ChannelID); err == nil && n == 0 {
			st.DeleteChannel(c.ChannelID)
		}
	}
	have, err := st.SourceChannels(src.ID)
	if err != nil {
		return err
	}
	byURL := map[string]store.SourceChannel{}
	for _, c := range have {
		byURL[c.URL] = c
	}
	streams, _, err := st.LiveStreams(store.StreamQuery{SourceID: src.ID, Limit: 10000})
	if err != nil {
		return err
	}
	streamOf := map[string]int64{}
	for _, s := range streams {
		streamOf[s.URL] = s.ID
	}
	for _, c := range list {
		if ch, ok := byURL[c.URL]; ok {
			if ch.Enabled != c.Live {
				if err := st.SetChannelEnabled(ch.ChannelID, c.Live); err != nil {
					return err
				}
			}
			continue
		}
		sid, ok := streamOf[c.URL]
		if !ok {
			continue
		}
		next, err := st.NextChannelNumber()
		if err != nil {
			return err
		}
		if _, err := st.CreateChannelsFromStreams([]int64{sid}, next); err != nil {
			return err
		}
		if !c.Live {
			if made, err := st.SourceChannels(src.ID); err == nil {
				for _, m := range made {
					if m.StreamID == sid {
						st.SetChannelEnabled(m.ChannelID, false)
					}
				}
			}
		}
	}

	// What's on: one guide entry per live stream, from when it started.
	gi, err := st.BeginGuideImport(src.ID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, c := range list {
		if !c.Live {
			continue
		}
		id := pluginEPGID(pluginID, c.Key)
		start := c.StartedAt
		if start <= 0 || start > now.Unix() {
			start = now.Add(-time.Minute).Unix()
		}
		title := c.Title
		if title == "" {
			title = c.Name + " is live"
		}
		if err := gi.Channel(id, c.Name, c.Logo); err != nil {
			gi.Abort()
			return err
		}
		if err := gi.Program(&store.GuideProgram{EPGID: id, Start: start, Stop: now.Add(pluginGuideAhead).Unix(),
			Title: title, Description: c.Description, Category: "Live"}); err != nil {
			gi.Abort()
			return err
		}
	}
	_, err = gi.Finish()
	return err
}

// IsPluginSource reports whether a source is a plugin's channels, which the
// plugin sends instead of being downloaded.
func IsPluginSource(src *store.LiveSource) bool { return strings.HasPrefix(src.URL, "plugin:") }

func pluginEPGID(pluginID, key string) string { return "plugin." + pluginID + "." + key }

func pluginSource(st *store.Store, pluginID, name string) (*store.LiveSource, error) {
	srcs, err := st.LiveSources("m3u")
	if err != nil {
		return nil, err
	}
	u := "plugin:" + pluginID
	for _, s := range srcs {
		if s.URL == u {
			return s, nil
		}
	}
	src := &store.LiveSource{Kind: "m3u", Name: name, URL: u, RefreshHours: 24, Enabled: true}
	if err := st.SaveLiveSource(src); err != nil {
		return nil, err
	}
	// Every group the plugin sends is imported.
	if err := st.SeedSourceGroups(src.ID, true, nil); err != nil {
		return nil, err
	}
	return src, nil
}

// ValidPluginChannels checks a plugin's list before it touches Live TV.
func ValidPluginChannels(list []PluginChannel) error {
	if len(list) > 500 {
		return fmt.Errorf("at most 500 channels")
	}
	seen := map[string]bool{}
	for _, c := range list {
		if c.Key == "" || c.Name == "" {
			return fmt.Errorf("each channel needs a key and a name")
		}
		if !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") {
			return fmt.Errorf("%s: the stream URL must be http(s)", c.Name)
		}
		if seen[c.URL] {
			return fmt.Errorf("%s: two channels share a stream URL", c.Name)
		}
		seen[c.URL] = true
	}
	return nil
}
