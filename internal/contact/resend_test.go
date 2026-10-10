package contact

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResendMailer_PostsCorrectJSON(t *testing.T) {
	var (
		gotAuth string
		gotPath string
		gotBody []byte
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"email_123"}`))
	}))
	defer ts.Close()

	m := NewResendMailer("re_secret_key")
	m.baseURL = ts.URL // overridable in tests (per the field's documentation)

	err := m.Send(context.Background(), Message{
		From: "from@example.com", To: "to@example.com", ReplyTo: "reply@example.com",
		Subject: "A subject", Body: "A body line",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/emails" {
		t.Errorf("posted to %q, want /emails", gotPath)
	}
	if gotAuth != "Bearer re_secret_key" {
		t.Errorf("Authorization = %q, want Bearer re_secret_key", gotAuth)
	}

	var payload struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		ReplyTo string   `json:"reply_to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if payload.From != "from@example.com" {
		t.Errorf("from = %q", payload.From)
	}
	if len(payload.To) != 1 || payload.To[0] != "to@example.com" {
		t.Errorf("to = %v", payload.To)
	}
	if payload.ReplyTo != "reply@example.com" {
		t.Errorf("reply_to = %q", payload.ReplyTo)
	}
	if payload.Subject != "A subject" {
		t.Errorf("subject = %q", payload.Subject)
	}
	if payload.Text != "A body line" {
		t.Errorf("text = %q", payload.Text)
	}
}

func TestResendMailer_Non2xxError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"invalid from address"}`))
	}))
	defer ts.Close()

	m := NewResendMailer("re_secret_key")
	m.baseURL = ts.URL
	if err := m.Send(context.Background(), Message{From: "f@x.com", To: "t@x.com", Subject: "s", Body: "b"}); err == nil {
		t.Fatal("expected an error on a non-2xx response")
	}
}
