package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"bingebandit/internal/store"
)

// sportsWatchLinks tells Stat Scraps (the CFB Tracker) which Live TV channel
// shows each TV network, so its game pages can offer "Watch on BingeBandit"
// straight to the channel:
//
//	GET /api/sports/watch-links?network=ESPN2&network=ABC
//	X-Api-Key: <the key under Server → Settings → Sports data>
//
// It answers {"links": [{network, channel: {id, number, name}, path}]} for
// the networks it has a channel for, leaving the others out. path is the web
// app's page for the channel (/live/{id}); opening it asks for a sign-in as
// usual, so the list only says which channels exist, never plays them.
//
// The key is the same one this server sends the tracker for its ESPN relay,
// so pairing the two apps once covers both directions. With no key set it
// answers 404.
func (s *Server) sportsWatchLinks(w http.ResponseWriter, r *http.Request) {
	key := s.Store.Setting(store.SettingSportsRelayKey)
	if key == "" {
		writeErr(w, http.StatusNotFound, "channel links are off: set the sports data key under Server → Settings → Sports data")
		return
	}
	if !sameSecret(r.Header.Get("X-Api-Key"), key) {
		writeErr(w, http.StatusUnauthorized, "wrong or missing X-Api-Key")
		return
	}
	var networks []string
	seen := map[string]bool{}
	for _, n := range r.URL.Query()["network"] {
		n = strings.TrimSpace(n)
		if n == "" || len(n) > 80 || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		networks = append(networks, n)
		if len(networks) == 12 {
			break
		}
	}
	links := []watchLink{}
	if len(networks) > 0 {
		chans, err := s.Store.Channels(0, false)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		mapped, err := s.Store.Networks()
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		for _, n := range networks {
			if c := channelForNetwork(n, mapped, chans); c != nil {
				links = append(links, watchLink{n, sportChannel{ID: c.ID, Number: c.Number, Name: c.Name}, "/live/" + strconv.FormatInt(c.ID, 10)})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

type watchLink struct {
	Network string       `json:"network"`
	Channel sportChannel `json:"channel"`
	Path    string       `json:"path"`
}

func sameSecret(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}

// channelForNetwork picks the channel to send a network's viewers to: the
// first one an admin tied to it (Admin → Live TV → Networks), or else the
// lowest-numbered channel named for it ("ESPN2" is "US: ESPN 2 HD").
// chans are the enabled channels in number order.
func channelForNetwork(network string, mapped []store.NetworkChannels, chans []*store.Channel) *store.Channel {
	byID := make(map[int64]*store.Channel, len(chans))
	for _, c := range chans {
		byID[c.ID] = c
	}
	for _, m := range mapped {
		if !strings.EqualFold(strings.TrimSpace(m.Network), network) {
			continue
		}
		for _, id := range m.Channels {
			if c := byID[id]; c != nil {
				return c
			}
		}
	}
	want := networkKey(network)
	if want == "" {
		return nil
	}
	for _, c := range chans {
		if networkKey(c.Name) == want {
			return c
		}
	}
	return nil
}

var (
	reCountryTag = regexp.MustCompile(`^\s*(us|usa)\s*[:|/-]+\s*`)
	reNonAlnum   = regexp.MustCompile(`[^a-z0-9+]+`)
)

// Words in channel names that don't change which network it is.
var channelNoise = map[string]bool{"hd": true, "fhd": true, "uhd": true, "sd": true, "4k": true, "east": true, "channel": true}

// Short forms ESPN and providers use, spelled out.
var networkAliases = map[string]string{
	"cbssn": "cbssportsnetwork", "btn": "bigtennetwork", "secn": "secnetwork", "accn": "accnetwork",
	"nfln": "nflnetwork", "mlbn": "mlbnetwork", "nhln": "nhlnetwork", "usanet": "usanetwork", "usa": "usanetwork",
	"cw": "thecw", "foxsports1": "fs1", "foxsports2": "fs2",
}

// networkKey reduces a network or channel name to a comparable key:
// lower case, no country tag, no HD/East, no spaces.
func networkKey(name string) string {
	s := strings.ToLower(name)
	s = reCountryTag.ReplaceAllString(s, "")
	var b strings.Builder
	for _, w := range strings.Fields(reNonAlnum.ReplaceAllString(s, " ")) {
		if !channelNoise[w] {
			b.WriteString(w)
		}
	}
	k := b.String()
	if a, ok := networkAliases[k]; ok {
		return a
	}
	return k
}
