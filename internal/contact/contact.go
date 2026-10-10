// Package contact implements the public contact form's email delivery. Sending
// sits behind a small Mailer interface so the transport can be swapped later
// (e.g. an HTTP email API if the deploy host blocks outbound SMTP) without
// touching the HTTP handler. The outgoing message is assembled with strict
// header sanitization so untrusted form input can never inject extra headers.
package contact

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Message is a single outgoing email assembled from a validated submission.
// Every field is treated as untrusted when turned into bytes (see buildMessage),
// so a value containing CR/LF cannot smuggle additional headers.
type Message struct {
	To      string
	From    string
	ReplyTo string
	Subject string
	Body    string
}

// Mailer sends a Message. Implementations must never let field contents inject
// extra headers; use buildMessage (or an equivalently safe assembler).
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Submission is the validated, trimmed user input the Service turns into a
// Message. The handler is responsible for all validation before calling Send.
type Submission struct {
	Name    string
	Email   string
	Message string
}

// Service bundles the Mailer, the resolved recipient/sender addresses, the
// per-IP rate limiter and the subject prefix. It is nil when contact is
// disabled; the handler treats a nil Service as 503.
type Service struct {
	mailer      Mailer
	to          string
	from        string
	subjectBase string
	limiter     *RateLimiter
	log         *slog.Logger
}

// NewService builds a contact Service. `to` is the recipient (CONTACT_TO, else
// ADMIN_EMAIL) and `from` is the envelope/From address (CONTACT_FROM, else
// SMTP_USERNAME). The rate limiter allows 5 submissions/hour per client IP.
func NewService(mailer Mailer, to, from string, log *slog.Logger) *Service {
	return &Service{
		mailer:      mailer,
		to:          to,
		from:        from,
		subjectBase: "[himanshurajaman.com] Contact from ",
		limiter:     NewRateLimiter(5, time.Hour, 50000),
		log:         log,
	}
}

// Allow reports whether a submission from ip is within the rate limit, counting
// this attempt against the window when allowed.
func (s *Service) Allow(ip string) bool { return s.limiter.Allow(ip, time.Now()) }

// Send assembles and dispatches the email for a validated submission. Reply-To
// is set to the submitter so a reply goes straight back to them.
func (s *Service) Send(ctx context.Context, sub Submission) error {
	msg := Message{
		To:      s.to,
		From:    s.from,
		ReplyTo: sub.Email,
		Subject: s.subjectBase + sub.Name,
		Body:    buildBody(sub),
	}
	return s.mailer.Send(ctx, msg)
}

// buildBody renders the plaintext body. The body is only ever treated as text
// (never interpolated into headers). Name and Email are single-line fields, so
// their line breaks are collapsed to keep the body tidy; the free-form Message
// keeps its newlines.
func buildBody(sub Submission) string {
	return fmt.Sprintf("Name: %s\nEmail: %s\n\n%s\n", collapseLine(sub.Name), collapseLine(sub.Email), sub.Message)
}

// collapseLine turns any CR/LF in a single-line field into spaces.
func collapseLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// buildMessage renders an RFC 5322 message. Every header value is sanitized to
// strip CR, LF and other control characters, so a submitter cannot fold a value
// into additional headers (SMTP header injection). The body is normalized to
// CRLF line endings and carried as UTF-8 text only.
func buildMessage(msg Message) []byte {
	var b strings.Builder
	writeHeader(&b, "From", msg.From)
	writeHeader(&b, "To", msg.To)
	if strings.TrimSpace(msg.ReplyTo) != "" {
		writeHeader(&b, "Reply-To", msg.ReplyTo)
	}
	writeHeader(&b, "Subject", msg.Subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(normalizeBody(msg.Body))
	return []byte(b.String())
}

func writeHeader(b *strings.Builder, name, value string) {
	b.WriteString(name)
	b.WriteString(": ")
	b.WriteString(sanitizeHeaderValue(value))
	b.WriteString("\r\n")
}

// sanitizeHeaderValue removes CR, LF, NUL and every other C0/C1 control
// character (plus DEL) from a header value and trims surrounding spaces. This
// is the single defense against SMTP header injection: no control byte survives
// to start a new header line.
func sanitizeHeaderValue(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(cleaned)
}

// normalizeBody rewrites all line endings to CRLF so the DATA block is
// well-formed regardless of what the submitter typed.
func normalizeBody(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}
