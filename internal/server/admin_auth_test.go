package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminAPI_NoSession401(t *testing.T) {
	h := newServer(t, options{adminEnabled: true})
	// No session cookie: requireSession runs before requireReady, so this is 401
	// (auth-before-readiness), NOT 503.
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/admin/posts", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d, want 401", rr.Code)
	}
}

func TestAdminDisabled503(t *testing.T) {
	h := newServer(t, options{adminEnabled: false})
	for _, path := range []string{"/api/admin/me", "/auth/login"} {
		rr := h.serve(httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("%s with admin disabled = %d, want 503", path, rr.Code)
		}
	}
}

func TestAdminWrite_MissingCSRF403(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/posts", strings.NewReader("{}"))
	req.AddCookie(h.sessionCookie("admin@example.com"))
	// Valid session but no CSRF cookie/header.
	if rr := h.serve(req); rr.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF = %d, want 403", rr.Code)
	}
}

func TestAdminWrite_InvalidCSRF403(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	csrfCookie, _ := h.csrfPair()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/posts", strings.NewReader("{}"))
	req.AddCookie(h.sessionCookie("admin@example.com"))
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", "a-different-token") // mismatch
	if rr := h.serve(req); rr.Code != http.StatusForbidden {
		t.Fatalf("invalid CSRF = %d, want 403", rr.Code)
	}
}

func TestAdmin_TamperedSession401(t *testing.T) {
	h := newServer(t, options{adminEnabled: true})
	c := h.sessionCookie("admin@example.com")
	b := []byte(c.Value)
	i := len(b) / 2
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	c.Value = string(b)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	req.AddCookie(c)
	if rr := h.serve(req); rr.Code != http.StatusUnauthorized {
		t.Fatalf("tampered session = %d, want 401", rr.Code)
	}
}

func TestAdmin_ExpiredSession401(t *testing.T) {
	h := newServer(t, options{adminEnabled: true})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	req.AddCookie(h.expiredSessionCookie("admin@example.com"))
	if rr := h.serve(req); rr.Code != http.StatusUnauthorized {
		t.Fatalf("expired session = %d, want 401", rr.Code)
	}
}

func TestAuthCallback_GuardsRedirectNoSession(t *testing.T) {
	h := newServer(t, options{adminEnabled: true})

	t.Run("no oauth cookie", func(t *testing.T) {
		rr := h.serve(httptest.NewRequest(http.MethodGet, "/auth/callback?code=x&state=y", nil))
		assertRedirectNoSession(t, h, rr)
	})

	t.Run("state mismatch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=x&state=attacker-state", nil)
		req.AddCookie(h.oauthCookie("real-state"))
		rr := h.serve(req)
		assertRedirectNoSession(t, h, rr)
	})
}

func assertRedirectNoSession(t *testing.T, h *harness, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if loc != "/admin/login?error=auth" {
		t.Errorf("Location = %q, want /admin/login?error=auth", loc)
	}
	if h.hasSessionCookie(rr) {
		t.Error("no session cookie should be minted on a guarded callback")
	}
}
