package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"bingebandit/internal/store"
)

// WebhookEvents are the events a webhook can subscribe to.
// player.progress (every ping) is left out: it's for plugins.
var WebhookEvents = []string{"playback.start", "playback.stop", "playback.finished",
	"player.play", "player.pause", "player.resume", "player.stop",
	"recording.started", "recording.completed", "recording.failed",
	"sports.game.start", "sports.score", "sports.game.end"}

var webhookClient = &http.Client{Timeout: 10 * time.Second}

// Emit sends an event to every enabled webhook and plugin subscribed to it, in the
// background. Payloads are signed with HMAC-SHA256 when a secret is set.
func (s *Server) Emit(event string, data any) {
	s.notifyEvent(event, data)
	if s.Plugins != nil {
		s.Plugins.Emit(event, data)
	}
	hooks, err := s.Store.Webhooks()
	if err != nil {
		return
	}
	for _, h := range hooks {
		if !h.Enabled || !contains(h.Events, event) {
			continue
		}
		go s.deliver(h, event, data)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// plexEvents maps our playback events to the Plex webhook events receivers expect.
var plexEvents = map[string]string{"playback.start": "media.play", "playback.stop": "media.stop", "playback.finished": "media.scrobble", "test": "media.play"}

// plexBody builds a Plex-style webhook: multipart form with a "payload" JSON field.
func (s *Server) plexBody(event string, data any) ([]byte, string) {
	ev, _ := data.(map[string]any)
	get := func(k string) any { return ev[k] }
	str := func(k string) string { v, _ := ev[k].(string); return v }
	md := map[string]any{"title": str("title"), "year": get("year"), "type": get("kind"), "Guid": []any{}}
	if event == "test" {
		md = map[string]any{"title": "BingeBandit test", "type": "movie", "Guid": []any{}}
	}
	guid := func(id any) []any {
		if n, ok := id.(int64); ok && n > 0 {
			return []any{map[string]string{"id": fmt.Sprintf("tmdb://%d", n)}}
		}
		return []any{}
	}
	switch str("kind") {
	case "movie":
		md["librarySectionType"], md["Guid"] = "movie", guid(get("tmdbId"))
	case "episode":
		// Plex sends the show as grandparent; we only know the show's TMDB id.
		md["librarySectionType"], md["title"] = "show", str("episodeTitle")
		md["grandparentTitle"], md["parentIndex"], md["index"] = str("title"), get("season"), get("episode")
		md["grandparentGuid"] = guid(get("showTmdbId"))
	}
	payload, _ := json.Marshal(map[string]any{
		"event": plexEvents[event], "user": true, "owner": true,
		"Account":  map[string]any{"title": str("user")},
		"Server":   map[string]any{"title": s.Store.Setting(store.SettingServerName)},
		"Player":   map[string]any{"title": str("client"), "local": get("local")},
		"Metadata": md,
	})
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("payload", string(payload))
	mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}

func (s *Server) deliver(h *store.Webhook, event string, data any) string {
	var body []byte
	contentType := "application/json"
	if h.Format == "plex" {
		if plexEvents[event] == "" {
			return "skipped: Plex-style webhooks only carry playback events"
		}
		body, contentType = s.plexBody(event, data)
	} else {
		body, _ = json.Marshal(map[string]any{
			"event": event, "timestamp": time.Now().UTC().Format(time.RFC3339), "server": s.Store.Setting(store.SettingServerName), "data": data,
		})
	}
	req, err := http.NewRequest(http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return err.Error()
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "BingeBandit-Webhook")
	req.Header.Set("X-BingeBandit-Event", event)
	if h.Secret != "" {
		mac := hmac.New(sha256.New, []byte(h.Secret))
		mac.Write(body)
		req.Header.Set("X-BingeBandit-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	status := ""
	resp, err := webhookClient.Do(req)
	if err != nil {
		status = "error: " + err.Error()
	} else {
		resp.Body.Close()
		status = resp.Status
	}
	s.Store.SetWebhookResult(h.ID, status)
	if err != nil || resp.StatusCode >= 300 {
		log.Printf("webhook %q (%s): %s", h.Name, event, status)
	}
	return status
}

func (s *Server) webhooks(w http.ResponseWriter, r *http.Request) {
	hooks, err := s.Store.Webhooks()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": hooks, "events": WebhookEvents})
}

func (s *Server) saveWebhook(w http.ResponseWriter, r *http.Request) {
	var h store.Webhook
	if err := readJSON(r, &h); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if id, ok := idParam(r, "id"); ok {
		h.ID = id
	}
	h.Name, h.URL = strings.TrimSpace(h.Name), strings.TrimSpace(h.URL)
	if h.Name == "" || !(strings.HasPrefix(h.URL, "http://") || strings.HasPrefix(h.URL, "https://")) {
		writeErr(w, http.StatusBadRequest, "a webhook needs a name and an http(s) URL")
		return
	}
	if h.Format != "" && h.Format != "plex" {
		writeErr(w, http.StatusBadRequest, "format must be empty or plex")
		return
	}
	for _, e := range h.Events {
		if !contains(WebhookEvents, e) {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown event %q", e))
			return
		}
	}
	if err := s.Store.SaveWebhook(&h); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeleteWebhook(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testWebhook sends a sample event right away and reports the response.
func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	hooks, err := s.Store.Webhooks()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	for _, h := range hooks {
		if h.ID == id {
			status := s.deliver(h, "test", map[string]string{"message": "Hello from BingeBandit"})
			writeJSON(w, http.StatusOK, map[string]string{"status": status})
			return
		}
	}
	writeErr(w, http.StatusNotFound, "not found")
}
