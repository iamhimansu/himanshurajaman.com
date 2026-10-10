package testutil

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
)

// OIDCServer is a minimal, hermetic stand-in for a Google-style OpenID Connect
// issuer. It serves the discovery document (with its own URL as the issuer),
// a JWKS exposing one RSA public key, and signs RS256 ID tokens with the
// matching private key. It is enough for github.com/coreos/go-oidc's
// NewProvider discovery and IDTokenVerifier to succeed against it.
type OIDCServer struct {
	Server *httptest.Server
	key    *rsa.PrivateKey
	kid    string
}

// NewOIDCServer starts the mock issuer registered for cleanup.
func NewOIDCServer(t testing.TB) *OIDCServer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("testutil: generate rsa key: %v", err)
	}
	o := &OIDCServer{key: key, kid: "test-key-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := o.Server.URL
		writeJSON(w, map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/auth",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, o.jwks(&key.PublicKey, o.kid))
	})
	o.Server = httptest.NewServer(mux)
	t.Cleanup(o.Server.Close)
	return o
}

// IssuerURL returns the issuer URL to pass through auth.WithIssuer. It equals
// the discovery document's `issuer` field (go-oidc requires an exact match).
func (o *OIDCServer) IssuerURL() string { return o.Server.URL }

// SignIDToken returns an RS256-signed JWT for the given claims, signed by the
// key published in the JWKS. The issuer claim is forced to IssuerURL and
// iat/exp are defaulted to a valid one-hour window when absent; callers supply
// aud (the client ID), sub, email, email_verified and nonce.
func (o *OIDCServer) SignIDToken(claims map[string]any) string {
	return o.sign(claims, o.key, o.kid)
}

// SignIDTokenUnknownKey signs with a throwaway key under the same kid, so the
// JWKS lookup finds a key but signature verification fails — a "bad signature"
// token.
func (o *OIDCServer) SignIDTokenUnknownKey(t testing.TB, claims map[string]any) string {
	t.Helper()
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("testutil: generate rsa key: %v", err)
	}
	return o.sign(claims, other, o.kid)
}

func (o *OIDCServer) sign(claims map[string]any, key *rsa.PrivateKey, kid string) string {
	c := map[string]any{}
	for k, v := range claims {
		c[k] = v
	}
	c["iss"] = o.Server.URL
	if _, ok := c["iat"]; !ok {
		c["iat"] = nowUnix()
	}
	if _, ok := c["exp"]; !ok {
		c["exp"] = nowUnix() + 3600
	}

	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}
	segments := b64url(mustJSON(header)) + "." + b64url(mustJSON(c))
	digest := sha256.Sum256([]byte(segments))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		panic("testutil: sign jwt: " + err.Error())
	}
	return segments + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// jwks returns a JWK Set JSON object exposing pub under kid.
func (o *OIDCServer) jwks(pub *rsa.PublicKey, kid string) map[string]any {
	return map[string]any{
		"keys": []any{
			map[string]any{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			},
		},
	}
}

func (o *OIDCServer) ServerURL() string { return o.Server.URL }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("testutil: marshal: " + err.Error())
	}
	return b
}
