package contact

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// defaultResendBaseURL is Resend's API base; overridable (via the unexported
// baseURL field) in tests.
const defaultResendBaseURL = "https://api.resend.com"

// ResendMailer delivers mail through Resend's HTTP API (POST /emails). It is the
// Render-free-compatible transport: unlike SMTP it needs no outbound SMTP port
// (which free hosts commonly block). The From address must be on a domain
// verified in the Resend dashboard.
type ResendMailer struct {
	apiKey  string // sensitive: never logged
	baseURL string
	client  *http.Client
}

// NewResendMailer builds a ResendMailer with a 15s HTTP timeout.
func NewResendMailer(apiKey string) *ResendMailer {
	return &ResendMailer{
		apiKey:  apiKey,
		baseURL: defaultResendBaseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// resendPayload is the JSON body for POST /emails. Resend treats these as
// structured fields (not raw headers), so header injection is not possible;
// values are still sanitized to single lines as defense in depth.
type resendPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	ReplyTo string   `json:"reply_to,omitempty"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

// Send posts the message to Resend. A non-2xx response is an error; the API key
// is never placed in the request log or the returned error.
func (m *ResendMailer) Send(ctx context.Context, msg Message) error {
	payload := resendPayload{
		From:    sanitizeHeaderValue(msg.From),
		To:      []string{sanitizeHeaderValue(msg.To)},
		ReplyTo: sanitizeHeaderValue(msg.ReplyTo),
		Subject: sanitizeHeaderValue(msg.Subject),
		Text:    msg.Body,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("resend marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend send: %w", err)
	}
	defer resp.Body.Close()
	// Drain a bounded amount so the connection can be reused.
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The snippet is Resend's (non-sensitive) error JSON; the key is never in it.
		return fmt.Errorf("resend: unexpected status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}
