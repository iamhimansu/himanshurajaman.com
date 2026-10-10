package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/mail"
	"strings"

	"github.com/iamhimansu/himanshurajaman.com/internal/contact"
)

// Contact form limits. The body is bounded with MaxBytesReader; the field
// limits mirror the client-side checks (the server is authoritative).
const (
	maxContactBytes   = 64 << 10 // 64 KiB request body cap
	maxContactName    = 100
	maxContactEmail   = 200
	maxContactMessage = 5000
)

type contactRequest struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Message string `json:"message"`
	Website string `json:"website"` // honeypot: must be empty
}

// handleContact accepts a public, unauthenticated contact submission and emails
// it to the site owner. It is NOT readiness-gated (it needs only config + SMTP,
// not the content cache) and is registered before the SPA fallback so the
// fallback never swallows it. When contact is not configured it returns 503.
func (s *Server) handleContact(w http.ResponseWriter, r *http.Request) {
	if s.contact == nil {
		writeError(w, http.StatusServiceUnavailable, "contact unavailable")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxContactBytes)
	var req contactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	email := strings.TrimSpace(req.Email)
	message := strings.TrimSpace(req.Message)

	// Honeypot: a real browser leaves `website` empty (it is hidden off-screen
	// and aria-hidden). A non-empty value means an automated submission, so we
	// pretend success (200) but drop it silently without sending an email, and
	// log the spam hit for visibility.
	if strings.TrimSpace(req.Website) != "" {
		s.log.Info("contact spam dropped", "reason", "honeypot", "ip", clientIP(r))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	// Validation (authoritative; mirrors the client-side checks).
	if name == "" || len(name) > maxContactName {
		writeError(w, http.StatusBadRequest, "name is required and must be at most 100 characters")
		return
	}
	if email == "" || len(email) > maxContactEmail {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if message == "" || len(message) > maxContactMessage {
		writeError(w, http.StatusBadRequest, "message is required and must be at most 5000 characters")
		return
	}

	// Rate limit per client IP (RealIP middleware has already resolved the real
	// address into r.RemoteAddr).
	if !s.contact.Allow(clientIP(r)) {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "too many messages, please try again later")
		return
	}

	if err := s.contact.Send(r.Context(), contact.Submission{Name: name, Email: email, Message: message}); err != nil {
		// Log a single sanitized line: never the password (the mailer never puts
		// it in errors) and never the message body.
		s.log.Error("contact send failed", "error", sanitizeLog(err.Error()))
		writeError(w, http.StatusBadGateway, "could not send message, please try again later")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// clientIP extracts the host portion of r.RemoteAddr, which the RealIP
// middleware has already resolved from the proxy's X-Forwarded-For / X-Real-IP
// header (see the trusted-proxy note in router.go). It falls back to the raw
// value when there is no port. This is the key for the per-IP rate limiter.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// sanitizeLog collapses CR/LF so a wrapped error cannot inject extra log lines.
func sanitizeLog(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
