package contact

import (
	"strings"
	"testing"
)

func TestSanitizeHeaderValue(t *testing.T) {
	// CR/LF/NUL and other control chars are removed; tabs become spaces; the
	// result is trimmed.
	got := sanitizeHeaderValue("Hello\r\nBcc: evil@example.com\x00\tworld ")
	if strings.ContainsAny(got, "\r\n\x00") {
		t.Errorf("control characters survived: %q", got)
	}
	if got != "HelloBcc: evil@example.com world" {
		t.Errorf("sanitizeHeaderValue = %q", got)
	}
}

func TestBuildMessage_StripsHeaderInjection(t *testing.T) {
	msg := Message{
		From:    "from@example.com",
		To:      "to@example.com",
		ReplyTo: "user@example.com",
		// A CRLF injection attempt in the subject (as if folded from the name).
		Subject: "Contact from Mallory\r\nBcc: victim@example.com",
		Body:    "line one\nline two",
	}
	raw := string(buildMessage(msg))

	// The folded "Bcc:" must not survive as its own header line.
	if strings.Contains(raw, "\r\nBcc:") {
		t.Errorf("header injection succeeded:\n%s", raw)
	}
	// The subject stays a single header with the CRLF collapsed out.
	if !strings.Contains(raw, "Subject: Contact from MalloryBcc: victim@example.com\r\n") {
		t.Errorf("subject not sanitized onto one line:\n%s", raw)
	}
	// Legitimate headers are present and the body is CRLF-normalized.
	for _, want := range []string{"From: from@example.com\r\n", "To: to@example.com\r\n", "Reply-To: user@example.com\r\n", "line one\r\nline two"} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %q in:\n%s", want, raw)
		}
	}
}
