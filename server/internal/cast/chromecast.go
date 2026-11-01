package cast

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Cast v2: TLS to port 8009, then length-prefixed protobuf CastMessages
// whose payloads are JSON. The protobuf is small enough to write by hand.

const (
	nsConnection = "urn:x-cast:com.google.cast.tp.connection"
	nsHeartbeat  = "urn:x-cast:com.google.cast.tp.heartbeat"
	nsReceiver   = "urn:x-cast:com.google.cast.receiver"
	nsMedia      = "urn:x-cast:com.google.cast.media"

	// defaultMediaReceiver is Google's own player app on every Cast device.
	defaultMediaReceiver = "CC1AD845"
	senderID             = "sender-bingebandit"
	maxCastMessage       = 64 << 10
)

type castMessage struct {
	Source, Destination, Namespace, Payload string
}

func appendVarint(b []byte, v uint64) []byte {
	return binary.AppendUvarint(b, v)
}

func appendString(b []byte, field int, s string) []byte {
	b = appendVarint(b, uint64(field<<3|2))
	b = appendVarint(b, uint64(len(s)))
	return append(b, s...)
}

// marshal encodes a CastMessage: protocol_version (1) CASTV2_1_0, source_id
// (2), destination_id (3), namespace (4), payload_type (5) STRING,
// payload_utf8 (6).
func (m castMessage) marshal() []byte {
	b := []byte{1 << 3, 0}
	b = appendString(b, 2, m.Source)
	b = appendString(b, 3, m.Destination)
	b = appendString(b, 4, m.Namespace)
	b = append(b, 5<<3, 0)
	return appendString(b, 6, m.Payload)
}

func unmarshalCast(b []byte) (castMessage, error) {
	var m castMessage
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return m, errors.New("cast: bad field key")
		}
		b = b[n:]
		field, wire := key>>3, key&7
		switch wire {
		case 0:
			_, n = binary.Uvarint(b)
			if n <= 0 {
				return m, errors.New("cast: bad varint")
			}
			b = b[n:]
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return m, errors.New("cast: bad length")
			}
			v := string(b[n : n+int(l)])
			b = b[n+int(l):]
			switch field {
			case 2:
				m.Source = v
			case 3:
				m.Destination = v
			case 4:
				m.Namespace = v
			case 6:
				m.Payload = v
			}
		default:
			return m, fmt.Errorf("cast: unexpected wire type %d", wire)
		}
	}
	return m, nil
}

type chromecast struct {
	conn net.Conn
	wmu  sync.Mutex

	nextID  atomic.Int64
	mu      sync.Mutex
	waiting map[int64]chan map[string]any
	closed  chan struct{}
	err     error

	// The Default Media Receiver once launched, and what it's playing.
	transportID    string
	appSessionID   string
	mediaSessionID int64
	last           Status
	ended          bool
}

// castDialer is replaced in tests.
var castDialer = func(ctx context.Context, addr string) (net.Conn, error) {
	d := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		// Cast devices present certificates signed by Google's device CA
		// for their own serial number, not for an address, so ordinary
		// verification can't apply. Everything sent is on the home network.
		Config: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
	return d.DialContext(ctx, "tcp", addr)
}

func dialChromecast(ctx context.Context, addr string) (*chromecast, error) {
	conn, err := castDialer(ctx, addr)
	if err != nil {
		return nil, err
	}
	c := &chromecast{conn: conn, waiting: map[int64]chan map[string]any{}, closed: make(chan struct{}), last: Status{State: StateIdle, Volume: -1}}
	if err := c.send(nsConnection, "receiver-0", map[string]any{"type": "CONNECT"}); err != nil {
		conn.Close()
		return nil, err
	}
	go c.readLoop()
	go c.heartbeat()
	return c, nil
}

func (c *chromecast) send(ns, dest string, payload map[string]any) error {
	p, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	body := castMessage{Source: senderID, Destination: dest, Namespace: ns, Payload: string(p)}.marshal()
	frame := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(requestTimeout))
	_, err = c.conn.Write(append(frame, body...))
	return err
}

// request sends payload with a fresh requestId and waits for the answer.
func (c *chromecast) request(ctx context.Context, ns, dest string, payload map[string]any) (map[string]any, error) {
	id := c.nextID.Add(1)
	payload["requestId"] = id
	ch := make(chan map[string]any, 1)
	c.mu.Lock()
	c.waiting[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
	}()
	if err := c.send(ns, dest, payload); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	select {
	case m := <-ch:
		switch m["type"] {
		case "LOAD_FAILED", "LOAD_CANCELLED", "INVALID_REQUEST", "LAUNCH_ERROR", "INVALID_PLAYER_STATE":
			reason, _ := m["reason"].(string)
			if reason == "" {
				reason, _ = m["type"].(string)
			}
			return m, fmt.Errorf("the Chromecast said %s", reason)
		}
		return m, nil
	case <-c.closed:
		return nil, c.closeErr()
	case <-ctx.Done():
		return nil, errors.New("the Chromecast didn't answer")
	}
}

func (c *chromecast) closeErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return errors.New("the connection to the Chromecast closed")
}

func (c *chromecast) readLoop() {
	r := bufio.NewReader(c.conn)
	var err error
	for {
		var size uint32
		c.conn.SetReadDeadline(time.Now().Add(30 * time.Second)) // heartbeats arrive every 5 s
		if err = binary.Read(r, binary.BigEndian, &size); err != nil {
			break
		}
		if size > maxCastMessage {
			err = errors.New("cast message too large")
			break
		}
		buf := make([]byte, size)
		if _, err = io.ReadFull(r, buf); err != nil {
			break
		}
		m, perr := unmarshalCast(buf)
		if perr != nil {
			continue
		}
		c.handle(m)
	}
	c.mu.Lock()
	if c.err == nil {
		c.err = fmt.Errorf("lost the connection to the Chromecast: %w", err)
	}
	c.mu.Unlock()
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	c.conn.Close()
}

func (c *chromecast) handle(m castMessage) {
	var p map[string]any
	if json.Unmarshal([]byte(m.Payload), &p) != nil {
		return
	}
	typ, _ := p["type"].(string)
	switch {
	case m.Namespace == nsHeartbeat && typ == "PING":
		c.send(nsHeartbeat, m.Source, map[string]any{"type": "PONG"})
		return
	case m.Namespace == nsConnection && typ == "CLOSE" && m.Source != "receiver-0":
		// The app on the TV went away (another phone took over the TV).
		c.mu.Lock()
		c.transportID, c.mediaSessionID = "", 0
		c.last.State = StateIdle
		c.mu.Unlock()
	case m.Namespace == nsMedia && typ == "MEDIA_STATUS":
		c.mediaStatus(p)
	case m.Namespace == nsReceiver && typ == "RECEIVER_STATUS":
		c.receiverStatus(p)
	}
	if id, ok := p["requestId"].(float64); ok && id > 0 {
		c.mu.Lock()
		ch := c.waiting[int64(id)]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- p:
			default:
			}
		}
	}
}

func (c *chromecast) receiverStatus(p map[string]any) {
	st, _ := p["status"].(map[string]any)
	c.mu.Lock()
	defer c.mu.Unlock()
	if vol, ok := st["volume"].(map[string]any); ok {
		if l, ok := vol["level"].(float64); ok {
			c.last.Volume = l
		}
	}
	apps, _ := st["applications"].([]any)
	found := false
	for _, a := range apps {
		app, _ := a.(map[string]any)
		if app["appId"] == defaultMediaReceiver {
			found = true
			c.transportID, _ = app["transportId"].(string)
			c.appSessionID, _ = app["sessionId"].(string)
		}
	}
	if !found && c.transportID != "" {
		// Someone opened another app on the TV.
		c.transportID, c.mediaSessionID = "", 0
		c.last.State = StateIdle
	}
}

func (c *chromecast) mediaStatus(p map[string]any) {
	list, _ := p["status"].([]any)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(list) == 0 {
		return
	}
	st, _ := list[0].(map[string]any)
	if id, ok := st["mediaSessionId"].(float64); ok {
		c.mediaSessionID = int64(id)
	}
	if t, ok := st["currentTime"].(float64); ok {
		c.last.Position = t
	}
	switch st["playerState"] {
	case "PLAYING":
		c.last.State = StatePlaying
	case "PAUSED":
		c.last.State = StatePaused
	case "BUFFERING", "LOADING":
		c.last.State = StateLoading
	case "IDLE":
		c.last.State = StateIdle
		if st["idleReason"] == "FINISHED" {
			c.last.State = StateEnded
			c.ended = true
		}
	}
}

func (c *chromecast) heartbeat() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-t.C:
			if c.send(nsHeartbeat, "receiver-0", map[string]any{"type": "PING"}) != nil {
				c.conn.Close()
				return
			}
		}
	}
}

// launch starts the Default Media Receiver (or finds it running) and
// connects to it.
func (c *chromecast) launch(ctx context.Context) (string, error) {
	c.mu.Lock()
	tid := c.transportID
	c.mu.Unlock()
	if tid != "" {
		return tid, nil
	}
	if _, err := c.request(ctx, nsReceiver, "receiver-0", map[string]any{"type": "LAUNCH", "appId": defaultMediaReceiver}); err != nil {
		return "", err
	}
	// The answer is a RECEIVER_STATUS; the app may still be starting.
	deadline := time.Now().Add(15 * time.Second)
	for {
		c.mu.Lock()
		tid = c.transportID
		c.mu.Unlock()
		if tid != "" {
			break
		}
		if time.Now().After(deadline) {
			return "", errors.New("the Chromecast didn't start its player")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		c.request(ctx, nsReceiver, "receiver-0", map[string]any{"type": "GET_STATUS"})
	}
	if err := c.send(nsConnection, tid, map[string]any{"type": "CONNECT"}); err != nil {
		return "", err
	}
	return tid, nil
}

func (c *chromecast) Load(ctx context.Context, m Media) error {
	tid, err := c.launch(ctx)
	if err != nil {
		return err
	}
	meta := map[string]any{"metadataType": 0, "title": m.Title}
	if m.Subtitle != "" {
		meta["subtitle"] = m.Subtitle
	}
	if m.Image != "" {
		meta["images"] = []any{map[string]any{"url": m.Image}}
	}
	media := map[string]any{
		"contentId":   m.URL,
		"contentUrl":  m.URL,
		"streamType":  "BUFFERED",
		"contentType": m.ContentType,
		"metadata":    meta,
	}
	if m.Duration > 0 {
		media["duration"] = m.Duration
	}
	if m.ContentType == "application/x-mpegURL" {
		media["hlsSegmentFormat"] = "ts"
		media["hlsVideoSegmentFormat"] = "mpeg2_ts"
	}
	c.mu.Lock()
	c.ended = false
	c.last.State, c.last.Position = StateLoading, m.Start
	c.mu.Unlock()
	_, err = c.request(ctx, nsMedia, tid, map[string]any{"type": "LOAD", "media": media, "autoplay": true, "currentTime": m.Start})
	return err
}

func (c *chromecast) mediaCommand(ctx context.Context, typ string, extra map[string]any) error {
	c.mu.Lock()
	tid, msid := c.transportID, c.mediaSessionID
	c.mu.Unlock()
	if tid == "" || msid == 0 {
		return errors.New("nothing is playing on the Chromecast")
	}
	p := map[string]any{"type": typ, "mediaSessionId": msid}
	for k, v := range extra {
		p[k] = v
	}
	_, err := c.request(ctx, nsMedia, tid, p)
	return err
}

func (c *chromecast) Play(ctx context.Context) error  { return c.mediaCommand(ctx, "PLAY", nil) }
func (c *chromecast) Pause(ctx context.Context) error { return c.mediaCommand(ctx, "PAUSE", nil) }
func (c *chromecast) Seek(ctx context.Context, pos float64) error {
	return c.mediaCommand(ctx, "SEEK", map[string]any{"currentTime": pos})
}

func (c *chromecast) SetVolume(ctx context.Context, level float64) error {
	_, err := c.request(ctx, nsReceiver, "receiver-0", map[string]any{"type": "SET_VOLUME", "volume": map[string]any{"level": level}})
	return err
}

func (c *chromecast) Status(ctx context.Context) (Status, error) {
	select {
	case <-c.closed:
		return Status{}, c.closeErr()
	default:
	}
	c.mu.Lock()
	tid, ended := c.transportID, c.ended
	c.mu.Unlock()
	if tid != "" && !ended {
		if _, err := c.request(ctx, nsMedia, tid, map[string]any{"type": "GET_STATUS"}); err != nil {
			return Status{}, err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last, nil
}

func (c *chromecast) Stop(ctx context.Context) error {
	c.mu.Lock()
	sid := c.appSessionID
	c.mu.Unlock()
	var err error
	if sid != "" {
		_, err = c.request(ctx, nsReceiver, "receiver-0", map[string]any{"type": "STOP", "sessionId": sid})
	}
	c.Close()
	return err
}

func (c *chromecast) Close() {
	c.conn.Close()
}
