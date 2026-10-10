package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/iamhimansu/himanshurajaman.com/internal/auth"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

func newSealer(t *testing.T, secure bool) *auth.Sealer {
	t.Helper()
	s, err := auth.NewSealer(testSecret, secure)
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	return s
}

func TestSealer_SessionRoundTrip(t *testing.T) {
	s := newSealer(t, false)
	now := time.Now()
	tok, err := s.SealSession("admin@example.com", now)
	if err != nil {
		t.Fatalf("SealSession: %v", err)
	}
	sess, err := s.OpenSession(tok, now)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if sess.Email != "admin@example.com" {
		t.Errorf("email = %q, want admin@example.com", sess.Email)
	}
}

func TestOpenSession_Expired(t *testing.T) {
	s := newSealer(t, false)
	// Seal 13h in the past so the 12h TTL has elapsed relative to now.
	past := time.Now().Add(-13 * time.Hour)
	tok, err := s.SealSession("admin@example.com", past)
	if err != nil {
		t.Fatalf("SealSession: %v", err)
	}
	if _, err := s.OpenSession(tok, time.Now()); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("OpenSession(expired) = %v, want ErrInvalidToken", err)
	}
}

func TestOpenSession_Tampered(t *testing.T) {
	s := newSealer(t, false)
	now := time.Now()
	tok, err := s.SealSession("admin@example.com", now)
	if err != nil {
		t.Fatalf("SealSession: %v", err)
	}
	b := []byte(tok)
	i := len(b) / 2
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	if _, err := s.OpenSession(string(b), now); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("OpenSession(tampered) = %v, want ErrInvalidToken", err)
	}
}

func TestOpen_CrossAADFails(t *testing.T) {
	s := newSealer(t, false)
	now := time.Now()
	// A token sealed for the oauth purpose must not open as a session.
	oauthTok, err := s.SealOAuth(auth.OAuthState{State: "st", Nonce: "n", Verifier: "v", Next: "/admin"}, now)
	if err != nil {
		t.Fatalf("SealOAuth: %v", err)
	}
	if _, err := s.OpenSession(oauthTok, now); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("OpenSession(oauth token) = %v, want ErrInvalidToken", err)
	}
}

func TestNewSealer_ShortSecret(t *testing.T) {
	if _, err := auth.NewSealer([]byte("too-short"), false); err == nil {
		t.Fatal("expected error for a secret shorter than 32 bytes")
	}
}

func TestCookieNames_SecurePrefix(t *testing.T) {
	insecure := newSealer(t, false)
	if got := insecure.SessionCookieName(); got != "hra_session" {
		t.Errorf("insecure session cookie = %q, want hra_session", got)
	}
	secure := newSealer(t, true)
	if got := secure.SessionCookieName(); got != "__Host-hra_session" {
		t.Errorf("secure session cookie = %q, want __Host-hra_session", got)
	}
	if got := secure.CSRFCookieName(); got != "__Host-hra_csrf" {
		t.Errorf("secure csrf cookie = %q, want __Host-hra_csrf", got)
	}
}
