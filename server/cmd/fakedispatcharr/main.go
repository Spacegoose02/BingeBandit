// Command fakedispatcharr pretends to be a Dispatcharr server set up on top of
// fakeiptv, for developing and testing Live TV → Import from Dispatcharr.
// It has one provider account (fakeiptv's playlist), fakeiptv's guide, a
// placeholder guide that can't be imported, and channels renumbered from 101
// in a "Favourites" group, with the flaky channel's backup as its fallback.
//
//	go run ./cmd/fakeiptv &
//	go run ./cmd/fakedispatcharr        # :9191, admin / admin, or API key fake-dispatcharr-key
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"

	"bingebandit/internal/livetv"
)

func main() {
	addr := flag.String("addr", ":9191", "listen address")
	iptv := flag.String("iptv", "http://localhost:8585", "fakeiptv address")
	flag.Parse()

	resp, err := http.Get(*iptv + "/playlist.m3u")
	if err != nil {
		log.Fatalf("start fakeiptv first: %v", err)
	}
	var entries []livetv.Entry
	err = livetv.ParseM3U(resp.Body, func(e livetv.Entry) error { entries = append(entries, e); return nil })
	resp.Body.Close()
	if err != nil {
		log.Fatal(err)
	}

	type stream struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		URL          string `json:"url"`
		M3UAccount   int    `json:"m3u_account"`
		ChannelGroup int    `json:"channel_group"`
	}
	groups := []map[string]any{{"id": 1, "name": "Favourites"}}
	groupID := map[string]int{}
	var streams []stream
	for i, e := range entries {
		if _, ok := groupID[e.Group]; !ok {
			groupID[e.Group] = len(groups) + 1
			groups = append(groups, map[string]any{"id": len(groups) + 1, "name": e.Group})
		}
		streams = append(streams, stream{ID: i + 1, Name: e.Name, URL: e.URL, M3UAccount: 1, ChannelGroup: groupID[e.Group]})
	}
	var logos []map[string]any
	var channels []map[string]any
	for i, e := range entries {
		if strings.Contains(e.Name, "(backup)") {
			continue
		}
		list := []stream{streams[i]}
		for j, b := range entries {
			if b.Name == e.Name+" (backup)" {
				list = append(list, streams[j])
			}
		}
		logos = append(logos, map[string]any{"id": i + 1, "name": e.Name, "url": e.Logo})
		channels = append(channels, map[string]any{
			"id": i + 1, "channel_number": 101 + len(channels), "name": e.Name, "channel_group_id": 1,
			"tvg_id": e.TvgID, "epg_data_id": nil, "logo_id": i + 1, "streams": list,
		})
	}
	var groupsOn []map[string]any
	for _, g := range groups[1:] {
		groupsOn = append(groupsOn, map[string]any{"channel_group": g["id"], "enabled": true})
	}
	routes := map[string]any{
		"/api/m3u/accounts/": []map[string]any{
			{"id": 1, "name": "Fake IPTV", "server_url": *iptv + "/playlist.m3u", "account_type": "STD", "max_streams": 2,
				"is_active": true, "user_agent": 1, "refresh_interval": 12, "channel_groups": groupsOn},
			{"id": 2, "name": "custom", "locked": true, "server_url": nil, "account_type": "STD", "is_active": true, "channel_groups": []any{}},
		},
		"/api/core/useragents/": []map[string]any{{"id": 1, "name": "VLC", "user_agent": "VLC/3.0.20 LibVLC/3.0.20"}},
		"/api/epg/sources/": []map[string]any{
			{"id": 1, "name": "Fake guide", "source_type": "xmltv", "url": *iptv + "/epg.xml", "is_active": true, "refresh_interval": 0},
			{"id": 2, "name": "Placeholder", "source_type": "dummy", "url": nil, "is_active": true},
		},
		"/api/channels/groups/":   groups,
		"/api/channels/logos/":    logos,
		"/api/channels/channels/": channels,
		"/api/epg/epgdata/":       []any{},
	}

	http.HandleFunc("/api/accounts/token/", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "admin" || body["password"] != "admin" {
			http.Error(w, `{"detail":"No active account found with the given credentials"}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"access": "fake-access", "refresh": "fake-refresh"})
	})
	for path, v := range routes {
		http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer fake-access" && r.Header.Get("X-API-Key") != "fake-dispatcharr-key" {
				http.Error(w, `{"detail":"Authentication credentials were not provided."}`, http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(v)
		})
	}
	fmt.Printf("fake Dispatcharr on %s (admin / admin), %d channels from %s\n", *addr, len(channels), *iptv)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
