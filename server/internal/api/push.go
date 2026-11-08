package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/store"
	"bingebandit/internal/webpush"
)

// Push notifications: each notification also goes to the browsers (and
// web apps installed on phones) a person turned push on in, so it arrives
// with BingeBandit closed. Browsers only offer push to a page served over
// HTTPS (or localhost), so this works through the public address.

// The VAPID key pair is made on first use and kept in internal settings
// (not in SettingDefaults, so admin settings never show the private key).
const (
	settingVAPIDPublic  = "push_vapid_public"
	settingVAPIDPrivate = "push_vapid_private"
)

const maxPushDevices = 30

var vapidMu sync.Mutex

func (s *Server) vapidKeys() (webpush.Keys, error) {
	vapidMu.Lock()
	defer vapidMu.Unlock()
	k := webpush.Keys{Public: s.Store.Setting(settingVAPIDPublic), Private: s.Store.Setting(settingVAPIDPrivate)}
	if k.Public != "" && k.Private != "" {
		return k, nil
	}
	k, err := webpush.GenerateKeys()
	if err != nil {
		return k, err
	}
	return k, s.Store.SetSettings(map[string]string{settingVAPIDPublic: k.Public, settingVAPIDPrivate: k.Private})
}

// pushHosts are the push services browsers use. The server only posts to
// these, so a subscription can't point it at something on the home network.
var pushHosts = []string{
	"fcm.googleapis.com",                // Chrome, Edge on Android, Brave, Opera, Samsung Internet
	"updates.push.services.mozilla.com", // Firefox
	"push.services.mozilla.com",
	"push.apple.com",     // Safari, iPhone and iPad home-screen apps (web.push.apple.com)
	"notify.windows.com", // Edge on Windows (*.notify.windows.com)
	"push.api.chrome.com",
}

var pushHostAllowed = func(host string) bool {
	host = strings.ToLower(host)
	for _, h := range pushHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

var pushClient = &http.Client{Timeout: 15 * time.Second}

// pushMessage is what the service worker (web/public/sw.js) shows.
type pushMessage struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Link  string `json:"link,omitempty"`
	Image string `json:"image,omitempty"`
	// Tag makes a newer message replace an older one on screen.
	Tag string `json:"tag,omitempty"`
}

// notify adds n to the user's notifications and pushes it to their devices.
func (s *Server) notify(n *store.Notification) error {
	if err := s.Store.AddNotification(n); err != nil {
		return err
	}
	msg := pushMessage{Kind: n.Kind, Title: n.Title, Body: n.Body, Link: n.Link, Image: n.Image}
	if n.ID != 0 {
		msg.Tag = "n" + strconv.FormatInt(n.ID, 10)
	}
	go s.push(n.UserID, msg)
	return nil
}

// push sends msg to every device of the user that hasn't turned its kind
// off, and returns what each push service said ("sent" or the error).
func (s *Server) push(userID int64, msg pushMessage) map[int64]string {
	devices, err := s.Store.PushDevices(userID)
	if err != nil || len(devices) == 0 {
		return nil
	}
	keys, err := s.vapidKeys()
	if err != nil {
		slog.Warn("push keys", "err", err)
		return nil
	}
	payload := pushPayload(msg)
	opts := webpush.Options{Subject: s.pushSubject(), TTL: pushTTL(msg.Kind), Urgency: "normal"}
	if msg.Kind == "sports" || msg.Kind == "together" || msg.Kind == "live" {
		opts.Urgency = "high"
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	results := map[int64]string{}
	for _, d := range devices {
		if msg.Kind != "test" && containsStr(d.KindsOff, msg.Kind) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := "sent"
			if err := s.pushTo(d, payload, keys, opts); err != nil {
				res = err.Error()
			}
			mu.Lock()
			results[d.ID] = res
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

func (s *Server) pushTo(d *store.PushDevice, payload []byte, keys webpush.Keys, opts webpush.Options) error {
	u, err := url.Parse(d.Endpoint)
	if err != nil || !pushHostAllowed(u.Hostname()) {
		return errors.New("not a known push service")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := webpush.Send(ctx, pushClient, webpush.Subscription{Endpoint: d.Endpoint, P256dh: d.P256dh, Auth: d.Auth}, payload, keys, opts)
	if res.Gone {
		// The browser unsubscribed, or its subscription ran out.
		s.Store.DropPushEndpoint(d.Endpoint)
		return errors.New("this browser stopped accepting notifications, so it was removed")
	}
	if err != nil {
		slog.Warn("push", "device", d.Device, "err", err)
		return err
	}
	s.Store.TouchPushDevice(d.ID)
	return nil
}

// pushTTL: an alert about a game or show starting is useless an hour on;
// news of an arrival can wait a day for a phone that's off.
func pushTTL(kind string) time.Duration {
	switch kind {
	case "sports", "live", "together", "test":
		return 30 * time.Minute
	}
	return 24 * time.Hour
}

// pushPayload encodes msg, shortening the body to fit a push message.
func pushPayload(msg pushMessage) []byte {
	for {
		b, _ := json.Marshal(msg)
		if len(b) <= webpush.MaxPayload || (msg.Body == "" && msg.Image == "") {
			return b
		}
		switch {
		case msg.Image != "" && len(msg.Image) > 300:
			msg.Image = ""
		case msg.Body != "":
			msg.Body = truncateRunes(msg.Body, utf8.RuneCountInString(msg.Body)/2)
		default:
			msg.Image = ""
		}
	}
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// pushSubject is the contact push services see: the server's public
// address when it's HTTPS, otherwise the project's page.
func (s *Server) pushSubject() string {
	if u, err := url.Parse(s.policy().publicURL); err == nil && u.Scheme == "https" && u.Host != "" {
		return "https://" + u.Host
	}
	return "https://github.com/Spacegoose02/BingeBandit"
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// GET /api/push: the key browsers subscribe with and this user's devices.
func (s *Server) pushStatus(w http.ResponseWriter, r *http.Request) {
	keys, err := s.vapidKeys()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "couldn't make push keys")
		return
	}
	devices, err := s.Store.PushDevices(currentUser(r).ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicKey": keys.Public, "devices": devices, "kinds": store.PushKinds})
}

// POST /api/push/devices: turn push on for this browser.
func (s *Server) addPushDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
		Device   string   `json:"device"`
		KindsOff []string `json:"kindsOff"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	sub := webpush.Subscription{Endpoint: strings.TrimSpace(req.Endpoint), P256dh: req.Keys.P256dh, Auth: req.Keys.Auth}
	if err := sub.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(sub.Endpoint) > 2000 {
		writeErr(w, http.StatusBadRequest, "the push address is too long")
		return
	}
	u, _ := url.Parse(sub.Endpoint)
	if !pushHostAllowed(u.Hostname()) {
		writeErr(w, http.StatusBadRequest, "this browser's push service isn't one BingeBandit knows")
		return
	}
	me := currentUser(r).ID
	existing, err := s.Store.PushDevices(me)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	known := false
	for _, d := range existing {
		known = known || d.Endpoint == sub.Endpoint
	}
	if !known && len(existing) >= maxPushDevices {
		writeErr(w, http.StatusBadRequest, "turn push off on a device you no longer use first")
		return
	}
	d := &store.PushDevice{UserID: me, Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth,
		Device: truncateRunes(strings.TrimSpace(req.Device), 80), KindsOff: req.KindsOff}
	if d.Device == "" {
		d.Device = "Browser"
	}
	if err := s.Store.SavePushDevice(d); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) setPushDevice(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req struct {
		KindsOff []string `json:"kindsOff"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetPushKindsOff(currentUser(r).ID, id, req.KindsOff); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deletePushDevice(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := s.Store.DeletePushDevice(currentUser(r).ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/push/test sends a test to each of the user's devices and says
// how each went.
func (s *Server) testPush(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r).ID
	results := s.push(me, pushMessage{Kind: "test", Title: "Push notifications work", Body: "BingeBandit will tell you here when something's ready.", Link: "/account", Tag: "test"})
	out := map[string]string{}
	for id, res := range results {
		out[strconv.FormatInt(id, 10)] = res
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// pushTogetherInvite tells each invited person's devices; the web app's
// own invitation banner still covers anyone with BingeBandit open.
func (s *Server) pushTogetherInvite(from string, room string, itemID int64, userIDs []int64) {
	for _, uid := range userIDs {
		it, err := s.Store.Item(uid, itemID)
		if err != nil {
			continue
		}
		title := it.Title
		if it.Kind == "episode" && it.ShowTitle != "" {
			title = it.ShowTitle + " · " + it.Title
		}
		s.push(uid, pushMessage{Kind: "together", Title: from + " invited you to watch together", Body: title,
			Link: "/play/" + strconv.FormatInt(itemID, 10) + "?together=" + url.QueryEscape(room), Image: it.Poster, Tag: "together-" + room})
	}
}
