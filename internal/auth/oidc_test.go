package auth_test

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/oauth2"

	"github.com/iamhimansu/himanshurajaman.com/internal/auth"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

const (
	testClientID   = "test-client-id"
	testAdminEmail = "admin@example.com"
	testNonce      = "nonce-abc"
)

// newAuthenticator wires an Authenticator to the mock OIDC issuer via the
// test-only WithIssuer seam.
func newAuthenticator(t *testing.T, oidc *testutil.OIDCServer) *auth.Authenticator {
	t.Helper()
	return auth.NewAuthenticator(testClientID, "secret", "http://app.test/auth/callback", testAdminEmail,
		auth.WithIssuer(oidc.IssuerURL()))
}

// tokenWith builds an *oauth2.Token carrying a signed id_token for claims.
func tokenWith(idToken string) *oauth2.Token {
	return (&oauth2.Token{}).WithExtra(map[string]any{"id_token": idToken})
}

func baseClaims() map[string]any {
	return map[string]any{
		"aud":            testClientID,
		"sub":            "subject-123",
		"email":          testAdminEmail,
		"email_verified": true,
		"nonce":          testNonce,
	}
}

func TestVerifyLogin_AcceptsAdmin(t *testing.T) {
	oidc := testutil.NewOIDCServer(t)
	a := newAuthenticator(t, oidc)

	tok := tokenWith(oidc.SignIDToken(baseClaims()))
	email, err := a.VerifyLogin(context.Background(), tok, testNonce)
	if err != nil {
		t.Fatalf("VerifyLogin: %v", err)
	}
	if email != testAdminEmail {
		t.Errorf("email = %q, want %q", email, testAdminEmail)
	}
}

func TestVerifyLogin_RejectsNonAdmin(t *testing.T) {
	oidc := testutil.NewOIDCServer(t)
	a := newAuthenticator(t, oidc)

	claims := baseClaims()
	claims["email"] = "someone-else@example.com"
	tok := tokenWith(oidc.SignIDToken(claims))

	if _, err := a.VerifyLogin(context.Background(), tok, testNonce); !errors.Is(err, auth.ErrLoginRejected) {
		t.Fatalf("VerifyLogin(non-admin) = %v, want ErrLoginRejected", err)
	}
}

func TestVerifyLogin_RejectsUnverified(t *testing.T) {
	oidc := testutil.NewOIDCServer(t)
	a := newAuthenticator(t, oidc)

	claims := baseClaims()
	claims["email_verified"] = false
	tok := tokenWith(oidc.SignIDToken(claims))

	if _, err := a.VerifyLogin(context.Background(), tok, testNonce); !errors.Is(err, auth.ErrLoginRejected) {
		t.Fatalf("VerifyLogin(unverified) = %v, want ErrLoginRejected", err)
	}
}

func TestVerifyLogin_NonceMismatch(t *testing.T) {
	oidc := testutil.NewOIDCServer(t)
	a := newAuthenticator(t, oidc)

	tok := tokenWith(oidc.SignIDToken(baseClaims()))
	// The token carries testNonce; a different expected nonce must be rejected.
	_, err := a.VerifyLogin(context.Background(), tok, "a-different-nonce")
	if err == nil {
		t.Fatal("expected nonce-mismatch error")
	}
	if errors.Is(err, auth.ErrLoginRejected) {
		t.Fatalf("nonce mismatch should be a generic verification error, got ErrLoginRejected")
	}
}

func TestVerifyLogin_BadSignature(t *testing.T) {
	oidc := testutil.NewOIDCServer(t)
	a := newAuthenticator(t, oidc)

	// Signed by a key not published in the JWKS -> signature verification fails.
	tok := tokenWith(oidc.SignIDTokenUnknownKey(t, baseClaims()))
	if _, err := a.VerifyLogin(context.Background(), tok, testNonce); err == nil {
		t.Fatal("expected a signature verification error")
	}
}
