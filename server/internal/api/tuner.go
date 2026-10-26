package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// The tuner routes pretend to be an HDHomeRun so Plex (and Emby/Jellyfin) can
// use BingeBandit's channels for Live TV. Plex only takes tuners, not M3U, and
// can't send headers; it appends paths to the tuner address, so the key goes
// in the path rather than a query string:
// /api/live/tuner/{key}/discover.json, lineup.json, epg.xml and so on.
func (s *Server) tunerRoutes(r chi.Router) {
	r.Use(tunerKey)
	r.Use(s.requireAuth)
	r.Use(requireLiveTV)
	r.Get("/", s.tunerDiscover)
	r.Get("/discover.json", s.tunerDiscover)
	r.Get("/device.xml", s.tunerDevice)
	r.Get("/lineup.json", s.tunerLineup)
	r.Get("/lineup_status.json", tunerLineupStatus)
	r.HandleFunc("/lineup.post", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/epg.xml", s.liveEPG)
	r.Get("/playlist.m3u", s.livePlaylist)
	// Streams too, so the lineup's links carry the key in the path: Plex
	// appends "?transcode=heavy" to them as if they had no query.
	r.Get("/channels/{id}/stream.ts", s.liveTS)
}

// tunerKey hands the key in the path to requireAuth as a bearer token,
// dropping any cookie so the tuner always acts as the key's owner.
func tunerKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.Clone(r.Context())
		r.Header.Del("Cookie")
		r.Header.Set("Authorization", "Bearer "+chi.URLParam(r, "key"))
		q := r.URL.Query()
		q.Del("token")
		r.URL.RawQuery = q.Encode()
		next.ServeHTTP(w, r)
	})
}

func tunerBase(r *http.Request) string {
	return baseURL(r) + "/api/live/tuner/" + chi.URLParam(r, "key")
}

// tunerDeviceID is a stable 8-hex-digit id per user, so Plex sees the same
// tuner across restarts but each user's key shows up as its own device.
func tunerDeviceID(uid int64) string {
	sum := sha256.Sum256([]byte("bingebandit-tuner-" + strconv.FormatInt(uid, 10)))
	return strings.ToUpper(hex.EncodeToString(sum[:4]))
}

func (s *Server) tunerDiscover(w http.ResponseWriter, r *http.Request) {
	base := tunerBase(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"FriendlyName":    "BingeBandit",
		"Manufacturer":    "Silicondust",
		"ModelNumber":     "HDTC-2US",
		"FirmwareName":    "hdhomeruntc_atsc",
		"FirmwareVersion": "20200101",
		"DeviceID":        tunerDeviceID(currentUser(r).ID),
		"DeviceAuth":      "bingebandit",
		"BaseURL":         base,
		"LineupURL":       base + "/lineup.json",
		"TunerCount":      4,
	})
}

func (s *Server) tunerDevice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
 <URLBase>%s</URLBase>
 <specVersion><major>1</major><minor>0</minor></specVersion>
 <device>
  <deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
  <friendlyName>BingeBandit</friendlyName>
  <manufacturer>Silicondust</manufacturer>
  <modelName>HDTC-2US</modelName>
  <modelNumber>HDTC-2US</modelNumber>
  <serialNumber></serialNumber>
  <UDN>uuid:%s</UDN>
 </device>
</root>
`, tunerBase(r), tunerDeviceID(currentUser(r).ID))
}

func (s *Server) tunerLineup(w http.ResponseWriter, r *http.Request) {
	chans, err := s.Store.Channels(currentUser(r).ID, false)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	base := tunerBase(r)
	type entry struct {
		GuideNumber string
		GuideName   string
		URL         string
	}
	out := make([]entry, 0, len(chans))
	for _, c := range chans {
		out = append(out, entry{
			GuideNumber: strconv.FormatFloat(c.Number, 'f', -1, 64),
			GuideName:   c.Name,
			URL:         fmt.Sprintf("%s/channels/%d/stream.ts", base, c.ID),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func tunerLineupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ScanInProgress": 0, "ScanPossible": 1, "Source": "Cable", "SourceList": []string{"Cable"},
	})
}
