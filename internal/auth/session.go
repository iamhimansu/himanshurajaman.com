// Package auth implements the admin authentication primitives: stateless
// sealed cookies (session + OAuth-temp), double-submit CSRF tokens, and a lazy
// Google OIDC authenticator. Cookie sealing is stdlib-only (AES-256-GCM with
// HKDF-SHA256 key derivation), so this package adds no cryptographic
// dependency.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Cookie base names (without the __Host- prefix, which is added in prod).
const (
	cookieSession = "hra_session"
	cookieOAuth   = "hra_oauth"
	cookieCSRF    = "hra_csrf"
)

// HKDF info strings bind each derived key to its single purpose.
const (
	infoSessionKey = "hra:session:aead:v1"
	infoOAuthKey   = "hra:oauth:aead:v1"
)

// AAD binds each sealed token to its purpose so a session token can never be
// opened as an oauth token (or vice versa) even if the keys were identical.
const (
	aadSession = "hra:session:v1"
	aadOAuth   = "hra:oauth:v1"
)

// Cookie lifetimes.
const (
	sessionTTL = 12 * time.Hour
	oauthTTL   = 10 * time.Minute
	csrfTTL    = 12 * time.Hour
)

// sealVersion is the single leading byte of every sealed token. A future format
// change bumps it and old tokens fail to open (treated as "no valid cookie").
const sealVersion = 0x01

// ErrInvalidToken is returned by open on any failure (version/nonce/AEAD/JSON).
// Callers treat it as "no valid cookie" and never log the raw token value.
var ErrInvalidToken = errors.New("auth: invalid token")

// Sealer owns the two per-purpose AES-256-GCM keys derived from SESSION_SECRET
// and the cookie flag policy (name prefix + Secure flag). It is safe for
// concurrent use.
type Sealer struct {
	sessionGCM cipher.AEAD
	oauthGCM   cipher.AEAD
	secure     bool
}

// NewSealer derives the session and oauth AEAD keys from secret via HKDF-SHA256
// and builds the AES-256-GCM ciphers. secret must be at least 32 bytes (config
// enforces this at boot). secure selects __Host- cookie names and the Secure
// flag (true in production over HTTPS).
func NewSealer(secret []byte, secure bool) (*Sealer, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("auth: session secret must be at least 32 bytes, got %d", len(secret))
	}
	sessGCM, err := gcmFromKey(secret, infoSessionKey)
	if err != nil {
		return nil, err
	}
	oauthGCM, err := gcmFromKey(secret, infoOAuthKey)
	if err != nil {
		return nil, err
	}
	return &Sealer{sessionGCM: sessGCM, oauthGCM: oauthGCM, secure: secure}, nil
}

func gcmFromKey(secret []byte, info string) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, secret, nil, info, 32)
	if err != nil {
		return nil, fmt.Errorf("auth: derive key %q: %w", info, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("auth: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: gcm: %w", err)
	}
	return gcm, nil
}

// seal returns base64url(version || nonce || AES-GCM.Seal(plaintext, aad)).
func seal(gcm cipher.AEAD, plaintext []byte, aad string) (string, error) {
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+gcm.Overhead())
	out = append(out, sealVersion)
	out = append(out, nonce...)
	out = gcm.Seal(out, nonce, plaintext, []byte(aad))
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// open verifies the version byte, splits the nonce, and AEAD-opens the token.
// Any failure returns ErrInvalidToken; the raw token is never included.
func open(gcm cipher.AEAD, token, aad string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidToken
	}
	ns := gcm.NonceSize()
	if len(raw) < 1+ns+gcm.Overhead() || raw[0] != sealVersion {
		return nil, ErrInvalidToken
	}
	nonce := raw[1 : 1+ns]
	ct := raw[1+ns:]
	plaintext, err := gcm.Open(nil, nonce, ct, []byte(aad))
	if err != nil {
		return nil, ErrInvalidToken
	}
	return plaintext, nil
}

// Session is the plaintext payload of the hra_session cookie.
type Session struct {
	Email string `json:"e"`
	IssAt int64  `json:"iat"`
	Exp   int64  `json:"exp"`
}

// OAuthState is the plaintext payload of the short-lived hra_oauth cookie,
// carrying the OAuth state, OIDC nonce, PKCE verifier, validated next path, and
// issue time.
type OAuthState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"nx"`
	IssAt    int64  `json:"iat"`
}

// SealSession seals a session for email valid for 12h from now.
func (s *Sealer) SealSession(email string, now time.Time) (string, error) {
	sess := Session{Email: email, IssAt: now.Unix(), Exp: now.Add(sessionTTL).Unix()}
	b, err := json.Marshal(sess)
	if err != nil {
		return "", err
	}
	return seal(s.sessionGCM, b, aadSession)
}

// OpenSession opens and validates a session token: it must decode and not be
// expired relative to now.
func (s *Sealer) OpenSession(token string, now time.Time) (*Session, error) {
	b, err := open(s.sessionGCM, token, aadSession)
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(b, &sess); err != nil {
		return nil, ErrInvalidToken
	}
	if now.Unix() >= sess.Exp {
		return nil, ErrInvalidToken
	}
	return &sess, nil
}

// SealOAuth seals the OAuth-temp state (state/nonce/verifier/next) stamped at now.
func (s *Sealer) SealOAuth(st OAuthState, now time.Time) (string, error) {
	st.IssAt = now.Unix()
	b, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	return seal(s.oauthGCM, b, aadOAuth)
}

// OpenOAuth opens and validates an OAuth-temp token: it must decode and be
// within the 10-minute window relative to now.
func (s *Sealer) OpenOAuth(token string, now time.Time) (*OAuthState, error) {
	b, err := open(s.oauthGCM, token, aadOAuth)
	if err != nil {
		return nil, err
	}
	var st OAuthState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, ErrInvalidToken
	}
	if now.Unix() >= st.IssAt+int64(oauthTTL.Seconds()) {
		return nil, ErrInvalidToken
	}
	return &st, nil
}

// --- cookie helpers ---

// CookieName returns the cookie name with the __Host- prefix when running in
// secure (production) mode; the prefix requires Secure + Path=/ + no Domain,
// which holds for all cookies this package sets.
func (s *Sealer) CookieName(base string) string {
	if s.secure {
		return "__Host-" + base
	}
	return base
}

// SessionCookieName / OAuthCookieName / CSRFCookieName return the environment-
// correct cookie names.
func (s *Sealer) SessionCookieName() string { return s.CookieName(cookieSession) }
func (s *Sealer) OAuthCookieName() string   { return s.CookieName(cookieOAuth) }
func (s *Sealer) CSRFCookieName() string    { return s.CookieName(cookieCSRF) }

// newCookie builds a cookie with the shared flag policy. httpOnly is false only
// for the readable CSRF cookie.
func (s *Sealer) newCookie(name, value string, maxAge int, httpOnly bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: httpOnly,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

// SessionCookie builds the HttpOnly session cookie carrying value.
func (s *Sealer) SessionCookie(value string) *http.Cookie {
	return s.newCookie(s.SessionCookieName(), value, int(sessionTTL.Seconds()), true)
}

// OAuthCookie builds the short-lived HttpOnly oauth-temp cookie.
func (s *Sealer) OAuthCookie(value string) *http.Cookie {
	return s.newCookie(s.OAuthCookieName(), value, int(oauthTTL.Seconds()), true)
}

// ClearCookie returns a cookie that expires the named cookie immediately.
func (s *Sealer) ClearCookie(name string) *http.Cookie {
	c := s.newCookie(name, "", -1, true)
	return c
}
