// Package webpush sends Web Push messages (RFC 8030) to browsers, encrypted
// for the browser (RFC 8291, aes128gcm) and signed by the server (VAPID,
// RFC 8292), with nothing but the standard library and x/crypto.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

// Keys are the server's VAPID key pair, as browsers and push services see
// them: base64url without padding.
type Keys struct {
	Public  string // 65-byte uncompressed P-256 point, given to browsers as applicationServerKey
	Private string // 32-byte scalar
}

// GenerateKeys makes a new VAPID key pair.
func GenerateKeys() (Keys, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Keys{}, err
	}
	d, err := k.Bytes()
	if err != nil {
		return Keys{}, err
	}
	pub, err := k.PublicKey.Bytes()
	if err != nil {
		return Keys{}, err
	}
	return Keys{Public: b64(pub), Private: b64(d)}, nil
}

func (k Keys) signer() (*ecdsa.PrivateKey, error) {
	d, err := unb64(k.Private)
	if err != nil {
		return nil, err
	}
	return ecdsa.ParseRawPrivateKey(elliptic.P256(), d)
}

// Subscription is what a browser's PushManager.subscribe() returns.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"` // the browser's public key, base64url
	Auth     string `json:"auth"`   // 16-byte auth secret, base64url
}

// Validate checks the keys decode to the right sizes.
func (s Subscription) Validate() error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("the push address must be https")
	}
	pub, err := unb64(s.P256dh)
	if err != nil || len(pub) != 65 || pub[0] != 4 {
		return errors.New("the browser's push key is not a P-256 key")
	}
	if a, err := unb64(s.Auth); err != nil || len(a) != 16 {
		return errors.New("the browser's push secret must be 16 bytes")
	}
	return nil
}

// Options for one message.
type Options struct {
	Subject string        // mailto: or https: contact for the push service
	TTL     time.Duration // how long the push service keeps it for an offline browser
	Urgency string        // very-low | low | normal | high; empty = normal
	Topic   string        // a newer message with the same topic replaces an undelivered one
}

// Result is the push service's answer.
type Result struct {
	Status int
	// Gone means the browser unsubscribed or the subscription expired, so
	// it should be forgotten.
	Gone bool
}

// MaxPayload is the most plaintext a push service is guaranteed to take
// (4096 bytes of record, less the header, tag and delimiter).
const MaxPayload = 3993

// Send encrypts payload for sub and posts it to the push service.
func Send(ctx context.Context, client *http.Client, sub Subscription, payload []byte, keys Keys, o Options) (Result, error) {
	if len(payload) > MaxPayload {
		return Result{}, fmt.Errorf("push message is %d bytes, more than %d", len(payload), MaxPayload)
	}
	body, err := Encrypt(sub, payload)
	if err != nil {
		return Result{}, err
	}
	auth, err := vapidHeader(sub.Endpoint, keys, o.Subject, time.Now())
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	ttl := o.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", strconv.Itoa(int(ttl.Seconds())))
	req.Header.Set("Authorization", auth)
	if o.Urgency != "" {
		req.Header.Set("Urgency", o.Urgency)
	}
	if o.Topic != "" {
		req.Header.Set("Topic", o.Topic)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	res := Result{Status: resp.StatusCode, Gone: resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return res, fmt.Errorf("push service answered %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return res, nil
}

const recordSize = 4096

// Encrypt makes the aes128gcm body for sub (RFC 8291 section 3.4): a header
// with a random salt and a fresh sender key, then one record.
func Encrypt(sub Subscription, payload []byte) ([]byte, error) {
	if err := sub.Validate(); err != nil {
		return nil, err
	}
	uaPub, _ := unb64(sub.P256dh)
	secret, _ := unb64(sub.Auth)
	ua, err := ecdh.P256().NewPublicKey(uaPub)
	if err != nil {
		return nil, err
	}
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	cek, nonce, err := deriveKeys(as, ua, uaPub, as.PublicKey().Bytes(), secret, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	asPub := as.PublicKey().Bytes()
	header := make([]byte, 0, 16+4+1+len(asPub))
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, recordSize)
	header = append(header, byte(len(asPub)))
	header = append(header, asPub...)
	plain := append(append([]byte{}, payload...), 2) // 2: the last (only) record
	return gcm.Seal(header, nonce, plain, nil), nil
}

// Decrypt is the browser's side of Encrypt, for tests and fakes: priv is
// the subscription's private key and auth its secret.
func Decrypt(body []byte, priv *ecdh.PrivateKey, auth []byte) ([]byte, error) {
	if len(body) < 21 {
		return nil, errors.New("short body")
	}
	salt, idlen := body[:16], int(body[20])
	if len(body) < 21+idlen {
		return nil, errors.New("short header")
	}
	asPub := body[21 : 21+idlen]
	as, err := ecdh.P256().NewPublicKey(asPub)
	if err != nil {
		return nil, err
	}
	cek, nonce, err := deriveKeys(priv, as, priv.PublicKey().Bytes(), asPub, auth, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		return nil, err
	}
	if len(plain) == 0 || plain[len(plain)-1] != 2 {
		return nil, errors.New("not a single last record")
	}
	return plain[:len(plain)-1], nil
}

// deriveKeys is the key schedule of RFC 8291 section 3.4, the same on
// both ends: priv is either side's private key and peer the other's public.
func deriveKeys(priv *ecdh.PrivateKey, peer *ecdh.PublicKey, uaPub, asPub, authSecret, salt []byte) (cek, nonce []byte, err error) {
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, nil, err
	}
	info := append(append([]byte("WebPush: info\x00"), uaPub...), asPub...)
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, authSecret, info), ikm); err != nil {
		return nil, nil, err
	}
	cek = make([]byte, 16)
	nonce = make([]byte, 12)
	if _, err := io.ReadFull(hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: aes128gcm\x00")), cek); err != nil {
		return nil, nil, err
	}
	if _, err := io.ReadFull(hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: nonce\x00")), nonce); err != nil {
		return nil, nil, err
	}
	return cek, nonce, nil
}

// vapidHeader is the Authorization header of RFC 8292: a JWT for the push
// service's origin, signed with ES256, and the public key.
func vapidHeader(endpoint string, keys Keys, subject string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	priv, err := keys.signer()
	if err != nil {
		return "", fmt.Errorf("VAPID key: %w", err)
	}
	head := b64([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": now.Add(12 * time.Hour).Unix(),
		"sub": subject,
	})
	signing := head + "." + b64(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, priv, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + signing + "." + b64(sig) + ", k=" + keys.Public, nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// unb64 takes base64url with or without padding (browsers differ), and
// standard base64 too.
func unb64(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	s = strings.NewReplacer("+", "-", "/", "_").Replace(s)
	return base64.RawURLEncoding.DecodeString(s)
}
