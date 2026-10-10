package contact

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

// SMTPMailer is the default Mailer: it delivers over SMTP, requiring STARTTLS
// before PLAIN auth (fail-closed — credentials are never sent over a cleartext
// link). It is the swappable transport — if the deploy host blocks outbound
// SMTP, replace it with an HTTP email API implementation of Mailer without
// changing the handler or Service.
type SMTPMailer struct {
	host     string
	port     string
	username string
	password string // sensitive: never logged
	timeout  time.Duration
}

// NewSMTPMailer builds an SMTPMailer. An empty port defaults to 587 (submission
// with STARTTLS).
func NewSMTPMailer(host, port, username, password string) *SMTPMailer {
	if port == "" {
		port = "587"
	}
	return &SMTPMailer{
		host:     host,
		port:     port,
		username: username,
		password: password,
		timeout:  15 * time.Second,
	}
}

// Send dials the SMTP server, upgrades to TLS via STARTTLS (mandatory),
// authenticates, and transmits the assembled message. The message bytes are
// built by buildMessage, which sanitizes every header value, so form input
// cannot inject headers over the wire.
func (m *SMTPMailer) Send(ctx context.Context, msg Message) (err error) {
	addr := net.JoinHostPort(m.host, m.port)

	d := net.Dialer{Timeout: m.timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(m.timeout))
	}

	c, err := smtp.NewClient(conn, m.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() {
		// Prefer QUIT; fall back to Close so the connection never leaks.
		if qErr := c.Quit(); qErr != nil && err == nil {
			_ = c.Close()
		}
	}()

	// STARTTLS is mandatory before AUTH: credentials must never cross a cleartext
	// link. If the server does not advertise STARTTLS, abort before sending any
	// AUTH rather than falling back to PLAIN over plaintext. This is moot on
	// Render's free tier (outbound SMTP is blocked, so Send never gets here) but
	// correct for local SMTP and any other host.
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return fmt.Errorf("smtp: server does not offer STARTTLS; refusing to send credentials over cleartext")
	}
	if err = c.StartTLS(&tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}); err != nil {
		return fmt.Errorf("smtp starttls: %w", err)
	}

	// Authenticate only over the now-encrypted connection.
	if ok, _ := c.Extension("AUTH"); ok {
		if err = c.Auth(smtp.PlainAuth("", m.username, m.password, m.host)); err != nil {
			// Do not wrap: the error may echo server text, but never the password.
			return fmt.Errorf("smtp auth failed")
		}
	}

	if err = c.Mail(msg.From); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err = c.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}

	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err = wc.Write(buildMessage(msg)); err != nil {
		_ = wc.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err = wc.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return nil
}
