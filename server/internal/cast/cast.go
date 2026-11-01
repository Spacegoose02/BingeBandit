// Package cast sends video to TVs on the home network: Chromecasts and
// Google TVs (the Cast v2 protocol, playing in Google's Default Media
// Receiver) and DLNA / UPnP media renderers (most smart TVs, some
// soundbars and game consoles).
//
// The server does the casting, not the browser or phone: Chrome only offers
// its Cast button on HTTPS pages, and phones would need Google's Cast SDK,
// while the server is on the same network as the TV anyway. Apps list the
// devices the server finds and ask it to play there.
package cast

import (
	"context"
	"errors"
	"time"
)

// Kinds of device.
const (
	KindChromecast = "chromecast"
	KindDLNA       = "dlna"
)

// Device is a TV or speaker the server can play on.
type Device struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Model string `json:"model,omitempty"`
	// Host is the device's IP address, which the server's own address is
	// picked for (the interface on the way to it).
	Host string `json:"host"`
	// Manual marks a device an admin added by address.
	Manual bool `json:"manual,omitempty"`

	// Chromecast: where its Cast port is (host:8009).
	addr string
	// DLNA: the AVTransport service's control URL and type.
	controlURL  string
	serviceType string
}

// Media is what to play.
type Media struct {
	URL         string
	ContentType string // video/mp4, application/x-mpegURL, video/mpeg
	Title       string
	Subtitle    string
	Image       string  // absolute poster URL, optional
	Start       float64 // seconds into URL's media
	Duration    float64 // seconds, when known (a stream that grows doesn't say)
	Live        bool    // an HLS stream still being written (a conversion)
}

// Player states.
const (
	StateLoading = "loading"
	StatePlaying = "playing"
	StatePaused  = "paused"
	StateIdle    = "idle"     // nothing loaded, or the viewer stopped it on the TV
	StateEnded   = "finished" // played to the end
)

// Status is what the device says it's doing. Position is seconds into the
// loaded media.
type Status struct {
	State    string  `json:"state"`
	Position float64 `json:"position"`
	Volume   float64 `json:"volume"` // 0-1, -1 when unknown
}

// Player controls one device.
type Player interface {
	Load(ctx context.Context, m Media) error
	Play(ctx context.Context) error
	Pause(ctx context.Context) error
	Seek(ctx context.Context, pos float64) error
	SetVolume(ctx context.Context, level float64) error
	Status(ctx context.Context) (Status, error)
	// Stop ends playback on the device and frees the connection.
	Stop(ctx context.Context) error
	Close()
}

// ErrUnsupported is returned by a device that can't do what was asked.
var ErrUnsupported = errors.New("the TV doesn't support that")

// Open connects to d.
func Open(ctx context.Context, d Device) (Player, error) {
	switch d.Kind {
	case KindChromecast:
		return dialChromecast(ctx, d.addr)
	case KindDLNA:
		return &dlnaPlayer{control: d.controlURL, service: d.serviceType, client: httpClient}, nil
	}
	return nil, errors.New("unknown device kind")
}

const requestTimeout = 8 * time.Second
