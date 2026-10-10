package contact_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/contact"
)

// fakeMailer records the last message it was asked to send.
type fakeMailer struct {
	called int
	last   contact.Message
	err    error
}

func (f *fakeMailer) Send(_ context.Context, msg contact.Message) error {
	f.called++
	f.last = msg
	return f.err
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestService_SendSetsReplyTo(t *testing.T) {
	fm := &fakeMailer{}
	svc := contact.NewService(fm, "owner@example.com", "noreply@example.com", discardLogger())

	err := svc.Send(context.Background(), contact.Submission{
		Name: "Alice", Email: "alice@example.com", Message: "Hello there",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if fm.called != 1 {
		t.Fatalf("mailer called %d times, want 1", fm.called)
	}
	if fm.last.To != "owner@example.com" {
		t.Errorf("To = %q, want owner@example.com", fm.last.To)
	}
	if fm.last.From != "noreply@example.com" {
		t.Errorf("From = %q, want noreply@example.com", fm.last.From)
	}
	// Reply-To is set to the submitter so a reply goes back to them.
	if fm.last.ReplyTo != "alice@example.com" {
		t.Errorf("ReplyTo = %q, want alice@example.com", fm.last.ReplyTo)
	}
	if !strings.Contains(fm.last.Subject, "Alice") {
		t.Errorf("Subject should mention the sender: %q", fm.last.Subject)
	}
	if !strings.Contains(fm.last.Body, "Hello there") {
		t.Errorf("Body should contain the message: %q", fm.last.Body)
	}
}
