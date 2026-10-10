package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postContact(t *testing.T, h *harness, jsonBody string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/contact", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	return h.serve(req)
}

func settingsContactEnabled(t *testing.T, h *harness) bool {
	t.Helper()
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/settings = %d, want 200", rr.Code)
	}
	var s struct {
		ContactEnabled bool `json:"contactEnabled"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	return s.ContactEnabled
}

func TestContact_HoneypotAccepted200NoSend(t *testing.T) {
	h := newServer(t, options{contact: true})
	rr := postContact(t, h, `{"name":"Alice","email":"alice@example.com","message":"Hello","website":"i-am-a-bot"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("honeypot submission = %d, want 200", rr.Code)
	}
	if h.mailer.called != 0 {
		t.Errorf("honeypot submission must NOT send mail, mailer called %d times", h.mailer.called)
	}
}

func TestContact_RateLimited429(t *testing.T) {
	h := newServer(t, options{contact: true})
	const valid = `{"name":"Alice","email":"alice@example.com","message":"Hello there"}`

	for i := 0; i < 5; i++ {
		if rr := postContact(t, h, valid); rr.Code != http.StatusOK {
			t.Fatalf("submission %d = %d, want 200", i+1, rr.Code)
		}
	}
	rr := postContact(t, h, valid)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("6th submission = %d, want 429", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("429 response should carry a Retry-After header")
	}
}

func TestContact_Disabled503_SettingsFalse(t *testing.T) {
	h := newServer(t, options{}) // contact not configured
	if rr := postContact(t, h, `{"name":"A","email":"a@example.com","message":"hi"}`); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled contact = %d, want 503", rr.Code)
	}
	if settingsContactEnabled(t, h) {
		t.Error("settings.contactEnabled should be false when contact is disabled")
	}
}

func TestContact_ValidSends(t *testing.T) {
	h := newServer(t, options{contact: true})
	rr := postContact(t, h, `{"name":"Alice","email":"alice@example.com","message":"Hello there"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("valid submission = %d, want 200", rr.Code)
	}
	if h.mailer.called != 1 {
		t.Fatalf("mailer called %d times, want 1", h.mailer.called)
	}
	if h.mailer.last.To != "owner@example.com" {
		t.Errorf("To = %q, want owner@example.com", h.mailer.last.To)
	}
	if h.mailer.last.ReplyTo != "alice@example.com" {
		t.Errorf("ReplyTo = %q, want the submitter's address", h.mailer.last.ReplyTo)
	}
	if !settingsContactEnabled(t, h) {
		t.Error("settings.contactEnabled should be true when contact is configured")
	}
}
