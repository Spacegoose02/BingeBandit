package cast

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Discover looks for Chromecasts (mDNS) and DLNA renderers (SSDP) for wait,
// and probes the extra addresses (devices an admin typed in, for servers in
// a Docker bridge network where multicast doesn't get out).
func Discover(ctx context.Context, wait time.Duration, extra []string) []Device {
	var mu sync.Mutex
	found := map[string]Device{}
	add := func(d Device) {
		mu.Lock()
		defer mu.Unlock()
		if old, ok := found[d.ID]; ok && old.Manual {
			d.Manual = true
		}
		found[d.ID] = d
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := discoverChromecasts(ctx, add); err != nil {
			log.Printf("cast: mDNS: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := discoverDLNA(ctx, "239.255.255.250:1900", add); err != nil {
			log.Printf("cast: SSDP: %v", err)
		}
	}()
	for _, host := range extra {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, d := range Probe(ctx, host) {
				d.Manual = true
				add(d)
			}
		}()
	}
	wg.Wait()
	out := make([]Device, 0, len(found))
	for _, d := range found {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Probe asks one address what it is: a Cast device on port 8009 and/or a
// DLNA renderer answering a unicast SSDP search.
func Probe(ctx context.Context, host string) []Device {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	var mu sync.Mutex
	var out []Device
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if d, ok := probeChromecast(ctx, host); ok {
			mu.Lock()
			out = append(out, d)
			mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		discoverDLNA(ctx, net.JoinHostPort(host, "1900"), func(d Device) {
			mu.Lock()
			out = append(out, d)
			mu.Unlock()
		})
	}()
	wg.Wait()
	return out
}

func probeChromecast(ctx context.Context, host string) (Device, bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addr := net.JoinHostPort(host, "8009")
	conn, err := castDialer(ctx, addr)
	if err != nil {
		return Device{}, false
	}
	conn.Close()
	d := Device{ID: "cc-" + host, Name: "Chromecast (" + host + ")", Kind: KindChromecast, Host: host, addr: addr}
	// Cast devices describe themselves on port 8008.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, "8008")+"/setup/eureka_info?params=name,device_info", nil)
	if res, err := httpClient.Do(req); err == nil {
		var info struct {
			Name       string `json:"name"`
			DeviceInfo struct {
				Model string `json:"model_name"`
				SSDP  string `json:"ssdp_udn"`
			} `json:"device_info"`
		}
		if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&info) == nil {
			if info.Name != "" {
				d.Name = info.Name
			}
			d.Model = info.DeviceInfo.Model
			if info.DeviceInfo.SSDP != "" {
				d.ID = "cc-" + strings.ReplaceAll(info.DeviceInfo.SSDP, "-", "")
			}
		}
		res.Body.Close()
	}
	return d, true
}

// --- mDNS ---------------------------------------------------------------

const castService = "_googlecast._tcp.local"

// dnsQuestion builds a one-question PTR query.
func dnsQuestion(name string) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[4:], 1) // QDCOUNT
	for _, label := range strings.Split(name, ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0, 0, 12, 0, 1) // PTR, IN
}

// dnsName reads a possibly compressed name at off; it returns the name and
// the offset after it.
func dnsName(msg []byte, off int) (string, int, error) {
	var labels []string
	end := -1
	for jumps := 0; ; {
		if off >= len(msg) {
			return "", 0, errors.New("dns: name out of range")
		}
		l := int(msg[off])
		switch {
		case l == 0:
			if end < 0 {
				end = off + 1
			}
			return strings.Join(labels, "."), end, nil
		case l&0xC0 == 0xC0:
			if off+1 >= len(msg) || jumps > 20 {
				return "", 0, errors.New("dns: bad pointer")
			}
			if end < 0 {
				end = off + 2
			}
			off = int(binary.BigEndian.Uint16(msg[off:]) & 0x3FFF)
			jumps++
		default:
			if off+1+l > len(msg) {
				return "", 0, errors.New("dns: label out of range")
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
}

type dnsRecord struct {
	name  string
	typ   uint16
	data  []byte
	start int // offset of data in the message, for names inside it
}

func parseDNS(msg []byte) ([]dnsRecord, error) {
	if len(msg) < 12 {
		return nil, errors.New("dns: short message")
	}
	qd := int(binary.BigEndian.Uint16(msg[4:]))
	total := int(binary.BigEndian.Uint16(msg[6:])) + int(binary.BigEndian.Uint16(msg[8:])) + int(binary.BigEndian.Uint16(msg[10:]))
	off := 12
	for i := 0; i < qd; i++ {
		_, next, err := dnsName(msg, off)
		if err != nil {
			return nil, err
		}
		off = next + 4
	}
	var out []dnsRecord
	for i := 0; i < total; i++ {
		name, next, err := dnsName(msg, off)
		if err != nil || next+10 > len(msg) {
			return out, err
		}
		typ := binary.BigEndian.Uint16(msg[next:])
		rdlen := int(binary.BigEndian.Uint16(msg[next+8:]))
		start := next + 10
		if start+rdlen > len(msg) {
			return out, errors.New("dns: record out of range")
		}
		out = append(out, dnsRecord{name: strings.ToLower(name), typ: typ, data: msg[start : start+rdlen], start: start})
		off = start + rdlen
	}
	return out, nil
}

// txtValues reads key=value strings from a TXT record.
func txtValues(data []byte) map[string]string {
	out := map[string]string{}
	for len(data) > 0 {
		l := int(data[0])
		if 1+l > len(data) {
			break
		}
		k, v, _ := strings.Cut(string(data[1:1+l]), "=")
		out[strings.ToLower(k)] = v
		data = data[1+l:]
	}
	return out
}

// castFromMDNS reads Cast devices out of one mDNS answer from src.
func castFromMDNS(msg []byte, src net.IP) []Device {
	recs, _ := parseDNS(msg)
	type inst struct {
		txt  map[string]string
		port int
		host string
	}
	insts := map[string]*inst{}
	get := func(name string) *inst {
		if insts[name] == nil {
			insts[name] = &inst{}
		}
		return insts[name]
	}
	addrs := map[string]net.IP{}
	for _, r := range recs {
		switch r.typ {
		case 12: // PTR
			if r.name == castService {
				if target, _, err := dnsName(msg, r.start); err == nil {
					get(strings.ToLower(target))
				}
			}
		case 16: // TXT
			if strings.HasSuffix(r.name, "."+castService) {
				get(r.name).txt = txtValues(r.data)
			}
		case 33: // SRV
			if strings.HasSuffix(r.name, "."+castService) && len(r.data) > 6 {
				in := get(r.name)
				in.port = int(binary.BigEndian.Uint16(r.data[4:]))
				in.host, _, _ = dnsName(msg, r.start+6)
				in.host = strings.ToLower(in.host)
			}
		case 1: // A
			if len(r.data) == 4 {
				addrs[r.name] = net.IP(r.data)
			}
		}
	}
	var out []Device
	for name, in := range insts {
		ip := src
		if a, ok := addrs[in.host]; ok {
			ip = a
		}
		if ip == nil {
			continue
		}
		port := in.port
		if port == 0 {
			port = 8009
		}
		id := in.txt["id"]
		if id == "" {
			id = name
		}
		d := Device{
			ID: "cc-" + strings.ToLower(id), Name: in.txt["fn"], Kind: KindChromecast, Model: in.txt["md"],
			Host: ip.String(), addr: net.JoinHostPort(ip.String(), fmt.Sprint(port)),
		}
		if d.Name == "" {
			d.Name = strings.TrimSuffix(name, "."+castService)
		}
		// Cast groups (several speakers at once) can't show video.
		if in.txt["md"] == "Google Cast Group" {
			continue
		}
		out = append(out, d)
	}
	return out
}

func discoverChromecasts(ctx context.Context, add func(Device)) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return err
	}
	defer conn.Close()
	group := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	q := dnsQuestion(castService)
	// The query goes out from an ordinary port, so devices answer it
	// straight back to us (RFC 6762 "legacy unicast"); no need to join the
	// multicast group. Ask again after a moment in case the first was lost.
	if _, err := conn.WriteToUDP(q, group); err != nil {
		return err
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(700 * time.Millisecond):
			conn.WriteToUDP(q, group)
		}
	}()
	return readUntil(ctx, conn, func(b []byte, src *net.UDPAddr) {
		for _, d := range castFromMDNS(b, src.IP) {
			add(d)
		}
	})
}

func readUntil(ctx context.Context, conn *net.UDPConn, fn func([]byte, *net.UDPAddr)) error {
	if dl, ok := ctx.Deadline(); ok {
		conn.SetReadDeadline(dl)
	}
	go func() {
		<-ctx.Done()
		conn.SetReadDeadline(time.Now())
	}()
	buf := make([]byte, 9000)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return nil
			}
			return err
		}
		fn(append([]byte(nil), buf[:n]...), src)
	}
}

// --- SSDP / UPnP ----------------------------------------------------------

const rendererType = "urn:schemas-upnp-org:device:MediaRenderer:1"

func discoverDLNA(ctx context.Context, target string, add func(Device)) error {
	raddr, err := net.ResolveUDPAddr("udp4", target)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return err
	}
	defer conn.Close()
	msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: " + rendererType + "\r\n\r\n"
	if _, err := conn.WriteToUDP([]byte(msg), raddr); err != nil {
		return err
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(700 * time.Millisecond):
			conn.WriteToUDP([]byte(msg), raddr)
		}
	}()
	seen := map[string]bool{}
	var wg sync.WaitGroup
	err = readUntil(ctx, conn, func(b []byte, src *net.UDPAddr) {
		loc := ssdpHeader(string(b), "location")
		if loc == "" || seen[loc] {
			return
		}
		seen[loc] = true
		// Only fetch descriptions from the device that answered, so a
		// stray packet can't point the server somewhere else.
		u, err := url.Parse(loc)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() != src.IP.String() {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			dctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if d, err := describeRenderer(dctx, loc); err == nil {
				add(d)
			}
		}()
	})
	wg.Wait()
	return err
}

func ssdpHeader(resp, name string) string {
	for _, line := range strings.Split(resp, "\r\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type upnpDevice struct {
	DeviceType   string        `xml:"deviceType"`
	FriendlyName string        `xml:"friendlyName"`
	Manufacturer string        `xml:"manufacturer"`
	ModelName    string        `xml:"modelName"`
	UDN          string        `xml:"UDN"`
	Services     []upnpService `xml:"serviceList>service"`
	Devices      []upnpDevice  `xml:"deviceList>device"`
}

type upnpService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

type upnpRoot struct {
	URLBase string     `xml:"URLBase"`
	Device  upnpDevice `xml:"device"`
}

// describeRenderer reads a UPnP device description and finds its
// AVTransport service.
func describeRenderer(ctx context.Context, loc string) (Device, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	if err != nil {
		return Device{}, err
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return Device{}, err
	}
	defer res.Body.Close()
	return parseDescription(io.LimitReader(res.Body, 1<<20), loc)
}

func parseDescription(r io.Reader, loc string) (Device, error) {
	var root upnpRoot
	if err := xml.NewDecoder(r).Decode(&root); err != nil {
		return Device{}, err
	}
	base, err := url.Parse(loc)
	if err != nil {
		return Device{}, err
	}
	if root.URLBase != "" {
		if b, err := url.Parse(root.URLBase); err == nil {
			base = b
		}
	}
	var find func(d upnpDevice) (upnpDevice, upnpService, bool)
	find = func(d upnpDevice) (upnpDevice, upnpService, bool) {
		for _, s := range d.Services {
			if strings.Contains(s.ServiceType, ":service:AVTransport:") {
				return d, s, true
			}
		}
		for _, c := range d.Devices {
			if dd, s, ok := find(c); ok {
				return dd, s, true
			}
		}
		return d, upnpService{}, false
	}
	dev, svc, ok := find(root.Device)
	if !ok {
		return Device{}, errNoAVTransport
	}
	ctl, err := base.Parse(strings.TrimSpace(svc.ControlURL))
	if err != nil {
		return Device{}, err
	}
	name := strings.TrimSpace(root.Device.FriendlyName)
	if name == "" {
		name = strings.TrimSpace(dev.FriendlyName)
	}
	if name == "" {
		name = "TV (" + base.Hostname() + ")"
	}
	model := strings.TrimSpace(strings.TrimSpace(root.Device.Manufacturer) + " " + strings.TrimSpace(root.Device.ModelName))
	id := strings.TrimPrefix(strings.TrimSpace(root.Device.UDN), "uuid:")
	if id == "" {
		id = base.Host
	}
	return Device{
		ID: "dlna-" + strings.ToLower(id), Name: name, Kind: KindDLNA, Model: model, Host: base.Hostname(),
		controlURL: ctl.String(), serviceType: strings.TrimSpace(svc.ServiceType),
	}, nil
}
