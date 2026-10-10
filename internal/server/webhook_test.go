package server_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// waitForResync waits until the background push-resync has observably completed
// (readyz commit moved off the seeded value) so test cleanup never closes the
// store mid-ingest.
func waitForResync(t *testing.T, h *harness, from string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rr := h.serve(httptest.NewRequest(http.MethodGet, "/readyz", nil))
		var body map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if c, _ := body["commit"].(string); c != "" && c != from {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("background resync did not complete in time")
}

func TestWebhook_ValidPush202(t *testing.T) {
	h := newServer(t, options{webhookSecret: "whsecret"})
	body := []byte(`{"ref":"refs/heads/main"}`)

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign("whsecret", body))
	req.Header.Set("X-GitHub-Event", "push")

	rr := h.serve(req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("valid push = %d, want 202", rr.Code)
	}
	// Let the single-flight background resync finish before the store closes.
	waitForResync(t, h, "test-commit")
}

func TestWebhook_BadSignature401(t *testing.T) {
	h := newServer(t, options{webhookSecret: "whsecret"})
	body := []byte(`{"ref":"refs/heads/main"}`)

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	req.Header.Set("X-GitHub-Event", "push")

	if rr := h.serve(req); rr.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature = %d, want 401", rr.Code)
	}
}

func TestWebhook_MissingSignature401(t *testing.T) {
	h := newServer(t, options{webhookSecret: "whsecret"})
	body := []byte(`{"ref":"refs/heads/main"}`)

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")

	if rr := h.serve(req); rr.Code != http.StatusUnauthorized {
		t.Fatalf("missing signature = %d, want 401", rr.Code)
	}
}

func TestWebhook_UnsetSecret404(t *testing.T) {
	h := newServer(t, options{}) // no webhook secret -> endpoint disabled
	body := []byte(`{"ref":"refs/heads/main"}`)

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign("whatever", body))
	req.Header.Set("X-GitHub-Event", "push")

	if rr := h.serve(req); rr.Code != http.StatusNotFound {
		t.Fatalf("unset secret = %d, want 404", rr.Code)
	}
}
