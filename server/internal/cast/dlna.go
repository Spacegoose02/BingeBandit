package cast

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var httpClient = &http.Client{Timeout: requestTimeout}

// dlnaPlayer drives a UPnP AVTransport service with SOAP calls.
type dlnaPlayer struct {
	control string
	service string
	client  *http.Client

	mu      sync.Mutex
	loaded  bool
	playing bool // the last state seen was PLAYING (to tell "finished" from "never started")
}

func (d *dlnaPlayer) call(ctx context.Context, action string, args [][2]string) (map[string]string, error) {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&body, `<u:%s xmlns:u="%s">`, action, html.EscapeString(d.service))
	for _, a := range args {
		fmt.Fprintf(&body, "<%s>%s</%s>", a[0], html.EscapeString(a[1]), a[0])
	}
	fmt.Fprintf(&body, `</u:%s></s:Body></s:Envelope>`, action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.control, strings.NewReader(body.String()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", fmt.Sprintf(`"%s#%s"`, d.service, action))
	res, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the TV didn't answer: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	if res.StatusCode != http.StatusOK {
		if code := soapFault(b); code != "" {
			return nil, fmt.Errorf("the TV refused %s (%s)", action, code)
		}
		return nil, fmt.Errorf("the TV refused %s (HTTP %d)", action, res.StatusCode)
	}
	return soapValues(b), nil
}

// soapValues returns the leaf elements of a SOAP response by local name.
func soapValues(b []byte) map[string]string {
	out := map[string]string{}
	dec := xml.NewDecoder(bytes.NewReader(b))
	var name string
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name = t.Name.Local
			text.Reset()
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			if name == t.Name.Local {
				out[name] = strings.TrimSpace(text.String())
			}
			name = ""
		}
	}
}

func soapFault(b []byte) string {
	v := soapValues(b)
	if d := v["errorDescription"]; d != "" {
		return d
	}
	return v["errorCode"]
}

// hms formats seconds for Seek's REL_TIME: H:MM:SS.
func hms(sec float64) string {
	t := int(sec)
	if t < 0 {
		t = 0
	}
	return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
}

// parseHMS reads H:MM:SS(.fff); "NOT_IMPLEMENTED" and the like read as 0.
func parseHMS(s string) float64 {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0
	}
	var total float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0
		}
		total = total*60 + v
	}
	return total
}

// didl is the item description TVs show while playing (and that some
// refuse to play without). protocolInfo tells them the type up front.
func didl(m Media) string {
	info := "http-get:*:" + m.ContentType + ":*"
	if m.ContentType == "video/mpeg" {
		// A stream that's being converted: no byte seeking, sent as it comes.
		info = "http-get:*:video/mpeg:DLNA.ORG_OP=00;DLNA.ORG_CI=1;DLNA.ORG_FLAGS=01700000000000000000000000000000"
	}
	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">`)
	b.WriteString(`<item id="0" parentID="-1" restricted="1">`)
	title := m.Title
	if m.Subtitle != "" {
		title += " · " + m.Subtitle
	}
	fmt.Fprintf(&b, "<dc:title>%s</dc:title>", html.EscapeString(title))
	if m.Image != "" {
		fmt.Fprintf(&b, "<upnp:albumArtURI>%s</upnp:albumArtURI>", html.EscapeString(m.Image))
	}
	b.WriteString("<upnp:class>object.item.videoItem</upnp:class>")
	dur := ""
	if m.Duration > 0 {
		dur = fmt.Sprintf(` duration="%s.000"`, hms(m.Duration))
	}
	fmt.Fprintf(&b, `<res protocolInfo="%s"%s>%s</res>`, html.EscapeString(info), dur, html.EscapeString(m.URL))
	b.WriteString("</item></DIDL-Lite>")
	return b.String()
}

func (d *dlnaPlayer) Load(ctx context.Context, m Media) error {
	// Some TVs won't take a new URI while playing.
	d.call(ctx, "Stop", [][2]string{{"InstanceID", "0"}})
	if _, err := d.call(ctx, "SetAVTransportURI", [][2]string{{"InstanceID", "0"}, {"CurrentURI", m.URL}, {"CurrentURIMetaData", didl(m)}}); err != nil {
		return err
	}
	d.mu.Lock()
	d.loaded, d.playing = true, false
	d.mu.Unlock()
	if err := d.Play(ctx); err != nil {
		return err
	}
	if m.Start > 1 && m.ContentType != "video/mpeg" {
		// Wait for the TV to open the file before seeking in it.
		for i := 0; i < 20; i++ {
			if st, err := d.Status(ctx); err == nil && st.State == StatePlaying {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		if err := d.Seek(ctx, m.Start); err != nil {
			return nil // plays from the start rather than not at all
		}
	}
	return nil
}

func (d *dlnaPlayer) Play(ctx context.Context) error {
	_, err := d.call(ctx, "Play", [][2]string{{"InstanceID", "0"}, {"Speed", "1"}})
	return err
}

func (d *dlnaPlayer) Pause(ctx context.Context) error {
	_, err := d.call(ctx, "Pause", [][2]string{{"InstanceID", "0"}})
	return err
}

func (d *dlnaPlayer) Seek(ctx context.Context, pos float64) error {
	_, err := d.call(ctx, "Seek", [][2]string{{"InstanceID", "0"}, {"Unit", "REL_TIME"}, {"Target", hms(pos)}})
	return err
}

// SetVolume would need the RenderingControl service; the TV's own remote
// does it.
func (d *dlnaPlayer) SetVolume(ctx context.Context, level float64) error {
	return ErrUnsupported
}

func (d *dlnaPlayer) Status(ctx context.Context) (Status, error) {
	st := Status{State: StateIdle, Volume: -1}
	info, err := d.call(ctx, "GetTransportInfo", [][2]string{{"InstanceID", "0"}})
	if err != nil {
		return st, err
	}
	if pos, err := d.call(ctx, "GetPositionInfo", [][2]string{{"InstanceID", "0"}}); err == nil {
		st.Position = parseHMS(pos["RelTime"])
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	switch info["CurrentTransportState"] {
	case "PLAYING":
		st.State = StatePlaying
		d.playing = true
	case "PAUSED_PLAYBACK", "PAUSED_RECORDING":
		st.State = StatePaused
	case "TRANSITIONING":
		st.State = StateLoading
	default: // STOPPED, NO_MEDIA_PRESENT
		// TVs say STOPPED both at the end and when stopped with their
		// remote; the caller tells them apart by the last position. Before
		// it has played at all, it is still opening the stream.
		if d.loaded && !d.playing {
			st.State = StateLoading
		}
	}
	return st, nil
}

func (d *dlnaPlayer) Stop(ctx context.Context) error {
	d.mu.Lock()
	d.playing, d.loaded = false, false
	d.mu.Unlock()
	_, err := d.call(ctx, "Stop", [][2]string{{"InstanceID", "0"}})
	return err
}

func (d *dlnaPlayer) Close() {}

var errNoAVTransport = errors.New("not a media renderer")
