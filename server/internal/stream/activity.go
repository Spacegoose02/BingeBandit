package stream

import (
	"net"
	"sort"
	"sync"
	"time"
)

// NowPlaying is one active player, kept alive by its progress pings.
type NowPlaying struct {
	ID          string  `json:"id"`
	UserID      int64   `json:"userId"`
	Username    string  `json:"username"`
	ItemID      int64   `json:"itemId"`
	Title       string  `json:"title"`
	Subtitle    string  `json:"subtitle,omitempty"`
	Poster      string  `json:"poster,omitempty"`
	FileID      int64   `json:"fileId"`
	Mode        string  `json:"mode"` // direct | transcode
	Position    float64 `json:"position"`
	Duration    float64 `json:"duration"`
	Paused      bool    `json:"paused"`
	IP          string  `json:"ip"`
	Local       bool    `json:"local"`
	Client      string  `json:"client"`
	Source      string  `json:"source"` // e.g. "1080p HEVC · EAC3 5.1"
	Target      string  `json:"target"` // e.g. "720p H264 (nvenc)"
	StartedAt   int64   `json:"startedAt"`
	UpdatedAt   int64   `json:"updatedAt"`
	TranscodeID string  `json:"transcodeId,omitempty"`
}

const activityTTL = 45 // seconds without a ping before a player is dropped

type Activity struct {
	mu sync.Mutex
	m  map[string]*NowPlaying
}

func NewActivity() *Activity { return &Activity{m: map[string]*NowPlaying{}} }

func (a *Activity) Update(np NowPlaying) {
	a.mu.Lock()
	defer a.mu.Unlock()
	np.UpdatedAt = time.Now().Unix()
	if prev, ok := a.m[np.ID]; ok && prev.ItemID == np.ItemID {
		np.StartedAt = prev.StartedAt
	} else {
		np.StartedAt = np.UpdatedAt
	}
	a.m[np.ID] = &np
}

func (a *Activity) Stop(id string) {
	a.mu.Lock()
	delete(a.m, id)
	a.mu.Unlock()
}

func (a *Activity) List() []NowPlaying {
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := time.Now().Unix() - activityTTL
	out := []NowPlaying{}
	for id, np := range a.m {
		if np.UpdatedAt < cutoff {
			delete(a.m, id)
			continue
		}
		out = append(out, *np)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out
}

func IsLocalIP(ip string) bool {
	p := net.ParseIP(ip)
	return p != nil && (p.IsPrivate() || p.IsLoopback() || p.IsLinkLocalUnicast())
}
