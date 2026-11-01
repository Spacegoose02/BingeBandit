package api

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"bingebandit/internal/store"
)

func TestNetworkKey(t *testing.T) {
	for name, want := range map[string]string{
		"ESPN2":              "espn2",
		"US: ESPN 2 HD":      "espn2",
		"USA | ESPN2 (East)": "espn2",
		"USA Net":            "usanetwork",
		"US: USA Network HD": "usanetwork",
		"CBSSN":              "cbssportsnetwork",
		"CBS Sports Network": "cbssportsnetwork",
		"FOX":                "fox",
		"Fox News":           "foxnews",
		"ESPN+":              "espn+",
		"The CW":             "thecw",
	} {
		if got := networkKey(name); got != want {
			t.Errorf("networkKey(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSportsWatchLinks(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	espn2 := &store.Channel{Number: 206, Name: "US: ESPN 2 HD", Enabled: true}
	fox := &store.Channel{Number: 5, Name: "FOX News", Enabled: true}
	wdbj := &store.Channel{Number: 7, Name: "WDBJ", Enabled: true}
	off := &store.Channel{Number: 300, Name: "ESPNU", Enabled: false}
	for _, c := range []*store.Channel{espn2, fox, wdbj, off} {
		if err := st.CreateChannel(c, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetNetworks([]store.NetworkChannels{{Network: "CBS", Channels: []int64{wdbj.ID}}}); err != nil {
		t.Fatal(err)
	}
	srv := &Server{Store: st}

	get := func(key, query string) (int, []watchLink) {
		req := httptest.NewRequest("GET", "/api/sports/watch-links?"+query, nil)
		if key != "" {
			req.Header.Set("X-Api-Key", key)
		}
		w := httptest.NewRecorder()
		srv.sportsWatchLinks(w, req)
		var out struct {
			Links []watchLink `json:"links"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.Links
	}

	if code, _ := get("anything", "network=ESPN2"); code != 404 {
		t.Fatalf("no key set: got %d, want 404", code)
	}
	if err := st.SetSettings(map[string]string{store.SettingSportsRelayKey: "s3cret"}); err != nil {
		t.Fatal(err)
	}
	if code, _ := get("", "network=ESPN2"); code != 401 {
		t.Fatalf("no key sent: got %d, want 401", code)
	}
	if code, _ := get("wrong", "network=ESPN2"); code != 401 {
		t.Fatalf("wrong key: got %d, want 401", code)
	}
	code, links := get("s3cret", "network=ESPN2&network=cbs&network=FOX&network=ESPNU&network=ESPN2")
	if code != 200 {
		t.Fatalf("got %d", code)
	}
	got := map[string]watchLink{}
	for _, l := range links {
		got[l.Network] = l
	}
	if len(links) != 2 {
		t.Fatalf("links = %+v, want ESPN2 and CBS only (FOX isn't FOX News, ESPNU is off)", links)
	}
	if l := got["ESPN2"]; l.Channel.ID != espn2.ID || l.Path != "/live/"+strconv.FormatInt(espn2.ID, 10) || l.Channel.Number != 206 {
		t.Errorf("ESPN2 = %+v", l)
	}
	if l := got["cbs"]; l.Channel.ID != wdbj.ID {
		t.Errorf("CBS should use the mapped station, got %+v", l)
	}
}
