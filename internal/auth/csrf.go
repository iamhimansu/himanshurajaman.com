package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

// NewCSRFToken returns a fresh 32-byte base64url CSRF token.
func NewCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CSRFValid reports whether the double-submit check passes: both the cookie
// value and the header value must be non-empty and equal under a constant-time
// comparison.
func CSRFValid(cookieValue, headerValue string) bool {
	if cookieValue == "" || headerValue == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookieValue), []byte(headerValue)) == 1
}

// CSRFCookie builds the readable (NOT HttpOnly) CSRF cookie so the SPA can read
// the token and echo it in the X-CSRF-Token header.
func (s *Sealer) CSRFCookie(token string) *http.Cookie {
	return s.newCookie(s.CSRFCookieName(), token, int(csrfTTL.Seconds()), false)
}
