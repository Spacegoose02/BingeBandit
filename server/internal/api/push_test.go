package api

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
	"bingebandit/internal/webpush"
)

// fakePushService is a push service that decrypts what it's sent the way
// each subscribed browser would.
type fakePushService struct {
	*httptest.Server
	mu       sync.Mutex
	browsers map[string]struct {
		priv *ecdh.PrivateKey
		auth []byte
	}
	got  map[string][]pushMessage // by path
	gone map[string]bool
}

func newFakePushService(t *testing.T) *fakePushService {
	f := &fakePushService{got: map[string][]pushMessage{}, gone: map[string]bool{}, browsers: map[string]struct {
		priv *ecdh.PrivateKey
		auth []byte
	}{}}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.gone[r.URL.Path] {
			w.WriteHeader(http.StatusGone)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") || r.Header.Get("TTL") == "" {
			t.Errorf("missing VAPID or TTL: %v", r.Header)
		}
		b := f.browsers[r.URL.Path]
		body, _ := io.ReadAll(r.Body)
		plain, err := webpush.Decrypt(body, b.priv, b.auth)
		if err != nil {
			t.Errorf("decrypt for %s: %v", r.URL.Path, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var m pushMessage
		json.Unmarshal(plain, &m)
		f.got[r.URL.Path] = append(f.got[r.URL.Path], m)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(f.Close)
	return f
}

// subscribe makes a browser subscription at path.
func (f *fakePushService) subscribe(path string) map[string]any {
	priv, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	rand.Read(auth)
	f.mu.Lock()
	f.browsers[path] = struct {
		priv *ecdh.PrivateKey
		auth []byte
	}{priv, auth}
	f.mu.Unlock()
	enc := base64.RawURLEncoding.EncodeToString
	return map[string]any{"endpoint": f.URL + path, "keys": map[string]string{"p256dh": enc(priv.PublicKey().Bytes()), "auth": enc(auth)}}
}

func (f *fakePushService) messages(path string) []pushMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pushMessage{}, f.got[path]...)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPushNotifications(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	sam, _ := st.CreateUser("sam", string(hash), false)
	kim, _ := st.CreateUser("kim", string(hash), false)
	st.CreateSession("sam", sam.ID, "Web", "")
	st.CreateSession("kim", kim.ID, "Web", "")

	fake := newFakePushService(t)
	oldAllowed, oldClient := pushHostAllowed, pushClient
	pushHostAllowed = func(h string) bool { return h == "127.0.0.1" }
	pushClient = fake.Client()
	defer func() { pushHostAllowed, pushClient = oldAllowed, oldClient }()

	srv := &Server{Store: st}
	h := srv.Router(context.Background())
	call := func(token, method, path string, body any) *httptest.ResponseRecorder {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
		req := httptest.NewRequest(method, path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// The public key is made once and kept.
	var status struct {
		PublicKey string              `json:"publicKey"`
		Devices   []*store.PushDevice `json:"devices"`
	}
	rec := call("sam", "GET", "/api/push", nil)
	json.Unmarshal(rec.Body.Bytes(), &status)
	if rec.Code != 200 || len(status.PublicKey) < 80 || len(status.Devices) != 0 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	first := status.PublicKey
	call("kim", "GET", "/api/push", nil)
	json.Unmarshal(call("sam", "GET", "/api/push", nil).Body.Bytes(), &status)
	if status.PublicKey != first {
		t.Fatal("VAPID key changed")
	}
	if all, _ := st.Settings(); all[settingVAPIDPrivate] != "" {
		t.Fatal("private key is listed in admin settings")
	}

	// Sam's phone and laptop; the laptop doesn't want sports.
	phone := fake.subscribe("/sam-phone")
	phone["device"] = "Chrome on Android"
	if rec := call("sam", "POST", "/api/push/devices", phone); rec.Code != 200 {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body)
	}
	laptop := fake.subscribe("/sam-laptop")
	laptop["kindsOff"] = []string{"sports", "bogus"}
	rec = call("sam", "POST", "/api/push/devices", laptop)
	var lap store.PushDevice
	json.Unmarshal(rec.Body.Bytes(), &lap)
	if rec.Code != 200 || strings.Join(lap.KindsOff, ",") != "sports" || lap.Device != "Browser" {
		t.Fatalf("laptop: %d %s", rec.Code, rec.Body)
	}
	kimSub := fake.subscribe("/kim")
	call("kim", "POST", "/api/push/devices", kimSub)

	// Refused: plain http, unknown push services, bad keys.
	bad := fake.subscribe("/x")
	bad["endpoint"] = "http://127.0.0.1/x"
	if rec := call("sam", "POST", "/api/push/devices", bad); rec.Code != 400 {
		t.Fatalf("http endpoint: %d", rec.Code)
	}
	pushHostAllowed = oldAllowed
	bad = fake.subscribe("/y")
	if rec := call("sam", "POST", "/api/push/devices", bad); rec.Code != 400 {
		t.Fatalf("unknown host: %d", rec.Code)
	}
	pushHostAllowed = func(h string) bool { return h == "127.0.0.1" }

	// A finished request reaches both devices, encrypted for each.
	n := &store.Notification{UserID: sam.ID, Kind: "request", Title: "The Bear is here", Link: "/item/7"}
	if err := srv.notify(n); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "request push", func() bool {
		return len(fake.messages("/sam-phone")) == 1 && len(fake.messages("/sam-laptop")) == 1
	})
	if m := fake.messages("/sam-phone")[0]; m.Title != "The Bear is here" || m.Link != "/item/7" || m.Kind != "request" || m.Tag == "" {
		t.Fatalf("phone got %+v", m)
	}
	if list, unread, _ := st.Notifications(sam.ID, 10); len(list) != 1 || unread != 1 {
		t.Fatal("the notification wasn't kept under the bell")
	}

	// A game alert skips the laptop.
	srv.notify(&store.Notification{UserID: sam.ID, Kind: "sports", Title: "Hokies vs Hurricanes starts in 15 minutes"})
	waitFor(t, "sports push", func() bool { return len(fake.messages("/sam-phone")) == 2 })
	time.Sleep(50 * time.Millisecond)
	if len(fake.messages("/sam-laptop")) != 1 {
		t.Fatal("laptop got a sports alert it turned off")
	}
	if len(fake.messages("/kim")) != 0 {
		t.Fatal("kim got sam's notification")
	}

	// Kim can't change or remove Sam's devices.
	if rec := call("kim", "PUT", "/api/push/devices/"+itoa(lap.ID), map[string]any{"kindsOff": []string{}}); rec.Code != 404 {
		t.Fatalf("kim edits sam's device: %d", rec.Code)
	}
	if rec := call("kim", "DELETE", "/api/push/devices/"+itoa(lap.ID), nil); rec.Code != 404 {
		t.Fatalf("kim deletes sam's device: %d", rec.Code)
	}
	if rec := call("sam", "PUT", "/api/push/devices/"+itoa(lap.ID), map[string]any{"kindsOff": []string{"together"}}); rec.Code != 204 {
		t.Fatalf("set kinds: %d", rec.Code)
	}

	// The test message goes everywhere and reports per device. A browser
	// that unsubscribed is forgotten.
	fake.mu.Lock()
	fake.gone["/sam-phone"] = true
	fake.mu.Unlock()
	rec = call("sam", "POST", "/api/push/test", nil)
	var res struct {
		Results map[string]string `json:"results"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Results[itoa(lap.ID)] != "sent" || len(res.Results) != 2 {
		t.Fatalf("test results %s", rec.Body)
	}
	json.Unmarshal(call("sam", "GET", "/api/push", nil).Body.Bytes(), &status)
	if len(status.Devices) != 1 || status.Devices[0].ID != lap.ID || status.Devices[0].LastUsed == 0 {
		t.Fatalf("devices after gone: %+v", status.Devices)
	}

	// The same browser signing in as Kim moves to Kim.
	call("kim", "POST", "/api/push/devices", laptop)
	json.Unmarshal(call("sam", "GET", "/api/push", nil).Body.Bytes(), &status)
	if len(status.Devices) != 0 {
		t.Fatal("the laptop still pushes to sam")
	}
	if rec := call("kim", "DELETE", "/api/push/devices/"+itoa(lap.ID), nil); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestPushPayloadFits(t *testing.T) {
	m := pushMessage{Kind: "request", Title: "x", Body: strings.Repeat("é", 5000), Image: "/img/" + strings.Repeat("a", 500)}
	b := pushPayload(m)
	if len(b) > webpush.MaxPayload {
		t.Fatalf("%d bytes", len(b))
	}
	var back pushMessage
	if err := json.Unmarshal(b, &back); err != nil || back.Title != "x" || back.Body == "" {
		t.Fatalf("%v %+v", err, back)
	}
}

func TestPushHosts(t *testing.T) {
	for host, want := range map[string]bool{
		"fcm.googleapis.com": true, "web.push.apple.com": true, "wns2-by3p.notify.windows.com": true,
		"updates.push.services.mozilla.com": true, "evilfcm.googleapis.com.example": false, "192.168.1.1": false, "localhost": false,
	} {
		if pushHostAllowed(host) != want {
			t.Errorf("%s: want %v", host, want)
		}
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
