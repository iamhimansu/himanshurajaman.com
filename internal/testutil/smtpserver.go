package testutil

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
)

// FakeSMTPServer is a minimal SMTP listener for testing the SMTPMailer's
// fail-closed behavior. It speaks just enough of the protocol to answer the
// greeting and EHLO; whether it advertises STARTTLS is configurable. It records
// whether the client ever reached AUTH or DATA, so a test can prove the mailer
// aborted before sending credentials or the message when STARTTLS is absent.
type FakeSMTPServer struct {
	ln      net.Listener
	starttl bool

	mu           sync.Mutex
	reachedAUTH  bool
	reachedDATA  bool
	reachedHELLO bool
}

// NewFakeSMTPServer starts a listener on 127.0.0.1. advertiseSTARTTLS controls
// whether the EHLO reply offers the STARTTLS extension. The listener is closed
// on cleanup.
func NewFakeSMTPServer(t testing.TB, advertiseSTARTTLS bool) *FakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("testutil: smtp listen: %v", err)
	}
	s := &FakeSMTPServer{ln: ln, starttl: advertiseSTARTTLS}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

// Host returns the listener host (127.0.0.1).
func (s *FakeSMTPServer) Host() string {
	host, _, _ := net.SplitHostPort(s.ln.Addr().String())
	return host
}

// Port returns the listener port.
func (s *FakeSMTPServer) Port() string {
	_, port, _ := net.SplitHostPort(s.ln.Addr().String())
	return port
}

// ReachedAUTH reports whether the client issued an AUTH command.
func (s *FakeSMTPServer) ReachedAUTH() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reachedAUTH
}

// ReachedDATA reports whether the client issued a DATA command.
func (s *FakeSMTPServer) ReachedDATA() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reachedDATA
}

func (s *FakeSMTPServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed
		}
		go s.handle(conn)
	}
}

func (s *FakeSMTPServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	br := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }

	write("220 fakesmtp ESMTP ready")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			s.mu.Lock()
			s.reachedHELLO = true
			s.mu.Unlock()
			// Multiline 250: all but the last use "250-", the last uses "250 ".
			write("250-fakesmtp greets you")
			write("250-SIZE 1048576")
			if s.starttl {
				write("250-STARTTLS")
			}
			write("250 8BITMIME")
		case strings.HasPrefix(cmd, "HELO"):
			s.mu.Lock()
			s.reachedHELLO = true
			s.mu.Unlock()
			write("250 fakesmtp")
		case strings.HasPrefix(cmd, "AUTH"):
			s.mu.Lock()
			s.reachedAUTH = true
			s.mu.Unlock()
			write("535 authentication rejected")
		case strings.HasPrefix(cmd, "DATA"):
			s.mu.Lock()
			s.reachedDATA = true
			s.mu.Unlock()
			write("554 no data accepted")
		case strings.HasPrefix(cmd, "QUIT"):
			write("221 bye")
			return
		default:
			write("250 ok")
		}
	}
}
