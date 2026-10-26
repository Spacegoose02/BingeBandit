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
	// Watched is how many seconds have actually played in this session.
	Watched float64 `json:"watched"`
	// Lights says whether the device lets plugins like Hue Sync follow it.
	Lights bool `json:"-"`
	// LightsArea is the Hue room or zone the device picked for those lights.
	LightsArea string `json:"-"`
	// PlayID is the play-history row, once the session is long enough to record.
	PlayID int64 `json:"-"`
}

// maxPingGap caps how much play time one ping can add, so a laptop that
// slept with the player open doesn't log hours of viewing.
const maxPingGap = 20

const activityTTL = 45 // seconds without a ping before a player is dropped

type Activity struct {
	mu sync.Mutex
	m  map[string]*NowPlaying
	// OnExpire is called for each player dropped for not pinging.
	OnExpire func(NowPlaying)
}

func NewActivity() *Activity { return &Activity{m: map[string]*NowPlaying{}} }

// Update records a ping and returns the session with its accumulated play time.
func (a *Activity) Update(np NowPlaying) NowPlaying {
	a.mu.Lock()
	defer a.mu.Unlock()
	np.UpdatedAt = time.Now().Unix()
	played := np.Watched // what the client says it has played this session
	if prev, ok := a.m[np.ID]; ok && prev.ItemID == np.ItemID {
		np.StartedAt, np.PlayID, np.Watched = prev.StartedAt, prev.PlayID, prev.Watched
		// Count the time since the last ping only if the player was playing.
		if !prev.Paused {
			np.Watched += float64(min(np.UpdatedAt-prev.UpdatedAt, maxPingGap))
		}
	} else {
		np.StartedAt = np.UpdatedAt
	}
	// Prefer the client's own count (it includes the time before the first
	// ping), but never more than the wall-clock time the session has existed.
	if played > 0 {
		np.Watched = min(played, float64(np.UpdatedAt-np.StartedAt+maxPingGap))
	}
	a.m[np.ID] = &np
	return np
}

// Get returns a session as of its last ping.
func (a *Activity) Get(id string) (NowPlaying, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	np, ok := a.m[id]
	if !ok {
		return NowPlaying{}, false
	}
	return *np, true
}

func (a *Activity) SetPlayID(id string, playID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if np, ok := a.m[id]; ok {
		np.PlayID = playID
	}
}

func (a *Activity) Stop(id string) {
	a.mu.Lock()
	delete(a.m, id)
	a.mu.Unlock()
}

// List returns the current players, dropping ones that stopped pinging.
func (a *Activity) List() []NowPlaying {
	a.mu.Lock()
	cutoff := time.Now().Unix() - activityTTL
	out, expired := []NowPlaying{}, []NowPlaying{}
	for id, np := range a.m {
		if np.UpdatedAt < cutoff {
			delete(a.m, id)
			expired = append(expired, *np)
			continue
		}
		out = append(out, *np)
	}
	onExpire := a.OnExpire
	a.mu.Unlock()
	if onExpire != nil {
		for _, np := range expired {
			onExpire(np)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out
}

func IsLocalIP(ip string) bool {
	p := net.ParseIP(ip)
	return p != nil && (p.IsPrivate() || p.IsLoopback() || p.IsLinkLocalUnicast())
}
