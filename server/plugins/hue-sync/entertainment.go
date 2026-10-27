package main

// Entertainment streaming: the Hue Entertainment API the Hue Sync box and
// app use. Instead of one REST call per light change (about ten a second
// for the whole bridge), the plugin opens a DTLS stream to the bridge and
// sends every light of an entertainment area its colour in one small UDP
// packet, 25 times a second. Entertainment areas are made in the Hue app
// (Settings → Entertainment areas), where each light gets a place around
// the TV; that place picks the part of the picture the light follows.

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pion/dtls/v3"
)

// streamHz is how often the colours go out while streaming. The bridge
// passes on about 25 a second; sending more just gets dropped.
const streamHz = 25

// streamFPS is how many frames a second the sampler reads for a streaming
// session: more than REST's sampleFPS, since the lights can keep up.
const streamFPS = 12

// EntArea is an entertainment area (an "entertainment configuration" in the
// bridge's v2 API).
type EntArea struct {
	ID       string       // v2 id, a UUID
	Group    string       // the matching v1 group id, which devices pick
	Name     string       //
	Channels []EntChannel // at most 20
}

// EntChannel is one light (or one segment of a gradient light) in an area,
// placed around the TV: x left (-1) to right (1), y back to front, z floor
// to ceiling.
type EntChannel struct {
	ID      int
	X, Y, Z float64
}

// zone is the part of the picture a channel follows, from where it stands.
func (c EntChannel) zone() string {
	switch {
	case c.X < -0.33:
		return "left"
	case c.X > 0.33:
		return "right"
	case c.Z > 0.5:
		return "top"
	case c.Z < -0.5:
		return "bottom"
	}
	return "all"
}

// host is the bridge's address without a scheme or port.
func (b *Bridge) host() string {
	a := strings.TrimRight(b.Addr, "/")
	if u, err := url.Parse(a); err == nil && u.Host != "" {
		a = u.Host
	}
	if h, _, err := net.SplitHostPort(a); err == nil {
		return h
	}
	return a
}

// v2Client talks HTTPS to the bridge. Bridges carry a certificate signed by
// Philips' own authority for their bridge id, not a public one, so it isn't
// checked; the v1 API this plugin already uses is plain HTTP on the same LAN.
var v2Client = &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // see above
}}

// v2 calls the bridge's CLIP v2 API.
func (b *Bridge) v2(ctx context.Context, method, path string, body, out any) (http.Header, error) {
	var rd io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rd = bytes.NewReader(buf)
	}
	base := b.v2URL
	if base == "" {
		base = "https://" + b.host()
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("hue-application-key", b.Username)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := v2Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var res struct {
		Errors []struct {
			Description string `json:"description"`
		} `json:"errors"`
	}
	json.Unmarshal(raw, &res)
	if len(res.Errors) > 0 {
		return nil, errors.New("bridge: " + res.Errors[0].Description)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bridge: %s", resp.Status)
	}
	if out != nil {
		return resp.Header, json.Unmarshal(raw, out)
	}
	return resp.Header, nil
}

// EntertainmentAreas lists the bridge's entertainment areas.
func (b *Bridge) EntertainmentAreas(ctx context.Context) ([]EntArea, error) {
	var res struct {
		Data []struct {
			ID       string `json:"id"`
			IDv1     string `json:"id_v1"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Channels []struct {
				ChannelID int `json:"channel_id"`
				Position  struct {
					X float64 `json:"x"`
					Y float64 `json:"y"`
					Z float64 `json:"z"`
				} `json:"position"`
			} `json:"channels"`
		} `json:"data"`
	}
	if _, err := b.v2(ctx, http.MethodGet, "/clip/v2/resource/entertainment_configuration", nil, &res); err != nil {
		return nil, err
	}
	out := make([]EntArea, 0, len(res.Data))
	for _, d := range res.Data {
		a := EntArea{ID: d.ID, Group: strings.TrimPrefix(d.IDv1, "/groups/"), Name: d.Metadata.Name}
		for _, c := range d.Channels {
			a.Channels = append(a.Channels, EntChannel{c.ChannelID, c.Position.X, c.Position.Y, c.Position.Z})
		}
		out = append(out, a)
	}
	return out, nil
}

// entertainmentArea finds the area behind a v1 group id.
func (b *Bridge) entertainmentArea(ctx context.Context, group string) (*EntArea, error) {
	areas, err := b.EntertainmentAreas(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range areas {
		if a.Group == group {
			return &a, nil
		}
	}
	return nil, errors.New("that entertainment area is gone")
}

// setStreaming starts or stops an area's streaming mode on the bridge.
func (b *Bridge) setStreaming(ctx context.Context, areaID string, on bool) error {
	action := "stop"
	if on {
		action = "start"
	}
	_, err := b.v2(ctx, http.MethodPut, "/clip/v2/resource/entertainment_configuration/"+areaID, map[string]string{"action": action}, nil)
	return err
}

// appID is the application id the DTLS handshake names itself with. Older
// bridges don't send one; the username works there.
func (b *Bridge) appID(ctx context.Context) string {
	if h, err := b.v2(ctx, http.MethodGet, "/auth/v1", nil, nil); err == nil {
		if id := h.Get("hue-application-id"); id != "" {
			return id
		}
	}
	return b.Username
}

// Stream is an open entertainment stream to one area.
type Stream struct {
	b    *Bridge
	area EntArea
	conn net.Conn
	seq  byte
}

// openStream puts the area into streaming mode and opens the DTLS stream.
// clientKey is the hex key the bridge gave when pairing.
func openStream(ctx context.Context, b *Bridge, clientKey string, area EntArea) (*Stream, error) {
	psk, err := hex.DecodeString(clientKey)
	if err != nil || len(psk) == 0 {
		return nil, errors.New("pair the bridge again to use entertainment areas")
	}
	if err := b.setStreaming(ctx, area.ID, true); err != nil {
		return nil, err
	}
	at := b.streamAddr
	if at == "" {
		at = net.JoinHostPort(b.host(), "2100")
	}
	addr, err := net.ResolveUDPAddr("udp", at)
	if err != nil {
		b.setStreaming(ctx, area.ID, false)
		return nil, err
	}
	conn, err := dtls.Dial("udp", addr, &dtls.Config{
		PSK:             func([]byte) ([]byte, error) { return psk, nil },
		PSKIdentityHint: []byte(b.appID(ctx)),
		CipherSuites:    []dtls.CipherSuiteID{dtls.TLS_PSK_WITH_AES_128_GCM_SHA256},
	})
	if err == nil {
		hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = conn.HandshakeContext(hctx)
		cancel()
		if err != nil {
			conn.Close()
		}
	}
	if err != nil {
		b.setStreaming(context.WithoutCancel(ctx), area.ID, false)
		return nil, fmt.Errorf("couldn't open the stream: %w", err)
	}
	return &Stream{b: b, area: area, conn: conn}, nil
}

// Send gives each channel its colour (by channel, in area.Channels order),
// with brightness already scaled to 0–1.
func (s *Stream) Send(colours []Colour) error {
	_, err := s.conn.Write(streamMessage(s.area, s.seq, colours))
	s.seq++
	return err
}

// Close ends the stream and takes the area out of streaming mode, which
// gives the lights back to the bridge.
func (s *Stream) Close(ctx context.Context) {
	s.conn.Close()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	s.b.setStreaming(ctx, s.area.ID, false)
}

// streamMessage builds one HueStream v2 packet in the XY + brightness
// colour space.
func streamMessage(area EntArea, seq byte, colours []Colour) []byte {
	msg := make([]byte, 0, 52+7*len(area.Channels))
	msg = append(msg, "HueStream"...)
	msg = append(msg, 2, 0, seq, 0, 0, 1, 0) // version 2.0, sequence, reserved, XY colour space, reserved
	msg = append(msg, area.ID...)            // 36 characters
	u16 := func(f float64) []byte {
		return binary.BigEndian.AppendUint16(nil, uint16(math.Round(math.Max(0, math.Min(1, f))*0xffff)))
	}
	for i, ch := range area.Channels {
		if i >= len(colours) || i >= 20 {
			break
		}
		c := colours[i]
		msg = append(msg, byte(ch.ID))
		msg = append(msg, u16(c.X)...)
		msg = append(msg, u16(c.Y)...)
		msg = append(msg, u16(c.Bri)...)
	}
	return msg
}
