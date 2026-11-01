// Command fakedlna is a development tool that pretends to be a DLNA smart
// TV: it answers SSDP searches, takes AVTransport commands (play, pause,
// seek, stop) and "plays" what it's sent by downloading it at roughly real
// time, so casting can be tried end to end without a TV.
//
//	go run ./cmd/fakedlna -name "Fake TV"
//
// The server finds it by multicast; where that doesn't work, list
// 127.0.0.1 under Admin → Settings → Casting → TVs to look for by address.
package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const st = "urn:schemas-upnp-org:device:MediaRenderer:1"

type tv struct {
	mu       sync.Mutex
	state    string // NO_MEDIA_PRESENT | PLAYING | PAUSED_PLAYBACK | STOPPED
	uri      string
	title    string
	pos      float64 // seconds played
	since    time.Time
	gen      int // bumps on each new URI, ending the old download
	received int64
	duration float64 // from the metadata, when sent
}

func (t *tv) position() float64 {
	if t.state == "PLAYING" {
		return t.pos + time.Since(t.since).Seconds()
	}
	return t.pos
}

// play downloads uri as a TV would, about as fast as it plays (assuming a
// few megabits), and stops at its end.
func (t *tv) play(uri string, gen int) {
	res, err := http.Get(uri)
	if err != nil {
		log.Printf("can't open %s: %v", uri, err)
		t.stopIf(gen)
		return
	}
	defer res.Body.Close()
	log.Printf("opened: HTTP %d, %s", res.StatusCode, res.Header.Get("Content-Type"))
	if res.StatusCode != http.StatusOK {
		t.stopIf(gen)
		return
	}
	buf := make([]byte, 64<<10)
	for {
		t.mu.Lock()
		cur, state := t.gen, t.state
		t.mu.Unlock()
		if cur != gen {
			return
		}
		if state != "PLAYING" {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		n, err := res.Body.Read(buf)
		t.mu.Lock()
		t.received += int64(n)
		t.mu.Unlock()
		if err != nil {
			log.Printf("end of stream after %d bytes (%v)", t.received, err)
			// A TV buffers ahead; it stops when it has shown what it got.
			for {
				t.mu.Lock()
				done := t.gen != gen || t.duration == 0 || t.position() >= t.duration
				t.mu.Unlock()
				if done {
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			t.stopIf(gen)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (t *tv) stopIf(gen int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.gen == gen {
		t.pos = t.position()
		t.state = "STOPPED"
	}
}

var reDuration = regexp.MustCompile(`duration="([0-9:.]+)"`)

func parseHMS(s string) float64 {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	sec, _ := strconv.ParseFloat(parts[2], 64)
	return float64(h*3600+m*60) + sec
}

func hms(s float64) string {
	n := int(s)
	return fmt.Sprintf("%d:%02d:%02d", n/3600, n/60%60, n%60)
}

func args(body []byte) map[string]string {
	out := map[string]string{}
	dec := xml.NewDecoder(bytes.NewReader(body))
	var name string
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch v := tok.(type) {
		case xml.StartElement:
			name = v.Name.Local
		case xml.CharData:
			if name != "" {
				out[name] += string(v)
			}
		case xml.EndElement:
			name = ""
		}
	}
}

func (t *tv) control(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	action := r.Header.Get("SOAPACTION")
	action = strings.Trim(action[strings.LastIndex(action, "#")+1:], `"`)
	a := args(body)
	t.mu.Lock()
	reply := ""
	switch action {
	case "SetAVTransportURI":
		t.gen++
		t.uri, t.state, t.pos, t.received = a["CurrentURI"], "STOPPED", 0, 0
		if m := args([]byte(a["CurrentURIMetaData"])); m["title"] != "" {
			t.title = m["title"]
		}
		t.duration = 0
		if m := reDuration.FindStringSubmatch(a["CurrentURIMetaData"]); m != nil {
			t.duration = parseHMS(m[1])
		}
		log.Printf("now showing %q: %s", t.title, t.uri)
	case "Play":
		if t.state != "PLAYING" {
			if t.state == "STOPPED" && t.uri != "" && t.received == 0 {
				go t.play(t.uri, t.gen)
			}
			t.state, t.since = "PLAYING", time.Now()
		}
	case "Pause":
		t.pos, t.state = t.position(), "PAUSED_PLAYBACK"
	case "Stop":
		t.gen++
		t.pos, t.state = 0, "STOPPED"
	case "Seek":
		t.pos, t.since = parseHMS(a["Target"]), time.Now()
	case "GetTransportInfo":
		reply = "<CurrentTransportState>" + t.state + "</CurrentTransportState><CurrentTransportStatus>OK</CurrentTransportStatus><CurrentSpeed>1</CurrentSpeed>"
	case "GetPositionInfo":
		reply = "<Track>1</Track><RelTime>" + hms(t.position()) + "</RelTime><TrackURI>" + html.EscapeString(t.uri) + "</TrackURI>"
	default:
		t.mu.Unlock()
		w.WriteHeader(500)
		io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>401</errorCode><errorDescription>Invalid Action</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
		return
	}
	if action != "GetTransportInfo" && action != "GetPositionInfo" {
		log.Printf("%s %v → %s at %s", action, a["Target"], t.state, hms(t.position()))
	}
	t.mu.Unlock()
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">%s</u:%sResponse></s:Body></s:Envelope>`, action, reply, action)
}

func main() {
	name := flag.String("name", "Fake TV", "the TV's name")
	httpAddr := flag.String("http", ":8200", "where the TV's UPnP services listen")
	flag.Parse()
	t := &tv{state: "NO_MEDIA_PRESENT"}
	_, port, _ := net.SplitHostPort(*httpAddr)

	mux := http.NewServeMux()
	mux.HandleFunc("/desc.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprintf(w, `<?xml version="1.0"?><root xmlns="urn:schemas-upnp-org:device-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><device>
<deviceType>%s</deviceType><friendlyName>%s</friendlyName><manufacturer>BingeBandit</manufacturer><modelName>fakedlna</modelName>
<UDN>uuid:fakedlna-%s</UDN><serviceList><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType>
<serviceId>urn:upnp-org:serviceId:AVTransport</serviceId><controlURL>/AVTransport/control</controlURL><SCPDURL>/AVTransport.xml</SCPDURL></service>
</serviceList></device></root>`, st, html.EscapeString(*name), port)
	})
	mux.HandleFunc("/AVTransport/control", t.control)
	go func() { log.Fatal(http.ListenAndServe(*httpAddr, mux)) }()

	// SSDP: the multicast group if this machine allows it, else plain
	// port 1900 for searches sent straight to it.
	group := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	conn, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		log.Printf("multicast: %v; answering searches sent to port 1900 only", err)
		if conn, err = net.ListenUDP("udp4", &net.UDPAddr{Port: 1900}); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("%q answering SSDP on :1900, services on %s", *name, *httpAddr)
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Fatal(err)
		}
		msg := string(buf[:n])
		if !strings.HasPrefix(msg, "M-SEARCH") || !(strings.Contains(msg, st) || strings.Contains(msg, "ssdp:all")) {
			continue
		}
		// Answer with the address the searcher can reach us at.
		local := "127.0.0.1"
		if c, err := net.Dial("udp", from.String()); err == nil {
			local = c.LocalAddr().(*net.UDPAddr).IP.String()
			c.Close()
		}
		reply := fmt.Sprintf("HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nEXT:\r\nLOCATION: http://%s/desc.xml\r\nSERVER: fakedlna UPnP/1.0\r\nST: %s\r\nUSN: uuid:fakedlna-%s::%s\r\n\r\n",
			net.JoinHostPort(local, port), st, port, st)
		conn.WriteToUDP([]byte(reply), from)
	}
}
