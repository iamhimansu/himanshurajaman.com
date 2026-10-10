package contact_test

import (
	"context"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/contact"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

func TestSMTPMailer_FailsClosedWithoutSTARTTLS(t *testing.T) {
	// A server that does NOT advertise STARTTLS.
	srv := testutil.NewFakeSMTPServer(t, false)

	m := contact.NewSMTPMailer(srv.Host(), srv.Port(), "user", "pass")
	err := m.Send(context.Background(), contact.Message{
		From: "from@example.com", To: "to@example.com", Subject: "s", Body: "b",
	})
	if err == nil {
		t.Fatal("expected Send to fail closed when STARTTLS is unavailable")
	}
	if srv.ReachedAUTH() {
		t.Error("credentials must not be sent without STARTTLS (AUTH was reached)")
	}
	if srv.ReachedDATA() {
		t.Error("message must not be transmitted without STARTTLS (DATA was reached)")
	}
}
