// Command youtube-live is the YouTube Live plugin: the YouTubers an admin
// lists become Live TV channels, shown in the guide while they're live and
// played through yt-dlp. It checks each one's /live page every few minutes.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/discover"
	"bingebandit/pkg/bbplugin"
)

var manifest = bbplugin.Manifest{
	ID:          "youtube-live",
	Name:        "YouTube Live",
	Version:     "1.1.0",
	Description: "Live TV channels for the YouTubers you pick, shown in the guide while they're live.",
	Author:      "BingeBandit",
	Settings: []bbplugin.Setting{
		{Key: "youtubers", Label: "YouTubers", Type: "text",
			Help: "Channel links or @handles, separated by commas, e.g. @NASA, https://www.youtube.com/@LofiGirl"},
		{Key: "group", Label: "Channel group", Type: "text", Default: "YouTube Live"},
		{Key: "check_minutes", Label: "Check who's live every (minutes)", Type: "number", Default: "3"},
		{Key: "update_ytdlp", Label: "Keep yt-dlp up to date (once a day)", Type: "bool", Default: "true",
			Help: "YouTube changes often; an old yt-dlp stops finding streams."},
	},
	Pages: []bbplugin.Page{{Title: "YouTube Live", Path: "/", Admin: true}},
}

// youtuber is one listed channel and what the last look found.
type youtuber struct {
	Input     string      `json:"input"`
	ID        string      `json:"id,omitempty"`
	Name      string      `json:"name,omitempty"`
	Avatar    string      `json:"avatar,omitempty"`
	profileAt time.Time   // when Name and Avatar were read
	Live      liveState   `json:"live"`
	More      []liveState `json:"more,omitempty"` // other streams on at the same time
	Error     string      `json:"error,omitempty"`
	Checked   time.Time   `json:"checked"`
}

// channel is what the plugin API's PUT /live/channels takes for each.
type channel struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Logo        string `json:"logo,omitempty"`
	URL         string `json:"url"`
	Live        bool   `json:"live"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	StartedAt   int64  `json:"startedAt,omitempty"`
}

type app struct {
	p *bbplugin.Plugin
	// send hands the channel list to BingeBandit (tests replace it).
	send func(ctx context.Context, group string, chans []channel) error
	// updateYTDLP runs yt-dlp's self-update (tests replace it).
	updateYTDLP func(ctx context.Context)

	mu       sync.Mutex
	settings map[string]string
	people   map[string]*youtuber // by Input
	lastSent time.Time
	sendErr  string
	wake     chan struct{}
}

func newApp() *app {
	a := &app{people: map[string]*youtuber{}, wake: make(chan struct{}, 1)}
	a.send = func(ctx context.Context, group string, chans []channel) error {
		return a.p.Call(ctx, "PUT", "/live/channels", map[string]any{"group": group, "channels": chans}, nil)
	}
	a.updateYTDLP = selfUpdate
	return a
}

func main() {
	a := newApp()
	a.p = &bbplugin.Plugin{Manifest: manifest, OnEvent: a.onEvent, Start: a.run}
	a.p.Handler = a.routes()
	log.SetFlags(0)
	if err := a.p.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *app) onEvent(_ context.Context, e bbplugin.Event) {
	if e.Event != "plugin.settings" {
		return
	}
	var s map[string]string
	if json.Unmarshal(e.Data, &s) == nil {
		a.mu.Lock()
		a.settings = s
		a.mu.Unlock()
		a.kick()
	}
}

func (a *app) kick() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *app) conf(ctx context.Context) map[string]string {
	a.mu.Lock()
	s := a.settings
	a.mu.Unlock()
	if s != nil {
		return s
	}
	s, err := a.p.Settings(ctx)
	if err != nil {
		s = map[string]string{}
		for _, st := range manifest.Settings {
			s[st.Key] = st.Default
		}
		return s
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	return s
}

// run looks every few minutes, or at once after the settings change.
func (a *app) run(ctx context.Context) {
	var updated time.Time
	for {
		s := a.conf(ctx)
		if s["update_ytdlp"] != "false" && time.Since(updated) > 24*time.Hour {
			a.updateYTDLP(ctx)
			updated = time.Now()
		}
		a.look(ctx)
		mins, _ := strconv.Atoi(s["check_minutes"])
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		case <-time.After(time.Duration(max(mins, 1)) * time.Minute):
		}
	}
}

// look checks every listed YouTuber and sends the channels.
func (a *app) look(ctx context.Context) {
	s := a.conf(ctx)
	inputs := splitList(s["youtubers"])
	group := strings.TrimSpace(s["group"])
	if group == "" {
		group = "YouTube Live"
	}
	a.mu.Lock()
	keep := map[string]*youtuber{}
	for _, in := range inputs {
		y := a.people[in]
		if y == nil {
			y = &youtuber{Input: in}
		}
		keep[in] = y
	}
	a.people = keep
	a.mu.Unlock()

	var chans []channel
	seen := map[string]bool{}
	for _, in := range inputs {
		if ctx.Err() != nil {
			return
		}
		a.mu.Lock()
		y := *a.people[in]
		a.mu.Unlock()
		y.Error = ""
		if y.ID == "" {
			src, err := discover.Parse(ctx, in)
			if err == nil && src.Kind != "channel" {
				err = discover.ErrUnknownSource
			}
			if err != nil {
				y.Error = err.Error()
			} else {
				y.ID = src.ID
			}
		}
		if y.ID != "" && (y.Name == "" || time.Since(y.profileAt) > 24*time.Hour) {
			if p, err := channelProfile(ctx, y.ID); err != nil {
				y.Error = err.Error()
			} else {
				y.Name, y.Avatar, y.profileAt = p.Name, p.Avatar, time.Now()
			}
		}
		if y.ID != "" && y.Error == "" {
			if st, err := checkLive(ctx, y.ID); err != nil {
				y.Error = err.Error() // keep the last state for one bad look
			} else {
				y.Live = st
			}
		}
		if !y.Live.Live {
			y.More = nil
		} else if y.Error == "" {
			if more, err := moreLive(ctx, y.ID, y.Live.VideoID); err == nil {
				y.More = more
			} // else keep the last list for one bad look
		}
		y.Checked = time.Now()
		a.mu.Lock()
		if cur := a.people[in]; cur != nil {
			*cur = y
		}
		a.mu.Unlock()
		if y.ID == "" || y.Name == "" || seen[y.ID] {
			continue
		}
		seen[y.ID] = true
		chans = append(chans, channel{Key: y.ID, Name: y.Name, Logo: y.Avatar, URL: "https://www.youtube.com/channel/" + y.ID + "/live",
			Live: y.Live.Live, Title: y.Live.Title, Description: y.Live.Description, StartedAt: y.Live.StartedAt})
		// Each other stream on at once gets a channel of its own while it lasts.
		for i, m := range y.More {
			chans = append(chans, channel{Key: y.ID + "." + m.VideoID, Name: fmt.Sprintf("%s (%d)", y.Name, i+2), Logo: y.Avatar,
				URL: "https://www.youtube.com/watch?v=" + m.VideoID, Live: true, Title: m.Title})
		}
	}
	if chans == nil {
		chans = []channel{}
	}
	err := a.send(ctx, group, chans)
	a.mu.Lock()
	a.lastSent, a.sendErr = time.Now(), ""
	if err != nil {
		a.sendErr = err.Error()
	}
	a.mu.Unlock()
	if err != nil && err != bbplugin.ErrNotPaired {
		log.Printf("sending channels: %v", err)
	}
}

// selfUpdate runs "yt-dlp -U", which replaces its own file when it can.
func selfUpdate(ctx context.Context) {
	path, err := exec.LookPath("yt-dlp")
	if err != nil {
		log.Printf("yt-dlp isn't installed, so YouTube streams won't play (the Docker image has it)")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-U").CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if i := strings.LastIndex(msg, "\n"); i >= 0 {
		msg = msg[i+1:]
	}
	if err != nil {
		log.Printf("updating yt-dlp: %v %s", err, msg)
	} else if msg != "" {
		log.Printf("yt-dlp: %s", msg)
	}
}
