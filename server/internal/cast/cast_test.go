package cast

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCastMessageRoundTrip(t *testing.T) {
	in := castMessage{Source: "sender-0", Destination: "receiver-0", Namespace: nsReceiver, Payload: `{"type":"GET_STATUS","requestId":1}`}
	out, err := unmarshalCast(in.marshal())
	if err != nil || out != in {
		t.Fatalf("got %+v, %v", out, err)
	}
	if _, err := unmarshalCast([]byte{0x12, 0x50, 'x'}); err == nil {
		t.Fatal("a truncated message parsed")
	}
}

// fakeCast plays the Chromecast's side of the protocol over a pipe.
type fakeCast struct {
	t       *testing.T
	conn    net.Conn
	mu      sync.Mutex
	got     []map[string]any
	state   string
	time    float64
	running bool
	pong    bool
}

func (f *fakeCast) send(ns, src, dest string, p map[string]any) {
	b, _ := json.Marshal(p)
	body := castMessage{Source: src, Destination: dest, Namespace: ns, Payload: string(b)}.marshal()
	f.conn.Write(append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), body...))
}

func (f *fakeCast) serve() {
	r := bufio.NewReader(f.conn)
	f.send(nsHeartbeat, "receiver-0", senderID, map[string]any{"type": "PING"})
	for {
		var n uint32
		if binary.Read(r, binary.BigEndian, &n) != nil {
			return
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return
		}
		m, err := unmarshalCast(buf)
		if err != nil {
			f.t.Errorf("bad message from the server: %v", err)
			return
		}
		var p map[string]any
		json.Unmarshal([]byte(m.Payload), &p)
		f.mu.Lock()
		f.got = append(f.got, p)
		id := p["requestId"]
		switch p["type"] {
		case "PONG":
			f.pong = true
		case "LAUNCH":
			f.running = true
		case "STOP":
			f.running = false
		case "LOAD":
			f.state = "PLAYING"
			f.time, _ = p["currentTime"].(float64)
		case "PAUSE":
			f.state = "PAUSED"
		case "PLAY":
			f.state = "PLAYING"
		case "SEEK":
			f.time, _ = p["currentTime"].(float64)
		}
		running, state, tm := f.running, f.state, f.time
		f.mu.Unlock()
		switch m.Namespace {
		case nsReceiver:
			apps := []any{}
			if running {
				apps = append(apps, map[string]any{"appId": defaultMediaReceiver, "transportId": "web-7", "sessionId": "app-1"})
			}
			f.send(nsReceiver, "receiver-0", m.Source, map[string]any{"type": "RECEIVER_STATUS", "requestId": id,
				"status": map[string]any{"applications": apps, "volume": map[string]any{"level": 0.3}}})
		case nsMedia:
			if m.Destination != "web-7" {
				f.t.Errorf("media message to %q, not the player", m.Destination)
			}
			f.send(nsMedia, "web-7", m.Source, map[string]any{"type": "MEDIA_STATUS", "requestId": id,
				"status": []any{map[string]any{"mediaSessionId": 1, "playerState": state, "currentTime": tm}}})
		}
	}
}

func (f *fakeCast) sent(typ string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.got {
		if p["type"] == typ {
			return p
		}
	}
	return nil
}

func TestChromecast(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	fake := &fakeCast{t: t}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		fake.conn = conn
		fake.serve()
	}()
	old := castDialer
	castDialer = func(ctx context.Context, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", ln.Addr().String())
	}
	defer func() { castDialer = old }()

	ctx := context.Background()
	p, err := Open(ctx, Device{Kind: KindChromecast, addr: "tv:8009"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Load(ctx, Media{URL: "http://srv/a.m3u8?token=x", ContentType: "application/x-mpegURL", Title: "Clip", Image: "http://srv/p.jpg", Start: 0}); err != nil {
		t.Fatal(err)
	}
	load := fake.sent("LOAD")
	media, _ := load["media"].(map[string]any)
	if media["contentId"] != "http://srv/a.m3u8?token=x" || media["hlsSegmentFormat"] != "ts" || load["autoplay"] != true {
		t.Fatalf("LOAD: %v", load)
	}
	st, err := p.Status(ctx)
	if err != nil || st.State != StatePlaying || st.Volume != 0.3 {
		t.Fatalf("status %+v %v", st, err)
	}
	if err := p.Seek(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if err := p.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ = p.Status(ctx)
	if st.State != StatePaused || st.Position != 42 {
		t.Fatalf("after seek and pause: %+v", st)
	}
	if seek := fake.sent("SEEK"); seek["mediaSessionId"] != float64(1) {
		t.Fatalf("SEEK: %v", seek)
	}
	if err := p.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.running || !fake.pong {
		t.Fatalf("running after stop: %v, answered ping: %v", fake.running, fake.pong)
	}
}

// buildMDNS writes a Chromecast's mDNS answer: PTR, then SRV, TXT and A
// records in the additional section, using name compression.
func buildMDNS() []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[2:], 0x8400)
	binary.BigEndian.PutUint16(b[6:], 1)  // answers
	binary.BigEndian.PutUint16(b[10:], 3) // additional
	name := func(s string) []byte {
		var out []byte
		for _, l := range strings.Split(s, ".") {
			out = append(out, byte(len(l)))
			out = append(out, l...)
		}
		return append(out, 0)
	}
	rec := func(owner []byte, typ uint16, data []byte) {
		b = append(b, owner...)
		b = binary.BigEndian.AppendUint16(b, typ)
		b = binary.BigEndian.AppendUint16(b, 1)
		b = binary.BigEndian.AppendUint32(b, 120)
		b = binary.BigEndian.AppendUint16(b, uint16(len(data)))
		b = append(b, data...)
	}
	serviceAt := len(b)
	svc := name("_googlecast._tcp.local")
	// PTR: the instance name, its last part pointing back at the service.
	instLabel := "Chromecast-abc123"
	ptr := append([]byte{byte(len(instLabel))}, instLabel...)
	ptr = append(ptr, 0xC0|byte(serviceAt>>8), byte(serviceAt))
	rec(svc, 12, ptr)
	instAt := len(b) - len(ptr)
	inst := []byte{0xC0 | byte(instAt>>8), byte(instAt)}
	srv := []byte{0, 0, 0, 0, 0x1F, 0x49} // port 8009
	srv = append(srv, name("abc123.local")...)
	rec(inst, 33, srv)
	var txt []byte
	for _, kv := range []string{"id=ABC123", "md=Chromecast with Google TV", "fn=Living Room"} {
		txt = append(txt, byte(len(kv)))
		txt = append(txt, kv...)
	}
	rec(inst, 16, txt)
	rec(name("abc123.local"), 1, []byte{192, 168, 1, 50})
	return b
}

func TestMDNSAnswer(t *testing.T) {
	got := castFromMDNS(buildMDNS(), net.IPv4(10, 0, 0, 9))
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	d := got[0]
	if d.ID != "cc-abc123" || d.Name != "Living Room" || d.Model != "Chromecast with Google TV" || d.Host != "192.168.1.50" || d.addr != "192.168.1.50:8009" {
		t.Fatalf("device %+v", d)
	}
	q := dnsQuestion(castService)
	if name, _, err := dnsName(q, 12); err != nil || name != castService {
		t.Fatalf("question name %q %v", name, err)
	}
}

const description = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <device>
    <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
    <friendlyName>[TV] Bedroom</friendlyName>
    <manufacturer>Samsung</manufacturer>
    <modelName>UE55</modelName>
    <UDN>uuid:1234-ABCD</UDN>
    <serviceList>
      <service><serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType><controlURL>/rc</controlURL></service>
      <service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/upnp/control/AVTransport1</controlURL></service>
    </serviceList>
  </device>
</root>`

// fakeRenderer is a DLNA TV: an SSDP responder and its HTTP services.
type fakeRenderer struct {
	mu      sync.Mutex
	state   string
	uri     string
	meta    string
	target  string
	actions []string
}

func (f *fakeRenderer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/desc.xml" {
		io.WriteString(w, description)
		return
	}
	body, _ := io.ReadAll(r.Body)
	action := r.Header.Get("SOAPACTION")
	action = strings.Trim(action[strings.Index(action, "#")+1:], `"`)
	v := soapValues(body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	reply := ""
	switch action {
	case "SetAVTransportURI":
		f.uri, f.meta, f.state = v["CurrentURI"], v["CurrentURIMetaData"], "STOPPED"
	case "Play":
		f.state = "PLAYING"
	case "Pause":
		f.state = "PAUSED_PLAYBACK"
	case "Stop":
		f.state = "STOPPED"
	case "Seek":
		f.target = v["Target"]
	case "GetTransportInfo":
		reply = "<CurrentTransportState>" + f.state + "</CurrentTransportState>"
	case "GetPositionInfo":
		reply = "<RelTime>0:01:05</RelTime>"
	default:
		w.WriteHeader(500)
		io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>401</errorCode><errorDescription>Invalid Action</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
		return
	}
	fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">%s</u:%sResponse></s:Body></s:Envelope>`, action, reply, action)
}

func TestDLNA(t *testing.T) {
	tv := &fakeRenderer{state: "NO_MEDIA_PRESENT"}
	srv := httptest.NewServer(tv)
	defer srv.Close()

	// An SSDP responder on loopback, answering a unicast search the way a
	// TV does.
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := udp.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if !strings.Contains(string(buf[:n]), "ST: "+rendererType) {
				continue
			}
			udp.WriteToUDP([]byte("HTTP/1.1 200 OK\r\nST: "+rendererType+"\r\nLOCATION: "+srv.URL+"/desc.xml\r\nUSN: uuid:1234-ABCD\r\n\r\n"), from)
			// Twice, as TVs do; it's only described once.
			udp.WriteToUDP([]byte("HTTP/1.1 200 OK\r\nLOCATION: "+srv.URL+"/desc.xml\r\n\r\n"), from)
			// A stray answer pointing somewhere else is ignored.
			udp.WriteToUDP([]byte("HTTP/1.1 200 OK\r\nLOCATION: http://192.0.2.1/desc.xml\r\n\r\n"), from)
		}
	}()
	var found []Device
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := discoverDLNA(ctx, udp.LocalAddr().String(), func(d Device) { found = append(found, d) }); err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("found %+v", found)
	}
	d := found[0]
	if d.ID != "dlna-1234-abcd" || d.Name != "[TV] Bedroom" || d.Model != "Samsung UE55" || d.controlURL != srv.URL+"/upnp/control/AVTransport1" || d.serviceType != "urn:schemas-upnp-org:service:AVTransport:1" {
		t.Fatalf("device %+v", d)
	}

	p, err := Open(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Load(context.Background(), Media{URL: "http://srv/stream.ts?token=a&b", ContentType: "video/mpeg", Title: "Show", Subtitle: "S1 E2 · Pilot <1>"}); err != nil {
		t.Fatal(err)
	}
	tv.mu.Lock()
	if tv.uri != "http://srv/stream.ts?token=a&b" || !strings.Contains(tv.meta, "<dc:title>Show · S1 E2 · Pilot &lt;1&gt;</dc:title>") || !strings.Contains(tv.meta, "DLNA.ORG_OP=00") {
		t.Fatalf("SetAVTransportURI: %q %q", tv.uri, tv.meta)
	}
	tv.mu.Unlock()
	st, err := p.Status(context.Background())
	if err != nil || st.State != StatePlaying || st.Position != 65 {
		t.Fatalf("status %+v %v", st, err)
	}
	if err := p.Seek(context.Background(), 3725); err != nil || tv.target != "1:02:05" {
		t.Fatalf("seek: %v %q", err, tv.target)
	}
	p.Pause(context.Background())
	if st, _ := p.Status(context.Background()); st.State != StatePaused {
		t.Fatalf("paused: %+v", st)
	}
	p.Stop(context.Background())
	if st, _ := p.Status(context.Background()); st.State != StateIdle {
		t.Fatalf("stopped: %+v", st)
	}
	if err := p.SetVolume(context.Background(), 0.5); err != ErrUnsupported {
		t.Fatalf("volume: %v", err)
	}
	dp := p.(*dlnaPlayer)
	if _, err := dp.call(context.Background(), "Dance", nil); err == nil || !strings.Contains(err.Error(), "Invalid Action") {
		t.Fatalf("fault: %v", err)
	}
}

func TestParseHMS(t *testing.T) {
	for in, want := range map[string]float64{"0:00:00": 0, "1:02:03": 3723, "00:00:05.500": 5.5, "NOT_IMPLEMENTED": 0, "": 0} {
		if got := parseHMS(in); got != want {
			t.Errorf("parseHMS(%q) = %v, want %v", in, got, want)
		}
	}
}
