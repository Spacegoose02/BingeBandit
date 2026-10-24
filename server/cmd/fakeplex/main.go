// Command fakeplex pretends to be a Plex Media Server and a Tautulli instance
// with a small, fixed watch history, for developing and testing the importer.
// It matches the generated test media (Big Test Movie, Subtitle Test, Marker
// Show, Test Show) and adds a few things that won't match.
//
//	go run ./cmd/fakeplex                         # Plex on :32400, Tautulli on :8181
//	go run ./cmd/fakeplex -makedb tautulli.db     # write a Tautulli database file and exit
//
// Plex token: fake-plex-token. Tautulli API key: fake-tautulli-key.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

const (
	plexToken = "fake-plex-token"
	apiKey    = "fake-tautulli-key"
)

type item struct {
	rk, kind, title string
	year            int
	tmdb            int
	file            string
	show            string // grandparent rating key for episodes
	season, episode int
	durationMS      int64
	deleted         bool // only in history: Plex no longer has it
}

var items = map[string]*item{
	"101": {rk: "101", kind: "movie", title: "Big Test Movie", year: 2021, tmdb: 9001, file: "/data/movies/Big Test Movie (2021)/Big.Test.Movie.2021.720p.mp4", durationMS: 30000},
	"102": {rk: "102", kind: "movie", title: "Subtitle Test", year: 2022, durationMS: 20000},
	"103": {rk: "103", kind: "movie", title: "Not In Your Library", year: 1999, tmdb: 1234, durationMS: 5400000},
	"104": {rk: "104", kind: "movie", title: "Amélie", year: 2001, durationMS: 7200000},
	"201": {rk: "201", kind: "show", title: "Marker Show", year: 2020, tmdb: 5000},
	"211": {rk: "211", kind: "episode", title: "Part 1", show: "201", season: 1, episode: 1, durationMS: 60000},
	"212": {rk: "212", kind: "episode", title: "Part 2", show: "201", season: 1, episode: 2, durationMS: 60000},
	"213": {rk: "213", kind: "episode", title: "Part 3", show: "201", season: 1, episode: 3, durationMS: 60000},
	"301": {rk: "301", kind: "show", title: "Test Show (US)", year: 2019},
	"311": {rk: "311", kind: "episode", title: "Episode Title 1", show: "301", season: 1, episode: 1, durationMS: 30000, file: "/data/tv/Test Show/Season 01/Test Show - S01E01 - Episode Title 1.mkv"},
	"312": {rk: "312", kind: "episode", title: "Episode Title 2", show: "301", season: 1, episode: 2, durationMS: 30000},
	"999": {rk: "999", kind: "movie", title: "Subtitle Test", year: 2022, durationMS: 20000, deleted: true},
}

var accounts = []struct {
	id         int
	name, user string
}{{1, "plexowner", "plexowner"}, {2, "Grandma", "grandma"}, {3, "kiddo", "kiddo"}}

type play struct {
	id      int
	account int
	rk      string
	daysAgo int
	percent int
}

var history = []play{
	{1, 1, "101", 30, 100}, {2, 1, "211", 20, 100}, {3, 1, "212", 19, 100}, {4, 1, "103", 18, 100},
	{5, 2, "102", 10, 100}, {6, 2, "311", 9, 100}, {7, 2, "312", 8, 100}, {8, 2, "104", 7, 100},
	{9, 3, "213", 5, 100}, {10, 3, "999", 4, 100}, {11, 1, "101", 2, 100},
}

func when(daysAgo int) int64 {
	return time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC).AddDate(0, 0, -daysAgo).Unix()
}

func guids(it *item) []map[string]string {
	if it.tmdb == 0 {
		return []map[string]string{}
	}
	return []map[string]string{{"id": "tmdb://" + strconv.Itoa(it.tmdb)}, {"id": "imdb://tt" + strconv.Itoa(1000000+it.tmdb)}}
}

func meta(it *item) map[string]any {
	m := map[string]any{"ratingKey": it.rk, "key": "/library/metadata/" + it.rk, "type": it.kind, "title": it.title, "year": it.year,
		"duration": it.durationMS, "Guid": guids(it), "summary": ""}
	if it.file != "" {
		m["Media"] = []any{map[string]any{"Part": []any{map[string]any{"file": it.file}}}}
	}
	if it.kind == "episode" {
		show := items[it.show]
		m["grandparentTitle"], m["grandparentRatingKey"], m["grandparentKey"] = show.title, show.rk, "/library/metadata/"+show.rk
		m["parentIndex"], m["index"] = it.season, it.episode
		delete(m, "year")
	}
	return m
}

func container(extra map[string]any) map[string]any {
	return map[string]any{"MediaContainer": extra}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func plex() http.Handler {
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Plex-Token") != plexToken && r.URL.Query().Get("X-Plex-Token") != plexToken {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			log.Printf("plex: %s", r.URL.RequestURI())
			h(w, r)
		}
	}
	mux.HandleFunc("/accounts", auth(func(w http.ResponseWriter, r *http.Request) {
		acc := []any{map[string]any{"id": 0, "name": ""}}
		for _, a := range accounts {
			acc = append(acc, map[string]any{"id": a.id, "name": a.name})
		}
		writeJSON(w, container(map[string]any{"size": len(acc), "Account": acc}))
	}))
	mux.HandleFunc("/library/sections", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, container(map[string]any{"Directory": []any{
			map[string]any{"key": "1", "title": "Movies", "type": "movie"},
			map[string]any{"key": "2", "title": "TV Shows", "type": "show"},
			map[string]any{"key": "3", "title": "Music", "type": "artist"},
		}}))
	}))
	mux.HandleFunc("/library/metadata/{rk}", auth(func(w http.ResponseWriter, r *http.Request) {
		it := items[r.PathValue("rk")]
		if it == nil || it.deleted {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, container(map[string]any{"size": 1, "Metadata": []any{meta(it)}}))
	}))
	// The owner's watched state: Big Test Movie watched, Subtitle Test half way, Marker Show S1E1 watched.
	state := map[string][2]int64{"101": {1, 0}, "102": {0, 9000}, "211": {1, 0}}
	mux.HandleFunc("/library/sections/{key}/all", auth(func(w http.ResponseWriter, r *http.Request) {
		want := map[string]string{"1": "movie", "4": "episode", "2": "show"}[r.URL.Query().Get("type")]
		var out []any
		for _, rk := range []string{"101", "102", "103", "104", "201", "211", "212", "213", "301", "311", "312"} {
			it := items[rk]
			if it.kind != want {
				continue
			}
			m := meta(it)
			if s, ok := state[rk]; ok {
				m["viewCount"], m["viewOffset"], m["lastViewedAt"] = s[0], s[1], when(1)
			}
			out = append(out, m)
		}
		writeJSON(w, container(map[string]any{"size": len(out), "Metadata": out}))
	}))
	mux.HandleFunc("/status/sessions/history/all", auth(func(w http.ResponseWriter, r *http.Request) {
		start, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Start"))
		size, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Size"))
		if size == 0 {
			size = 50
		}
		var out []any
		for i := start; i < len(history) && i < start+size; i++ {
			h := history[i]
			it := items[h.rk]
			m := meta(it)
			delete(m, "Guid")
			delete(m, "Media")
			m["historyKey"] = fmt.Sprintf("/status/sessions/history/%d", h.id)
			m["viewedAt"], m["accountID"] = when(h.daysAgo), h.account
			out = append(out, m)
		}
		writeJSON(w, container(map[string]any{"size": len(out), "totalSize": len(history), "Metadata": out}))
	}))
	mux.HandleFunc("/library/sections/{key}/collections", auth(func(w http.ResponseWriter, r *http.Request) {
		var cols []any
		switch r.PathValue("key") {
		case "1":
			cols = []any{map[string]any{"ratingKey": "501", "title": "Test Favorites", "summary": "Imported from Plex"}}
		case "2":
			cols = []any{map[string]any{"ratingKey": "502", "title": "Shows I Love"}}
		}
		writeJSON(w, container(map[string]any{"size": len(cols), "Metadata": cols}))
	}))
	mux.HandleFunc("/library/collections/{rk}/children", auth(func(w http.ResponseWriter, r *http.Request) {
		members := map[string][]string{"501": {"101", "102", "103"}, "502": {"201", "301"}}[r.PathValue("rk")]
		var out []any
		for _, rk := range members {
			out = append(out, meta(items[rk]))
		}
		writeJSON(w, container(map[string]any{"size": len(out), "Metadata": out}))
	}))
	return mux
}

// tautulliRows is the history as Tautulli records it: the Plex plays plus a
// partly-watched one and a music track (which must be skipped).
func tautulliRows() []map[string]any {
	var out []map[string]any
	add := func(id int, account int, rk string, daysAgo, percent int, kind string) {
		it := items[rk]
		stopped := when(daysAgo)
		dur := it.durationMS / 1000
		row := map[string]any{
			"id": id, "row_id": id, "date": stopped - dur, "started": stopped - dur, "stopped": stopped,
			"play_duration": dur * int64(percent) / 100, "duration": dur * int64(percent) / 100, "paused_counter": 0,
			"user_id": 100 + account, "user": accounts[account-1].name, "media_type": kind, "title": it.title, "year": it.year,
			"percent_complete": percent, "watched_status": map[bool]float64{true: 1, false: 0.5}[percent >= 85], "player": "Living Room TV",
			"grandparent_title": "", "media_index": "", "parent_media_index": "",
		}
		if it.kind == "episode" {
			show := items[it.show]
			row["grandparent_title"], row["media_index"], row["parent_media_index"] = show.title, strconv.Itoa(it.episode), strconv.Itoa(it.season)
			row["year"] = show.year
		}
		out = append(out, row)
	}
	for _, h := range history {
		add(h.id, h.account, h.rk, h.daysAgo, h.percent, items[h.rk].kind)
	}
	add(12, 2, "213", 3, 40, "episode")
	out = append(out, map[string]any{"id": 13, "media_type": "track", "title": "A Song", "user_id": 101, "stopped": when(1)})
	return out
}

func tautulli() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		log.Printf("tautulli: %s", q.Get("cmd"))
		resp := func(result string, data any, msg string) {
			writeJSON(w, map[string]any{"response": map[string]any{"result": result, "message": msg, "data": data}})
		}
		if r.URL.Path != "/api/v2" {
			http.NotFound(w, r)
			return
		}
		if q.Get("apikey") != apiKey {
			resp("error", map[string]any{}, "Invalid apikey")
			return
		}
		switch q.Get("cmd") {
		case "get_users":
			var us []any
			us = append(us, map[string]any{"user_id": 0, "username": "Local", "friendly_name": "Local"})
			for _, a := range accounts {
				us = append(us, map[string]any{"user_id": 100 + a.id, "username": a.name, "friendly_name": a.name})
			}
			resp("success", us, "")
		case "get_history":
			rows := tautulliRows()
			start, _ := strconv.Atoi(q.Get("start"))
			length, _ := strconv.Atoi(q.Get("length"))
			end := min(start+length, len(rows))
			if start > len(rows) {
				start = len(rows)
			}
			resp("success", map[string]any{"recordsFiltered": len(rows), "recordsTotal": len(rows), "data": rows[start:end]}, "")
		default:
			resp("error", map[string]any{}, "Unknown command")
		}
	})
}

func makeDB(path string) error {
	os.Remove(path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, user_id INTEGER UNIQUE, username TEXT, friendly_name TEXT)`,
		`CREATE TABLE session_history (id INTEGER PRIMARY KEY, reference_id INTEGER, started INTEGER, stopped INTEGER, rating_key INTEGER,
			user_id INTEGER, user TEXT, ip_address TEXT, paused_counter INTEGER DEFAULT 0, player TEXT, product TEXT, platform TEXT,
			media_type TEXT, view_offset INTEGER DEFAULT 0)`,
		`CREATE TABLE session_history_metadata (id INTEGER PRIMARY KEY, rating_key INTEGER, title TEXT, grandparent_title TEXT,
			full_title TEXT, media_index INTEGER, parent_media_index INTEGER, year INTEGER, duration INTEGER, guid TEXT)`,
	} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	for _, a := range accounts {
		db.Exec(`INSERT INTO users (user_id, username, friendly_name) VALUES (?, ?, ?)`, 100+a.id, a.name, a.name)
	}
	for _, row := range tautulliRows() {
		if row["media_type"] == "track" {
			db.Exec(`INSERT INTO session_history (id, started, stopped, user_id, user, media_type) VALUES (?, ?, ?, 101, 'plexowner', 'track')`, row["id"], row["stopped"], row["stopped"])
			continue
		}
		pct := row["percent_complete"].(int)
		var durMS int64
		var title string
		var ep, season any
		var year int
		title, year = row["title"].(string), row["year"].(int)
		for _, it := range items {
			if it.title == title {
				durMS = it.durationMS
				if it.kind == "episode" {
					ep, season = it.episode, it.season
				}
			}
		}
		if _, err := db.Exec(`INSERT INTO session_history (id, started, stopped, user_id, user, media_type, view_offset, player) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			row["id"], row["started"], row["stopped"], row["user_id"], row["user"], row["media_type"], durMS*int64(pct)/100, row["player"]); err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO session_history_metadata (id, title, grandparent_title, media_index, parent_media_index, year, duration) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			row["id"], title, row["grandparent_title"], ep, season, year, durMS); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	plexAddr := flag.String("plex", ":32400", "Plex listen address")
	tautulliAddr := flag.String("tautulli", ":8181", "Tautulli listen address")
	dbPath := flag.String("makedb", "", "write a Tautulli database to this path and exit")
	flag.Parse()
	if *dbPath != "" {
		if err := makeDB(*dbPath); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", *dbPath)
		return
	}
	go func() { log.Fatal(http.ListenAndServe(*tautulliAddr, tautulli())) }()
	log.Printf("fake Plex on %s (token %s), fake Tautulli on %s (API key %s)", *plexAddr, plexToken, *tautulliAddr, apiKey)
	log.Fatal(http.ListenAndServe(*plexAddr, plex()))
}
