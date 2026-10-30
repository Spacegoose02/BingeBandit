package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TV app download: /tv hands out the newest TV app APK with this server's
// address written into it, so someone setting up a TV only types one short
// address (in the Downloader app) and the app then opens already pointed at
// this server, straight to the sign-in code.
//
// The address goes in the APK Signing Block as an extra ID-value pair. APK
// signature schemes v2 and v3 sign everything except that block, so the
// signature still checks out (the same trick Android "channel" packagers use).
// The app reads it back from its installed APK (ApkStamp.kt).

// apkStampID marks our pair in the signing block ("BBsv").
const apkStampID = 0x76734242

const (
	apkSigBlockMagic = "APK Sig Block 42"
	verityPaddingID  = 0x42726577 // apksigner's padding to a 4096-byte block
)

type tvApp struct {
	mu      sync.Mutex
	version string
	raw     []byte // the release APK, as downloaded
	stamp   string // what stamped was made for
	stamped []byte
	used    time.Time
}

// tvAppDownload serves the TV app with this server's address inside.
func (s *Server) tvAppDownload(w http.ResponseWriter, r *http.Request) {
	rel, err := s.updates.get(r.Context(), s.UpdateRepo)
	if err != nil {
		http.Error(w, "Couldn't get the TV app right now: "+err.Error(), http.StatusBadGateway)
		return
	}
	if rel == nil {
		http.Error(w, "There's no TV app release to download yet.", http.StatusNotFound)
		return
	}
	// The address typed to get here works from where the TV is, so it goes
	// first; the public URL is a second choice.
	urls := []string{baseURL(r)}
	if pu := s.policy().publicURL; pu != "" && !strings.EqualFold(pu, urls[0]) {
		urls = append(urls, pu)
	}
	body, err := s.tvApp.get(s, rel, urls)
	if err != nil {
		// Fall back to the plain APK: the TV then asks for the address.
		http.Redirect(w, r, rel.URL, http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="BingeBandit-TV-%s.apk"`, rel.Version))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

func (t *tvApp) get(s *Server, rel *appRelease, urls []string) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.version != rel.Version || t.raw == nil {
		raw, err := s.fetchTVApp(rel)
		if err != nil {
			return nil, err
		}
		t.version, t.raw, t.stamp, t.stamped = rel.Version, raw, "", nil
	}
	payload, _ := json.Marshal(map[string]any{"servers": urls})
	if t.stamp != string(payload) || t.stamped == nil {
		out, err := stampAPK(t.raw, payload)
		if err != nil {
			return nil, err
		}
		t.stamp, t.stamped = string(payload), out
	}
	if t.used.IsZero() {
		go t.forgetWhenIdle()
	}
	t.used = time.Now()
	return t.stamped, nil
}

// forgetWhenIdle lets go of the APKs (tens of MB) once nobody has downloaded
// for a while; the copy on disk makes the next download quick.
func (t *tvApp) forgetWhenIdle() {
	for {
		time.Sleep(5 * time.Minute)
		t.mu.Lock()
		if time.Since(t.used) > 15*time.Minute {
			t.raw, t.stamped, t.stamp, t.used = nil, nil, "", time.Time{}
			t.mu.Unlock()
			return
		}
		t.mu.Unlock()
	}
}

// fetchTVApp reads the release APK from the cache, downloading it first.
func (s *Server) fetchTVApp(rel *appRelease) ([]byte, error) {
	dir := filepath.Join(s.DataDir, "cache", "tv-app")
	path := filepath.Join(dir, "BingeBandit-TV-"+filepath.Base(rel.Version)+".apk")
	if b, err := os.ReadFile(path); err == nil && (rel.Size == 0 || int64(len(b)) == rel.Size) {
		return b, nil
	}
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Get(rel.URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, err
	}
	if rel.Size != 0 && int64(len(b)) != rel.Size {
		return nil, fmt.Errorf("download was cut short")
	}
	// Only the current version is kept.
	os.RemoveAll(dir)
	if os.MkdirAll(dir, 0o755) == nil {
		os.WriteFile(path, b, 0o644)
	}
	return b, nil
}

// stampAPK returns a copy of apk with payload in its signing block under
// apkStampID, replacing an earlier stamp. When the block has apksigner's
// padding, the padding shrinks to make room so nothing else moves.
func stampAPK(apk, payload []byte) ([]byte, error) {
	eocd := findEOCD(apk)
	if eocd < 0 {
		return nil, errors.New("not a zip file")
	}
	cdOffset := int(binary.LittleEndian.Uint32(apk[eocd+16:]))
	if cdOffset < 32 || cdOffset > eocd || string(apk[cdOffset-16:cdOffset]) != apkSigBlockMagic {
		return nil, errors.New("APK has no v2 signature")
	}
	size := int(binary.LittleEndian.Uint64(apk[cdOffset-24:]))
	start := cdOffset - size - 8
	if size < 24 || start < 0 || int(binary.LittleEndian.Uint64(apk[start:])) != size {
		return nil, errors.New("APK signing block is damaged")
	}

	// Read the pairs, dropping an old stamp and keeping any padding apart.
	type pair struct {
		id  uint32
		val []byte
	}
	var pairs []pair
	padding := -1
	for p, end := start+8, cdOffset-24; p < end; {
		if p+12 > end {
			return nil, errors.New("APK signing block is damaged")
		}
		n := int(binary.LittleEndian.Uint64(apk[p:]))
		if n < 4 || p+8+n > end {
			return nil, errors.New("APK signing block is damaged")
		}
		id := binary.LittleEndian.Uint32(apk[p+8:])
		switch {
		case id == apkStampID:
		case id == verityPaddingID:
			padding = n - 4
		default:
			pairs = append(pairs, pair{id, apk[p+12 : p+8+n]})
		}
		p += 8 + n
	}
	pairs = append(pairs, pair{apkStampID, payload})

	var body bytes.Buffer
	for _, pr := range pairs {
		binary.Write(&body, binary.LittleEndian, uint64(len(pr.val)+4))
		binary.Write(&body, binary.LittleEndian, pr.id)
		body.Write(pr.val)
	}
	if padding >= 0 {
		// Keep the block the size it was if the padding has room.
		if left := size - 8 - 16 - body.Len() - 12; left >= 0 {
			padding = left
		} else {
			padding = 0
		}
		binary.Write(&body, binary.LittleEndian, uint64(padding+4))
		binary.Write(&body, binary.LittleEndian, uint32(verityPaddingID))
		body.Write(make([]byte, padding))
	}
	newSize := body.Len() + 8 + 16

	out := make([]byte, 0, len(apk)-size+newSize+8)
	out = append(out, apk[:start]...)
	out = binary.LittleEndian.AppendUint64(out, uint64(newSize))
	out = append(out, body.Bytes()...)
	out = binary.LittleEndian.AppendUint64(out, uint64(newSize))
	out = append(out, apkSigBlockMagic...)
	newCD := len(out)
	out = append(out, apk[cdOffset:]...)
	binary.LittleEndian.PutUint32(out[newCD+(eocd-cdOffset)+16:], uint32(newCD))
	return out, nil
}

// readAPKStamp returns the payload stampAPK wrote, or nil.
func readAPKStamp(apk []byte) []byte {
	eocd := findEOCD(apk)
	if eocd < 0 {
		return nil
	}
	cdOffset := int(binary.LittleEndian.Uint32(apk[eocd+16:]))
	if cdOffset < 32 || cdOffset > eocd || string(apk[cdOffset-16:cdOffset]) != apkSigBlockMagic {
		return nil
	}
	start := cdOffset - int(binary.LittleEndian.Uint64(apk[cdOffset-24:])) - 8
	if start < 0 {
		return nil
	}
	for p, end := start+8, cdOffset-24; p+12 <= end; {
		n := int(binary.LittleEndian.Uint64(apk[p:]))
		if n < 4 || p+8+n > end {
			return nil
		}
		if binary.LittleEndian.Uint32(apk[p+8:]) == apkStampID {
			return apk[p+12 : p+8+n]
		}
		p += 8 + n
	}
	return nil
}

// findEOCD finds the zip's end of central directory record.
func findEOCD(b []byte) int {
	for i := len(b) - 22; i >= 0 && i >= len(b)-22-0xffff; i-- {
		if binary.LittleEndian.Uint32(b[i:]) == 0x06054b50 && int(binary.LittleEndian.Uint16(b[i+20:]))+i+22 == len(b) {
			return i
		}
	}
	return -1
}
