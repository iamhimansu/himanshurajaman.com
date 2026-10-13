package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/iamhimansu/himanshurajaman.com/internal/auth"
)

// handleAuthLogin begins the OAuth flow: it validates the optional next target,
// mints state/nonce/PKCE verifier, seals them into the short-lived hra_oauth
// cookie, and redirects to Google's consent screen.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	next := auth.ValidateNext(r.URL.Query().Get("next"))

	state, err := randToken()
	if err != nil {
		s.log.Error("auth login: state", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	nonce, err := randToken()
	if err != nil {
		s.log.Error("auth login: nonce", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	verifier := oauth2.GenerateVerifier()

	sealed, err := s.sealer.SealOAuth(auth.OAuthState{
		State:    state,
		Nonce:    nonce,
		Verifier: verifier,
		Next:     next,
	}, time.Now())
	if err != nil {
		s.log.Error("auth login: seal oauth", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	url, err := s.auth.AuthCodeURL(r.Context(), state, nonce, verifier)
	if err != nil {
		if errors.Is(err, auth.ErrAuthUnavailable) {
			s.log.Warn("auth login: provider unavailable")
			writeError(w, http.StatusServiceUnavailable, "auth temporarily unavailable")
			return
		}
		s.log.Error("auth login: authcode url", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	http.SetCookie(w, s.sealer.OAuthCookie(sealed))
	http.Redirect(w, r, url, http.StatusFound)
}

// handleAuthCallback completes the OAuth flow: it opens hra_oauth, verifies the
// state, exchanges the code with the PKCE verifier, verifies the ID token and
// the allowlist, then mints the session + CSRF cookies and redirects to next.
// The hra_oauth cookie is always cleared.
func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	c, err := r.Cookie(s.sealer.OAuthCookieName())
	if err != nil {
		s.clearCookie(w, s.sealer.OAuthCookieName())
		s.redirectLogin(w, r, "auth")
		return
	}
	st, err := s.sealer.OpenOAuth(c.Value, now)
	// Clear the temp cookie regardless of what happens next.
	s.clearCookie(w, s.sealer.OAuthCookieName())
	if err != nil {
		s.redirectLogin(w, r, "auth")
		return
	}

	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(st.State)) != 1 {
		s.redirectLogin(w, r, "auth")
		return
	}
	code := q.Get("code")
	if code == "" {
		s.redirectLogin(w, r, "auth")
		return
	}

	tok, err := s.auth.Exchange(r.Context(), code, st.Verifier)
	if err != nil {
		if errors.Is(err, auth.ErrAuthUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "auth temporarily unavailable")
			return
		}
		s.redirectLogin(w, r, "auth")
		return
	}

	email, err := s.auth.VerifyLogin(r.Context(), tok, st.Nonce)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrAuthUnavailable):
			writeError(w, http.StatusServiceUnavailable, "auth temporarily unavailable")
		case errors.Is(err, auth.ErrLoginRejected):
			// Do NOT log the rejected email (PII).
			s.log.Warn("login rejected: email not allowlisted")
			s.redirectLogin(w, r, "forbidden")
		default:
			s.redirectLogin(w, r, "auth")
		}
		return
	}

	sessTok, err := s.sealer.SealSession(email, now)
	if err != nil {
		s.log.Error("auth callback: seal session", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	csrf, err := auth.NewCSRFToken()
	if err != nil {
		s.log.Error("auth callback: csrf token", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, s.sealer.SessionCookie(sessTok))
	http.SetCookie(w, s.sealer.CSRFCookie(csrf))
	http.Redirect(w, r, st.Next, http.StatusFound)
}

// handleAuthLogout clears the session + CSRF cookies. It runs behind
// requireSession + requireCSRF.
func (s *Server) handleAuthLogout(w http.ResponseWriter, _ *http.Request) {
	s.clearCookie(w, s.sealer.SessionCookieName())
	s.clearCookie(w, s.sealer.CSRFCookieName())
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminMe returns the signed-in identity, the CSRF token (issued+set when
// absent), whether GitHub writes are possible, and whether AI drafting is
// configured (so the SPA can conditionally render the AI panel). It runs behind
// requireAdminEnabled + requireSession.
func (s *Server) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	email, _ := r.Context().Value(ctxKeyEmail).(string)

	var token string
	if c, err := r.Cookie(s.sealer.CSRFCookieName()); err == nil && c.Value != "" {
		token = c.Value
	} else {
		t, terr := auth.NewCSRFToken()
		if terr != nil {
			s.log.Error("admin me: csrf token", "error", terr.Error())
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		token = t
		http.SetCookie(w, s.sealer.CSRFCookie(token))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"email":         email,
		"csrfToken":     token,
		"writesEnabled": s.writesEnabled,
		"aiEnabled":     s.aiEnabled,
	})
}

// redirectLogin sends the browser to the SPA login screen with an error code.
func (s *Server) redirectLogin(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/admin/login?error="+code, http.StatusFound)
}

// clearCookie expires the named cookie.
func (s *Server) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, s.sealer.ClearCookie(name))
}

// randToken returns 32 random bytes as a base64url string, used for the OAuth
// state and OIDC nonce.
func randToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
