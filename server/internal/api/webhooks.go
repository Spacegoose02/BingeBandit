package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"bingebandit/internal/store"
)

// WebhookEvents are the events a webhook can subscribe to.
var WebhookEvents = []string{"playback.start", "playback.stop", "playback.finished", "recording.started", "recording.completed", "recording.failed"}

var webhookClient = &http.Client{Timeout: 10 * time.Second}

// Emit sends an event to every enabled webhook subscribed to it, in the
// background. Payloads are signed with HMAC-SHA256 when a secret is set.
func (s *Server) Emit(event string, data any) {
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

func (s *Server) deliver(h *store.Webhook, event string, data any) string {
	body, _ := json.Marshal(map[string]any{
		"event": event, "timestamp": time.Now().UTC().Format(time.RFC3339), "server": s.Store.Setting(store.SettingServerName), "data": data,
	})
	req, err := http.NewRequest(http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
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
