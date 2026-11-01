package webpush

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// browser plays the receiving end: it holds the subscription's private key
// and decrypts what the server sends, as RFC 8291 describes.
type browser struct {
	priv *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T, endpoint string) (*browser, Subscription) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return &browser{priv, auth}, Subscription{Endpoint: endpoint, P256dh: b64(priv.PublicKey().Bytes()), Auth: b64(auth)}
}

func (b *browser) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	if rs := binary.BigEndian.Uint32(body[16:20]); rs != recordSize || body[20] != 65 {
		t.Fatalf("header rs=%d idlen=%d", rs, body[20])
	}
	plain, err := Decrypt(body, b.priv, b.auth)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	return plain
}

// checkVAPID verifies the JWT the way a push service does.
func checkVAPID(t *testing.T, header, audience string, keys Keys) map[string]any {
	t.Helper()
	tok, k, ok := strings.Cut(strings.TrimPrefix(header, "vapid t="), ", k=")
	if !ok || k != keys.Public {
		t.Fatalf("authorization header %q", header)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token %q", tok)
	}
	pubBytes, _ := unb64(k)
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), pubBytes)
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := unb64(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("VAPID signature doesn't verify")
	}
	raw, _ := unb64(parts[1])
	var claims map[string]any
	json.Unmarshal(raw, &claims)
	if claims["aud"] != audience {
		t.Fatalf("aud %v, want %s", claims["aud"], audience)
	}
	if exp, _ := claims["exp"].(float64); int64(exp) <= time.Now().Unix() || int64(exp) > time.Now().Add(24*time.Hour).Unix() {
		t.Fatalf("exp %v", claims["exp"])
	}
	return claims
}

func TestSendRoundTrip(t *testing.T) {
	keys, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	var b *browser
	var sub Subscription
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") != "60" || r.Header.Get("Urgency") != "high" {
			t.Errorf("headers %v", r.Header)
		}
		claims := checkVAPID(t, r.Header.Get("Authorization"), "https://"+r.Host, keys)
		if claims["sub"] != "mailto:me@example.com" {
			t.Errorf("sub %v", claims["sub"])
		}
		body, _ := io.ReadAll(r.Body)
		got = b.decrypt(t, body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	b, sub = newBrowser(t, srv.URL+"/push/abc")
	msg := []byte(`{"title":"The Bear is here"}`)
	res, err := Send(context.Background(), srv.Client(), sub, msg, keys, Options{Subject: "mailto:me@example.com", TTL: time.Minute, Urgency: "high"})
	if err != nil || res.Status != http.StatusCreated || res.Gone {
		t.Fatalf("send: %v %+v", err, res)
	}
	if string(got) != string(msg) {
		t.Fatalf("browser got %q", got)
	}
}

func TestSendGone(t *testing.T) {
	keys, _ := GenerateKeys()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unsubscribed", http.StatusGone)
	}))
	defer srv.Close()
	_, sub := newBrowser(t, srv.URL+"/x")
	res, err := Send(context.Background(), srv.Client(), sub, []byte("hi"), keys, Options{Subject: "mailto:a@b.c"})
	if err == nil || !res.Gone {
		t.Fatalf("want gone, got %v %+v", err, res)
	}
}

func TestValidate(t *testing.T) {
	_, good := newBrowser(t, "https://fcm.googleapis.com/fcm/send/x")
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Subscription{
		{Endpoint: "http://fcm.googleapis.com/x", P256dh: good.P256dh, Auth: good.Auth},
		{Endpoint: good.Endpoint, P256dh: good.Auth, Auth: good.Auth},
		{Endpoint: good.Endpoint, P256dh: good.P256dh, Auth: good.P256dh},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	// Padded standard base64, as some browsers hand it over.
	padded := good
	raw, _ := unb64(good.Auth)
	padded.Auth = strings.NewReplacer("-", "+", "_", "/").Replace(b64(raw)) + "=="
	if err := padded.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPayloadLimit(t *testing.T) {
	keys, _ := GenerateKeys()
	_, sub := newBrowser(t, "https://example.com/x")
	if _, err := Send(context.Background(), http.DefaultClient, sub, make([]byte, MaxPayload+1), keys, Options{}); err == nil {
		t.Fatal("too-big payload sent")
	}
	body, err := Encrypt(sub, make([]byte, MaxPayload))
	if err != nil || len(body) > 4096 {
		t.Fatalf("max payload: %v, %d bytes", err, len(body))
	}
}
