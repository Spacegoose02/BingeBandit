// Command fakeoidc is a tiny OpenID Connect provider for developing and
// testing BingeBandit's single sign-on. It signs in whoever you click on —
// never expose it to a network.
//
//	go run ./cmd/fakeoidc -addr :8686 -client bingebandit -secret dev-secret
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type person struct{ sub, username, name, email string }

var people = []person{
	{"u-1001", "alice", "Alice Example", "alice@example.com"},
	{"u-1002", "bob", "Bob Example", "bob@example.com"},
	{"u-1003", "carol", "Carol Example", "carol@example.com"},
}

type grant struct {
	who                 person
	clientID, nonce     string
	challenge, redirect string
	expires             time.Time
}

func main() {
	addr := flag.String("addr", ":8686", "listen address")
	issuer := flag.String("issuer", "http://localhost:8686", "issuer URL (must match how BingeBandit reaches this server)")
	clientID := flag.String("client", "bingebandit", "client ID")
	secret := flag.String("secret", "dev-secret", "client secret")
	flag.Parse()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}
	jwk := jose.JSONWebKey{Key: key, KeyID: "fake-1", Algorithm: string(jose.RS256), Use: "sig"}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jwk}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		log.Fatal(err)
	}

	var mu sync.Mutex
	codes := map[string]*grant{}

	http.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer": *issuer, "authorization_endpoint": *issuer + "/authorize", "token_endpoint": *issuer + "/token",
			"jwks_uri": *issuer + "/jwks", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"},
			"scopes_supported": []string{"openid", "profile", "email"},
		})
	})
	http.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk.Public()}})
	})

	// /authorize shows a "pick who you are" page; picking redirects back with a code.
	http.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client_id") != *clientID {
			http.Error(w, "unknown client_id", http.StatusBadRequest)
			return
		}
		if who := q.Get("as"); who != "" {
			for _, p := range people {
				if p.username != who {
					continue
				}
				code := randHex(16)
				mu.Lock()
				codes[code] = &grant{p, q.Get("client_id"), q.Get("nonce"), q.Get("code_challenge"), q.Get("redirect_uri"), time.Now().Add(time.Minute)}
				mu.Unlock()
				back, _ := url.Parse(q.Get("redirect_uri"))
				bq := back.Query()
				bq.Set("code", code)
				bq.Set("state", q.Get("state"))
				back.RawQuery = bq.Encode()
				log.Printf("authorize: signed in %s", p.username)
				http.Redirect(w, r, back.String(), http.StatusFound)
				return
			}
		}
		if q.Get("deny") != "" {
			back, _ := url.Parse(q.Get("redirect_uri"))
			bq := back.Query()
			bq.Set("error", "access_denied")
			bq.Set("error_description", "You cancelled the sign-in")
			bq.Set("state", q.Get("state"))
			back.RawQuery = bq.Encode()
			http.Redirect(w, r, back.String(), http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Fake OIDC</title><body style="font-family:sans-serif;background:#111;color:#eee;padding:40px"><h1>Fake OIDC provider</h1><p>Sign in as:</p>`)
		for _, p := range people {
			q.Set("as", p.username)
			fmt.Fprintf(w, `<p><a style="color:#2dd4bf;font-size:20px" href="/authorize?%s">%s (%s)</a></p>`, html.EscapeString(q.Encode()), html.EscapeString(p.name), html.EscapeString(p.email))
		}
		q.Del("as")
		q.Set("deny", "1")
		fmt.Fprintf(w, `<p><a style="color:#aaa" href="/authorize?%s">Cancel</a></p>`, html.EscapeString(q.Encode()))
	})

	http.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		id, sec, ok := r.BasicAuth()
		if !ok {
			id, sec = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		if id != *clientID || sec != *secret {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]string{"error": "invalid_client"})
			return
		}
		mu.Lock()
		g := codes[r.PostForm.Get("code")]
		delete(codes, r.PostForm.Get("code"))
		mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if g == nil || time.Now().After(g.expires) || g.redirect != r.PostForm.Get("redirect_uri") ||
			(g.challenge != "" && base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge) {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "invalid_grant"})
			return
		}
		now := time.Now()
		claims := map[string]any{
			"iss": *issuer, "sub": g.who.sub, "aud": g.clientID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
			"nonce": g.nonce, "email": g.who.email, "email_verified": true, "name": g.who.name, "preferred_username": g.who.username,
		}
		idt, err := jwt.Signed(signer).Claims(claims).Serialize()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"access_token": randHex(16), "token_type": "Bearer", "expires_in": 300, "id_token": idt})
	})

	log.Printf("fake OIDC provider on %s (issuer %s, client %s)", *addr, *issuer, *clientID)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
