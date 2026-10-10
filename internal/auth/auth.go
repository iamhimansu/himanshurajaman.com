package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const googleIssuer = "https://accounts.google.com"

// DefaultNext is the post-login destination when no valid next is supplied.
const DefaultNext = "/admin"

// Sentinels. ErrAuthUnavailable means Google discovery/JWKS could not be
// reached (handlers emit 503 "auth temporarily unavailable"); a later request
// retries. ErrLoginRejected means the identity was valid but not allowlisted or
// unverified (handlers redirect with error=forbidden, without logging the
// email).
var (
	ErrAuthUnavailable = errors.New("auth: provider temporarily unavailable")
	ErrLoginRejected   = errors.New("auth: login rejected")
)

// Authenticator wraps the Google OIDC provider/verifier and the oauth2 config.
// The provider is created lazily on first use (discovery is a network call) and
// cached only on success, so a cold boot never blocks on Google and a transient
// discovery failure is retried on the next request.
type Authenticator struct {
	clientID     string
	clientSecret string
	redirectURL  string
	adminEmail   string
	// issuer is the OIDC issuer URL discovery runs against. It defaults to
	// googleIssuer; the only reason it is overridable is so tests can point the
	// authenticator at a hermetic mock issuer (see WithIssuer). Production never
	// sets it, so behavior is unchanged.
	issuer string

	mu       sync.Mutex
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauthCfg *oauth2.Config
}

// Option customizes an Authenticator at construction.
type Option func(*Authenticator)

// WithIssuer overrides the OIDC issuer URL. TEST-ONLY: it exists so unit tests
// and server tests can discover against a mock issuer (httptest JWKS + signed
// ID token) instead of Google. Omitting it (as production does) keeps the
// hardcoded googleIssuer, so this option changes no production behavior.
func WithIssuer(url string) Option {
	return func(a *Authenticator) { a.issuer = url }
}

// NewAuthenticator builds an Authenticator from the resolved admin config. It
// performs no network I/O; the provider is discovered lazily.
func NewAuthenticator(clientID, clientSecret, redirectURL, adminEmail string, opts ...Option) *Authenticator {
	a := &Authenticator{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
		adminEmail:   adminEmail,
		issuer:       googleIssuer,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// ensure lazily discovers the provider and builds the verifier + oauth2 config.
// On a discovery failure it returns ErrAuthUnavailable and caches nothing, so
// the next call retries.
func (a *Authenticator) ensure(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.provider != nil {
		return nil
	}
	provider, err := oidc.NewProvider(ctx, a.issuer)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthUnavailable, err)
	}
	a.provider = provider
	a.verifier = provider.Verifier(&oidc.Config{ClientID: a.clientID})
	a.oauthCfg = &oauth2.Config{
		ClientID:     a.clientID,
		ClientSecret: a.clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  a.redirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "email"},
	}
	return nil
}

// AuthCodeURL builds the Google authorization URL with the OAuth state, OIDC
// nonce, and PKCE S256 challenge for verifier. It forces an account chooser.
func (a *Authenticator) AuthCodeURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	if err := a.ensure(ctx); err != nil {
		return "", err
	}
	return a.oauthCfg.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	), nil
}

// Exchange swaps the authorization code for a token, sending the PKCE verifier.
func (a *Authenticator) Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	if err := a.ensure(ctx); err != nil {
		return nil, err
	}
	return a.oauthCfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
}

// VerifyLogin verifies the ID token in tok, checks the nonce (constant-time),
// requires email_verified and an allowlisted admin email, and returns the
// canonical admin email on success. A valid-but-unauthorized identity returns
// ErrLoginRejected; any other verification failure returns a generic error.
func (a *Authenticator) VerifyLogin(ctx context.Context, tok *oauth2.Token, expectedNonce string) (string, error) {
	if err := a.ensure(ctx); err != nil {
		return "", err
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return "", errors.New("auth: id_token missing from token response")
	}
	idTok, err := a.verifier.Verify(ctx, rawID)
	if err != nil {
		return "", fmt.Errorf("auth: verify id token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idTok.Nonce), []byte(expectedNonce)) != 1 {
		return "", errors.New("auth: nonce mismatch")
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idTok.Claims(&claims); err != nil {
		return "", fmt.Errorf("auth: decode claims: %w", err)
	}
	if !claims.EmailVerified || !strings.EqualFold(claims.Email, a.adminEmail) {
		return "", ErrLoginRejected
	}
	// Canonical admin email (never echo the raw claim).
	return a.adminEmail, nil
}

// ValidateNext returns next if it is a safe local relative path, else
// DefaultNext ("/admin"). A safe path starts with a single "/", is not a
// scheme-relative ("//") or backslash ("/\\") URL, parses with empty Scheme and
// Host, and does not target the "/auth/" area.
func ValidateNext(next string) string {
	if next == "" {
		return DefaultNext
	}
	if !strings.HasPrefix(next, "/") {
		return DefaultNext
	}
	if strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return DefaultNext
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return DefaultNext
	}
	if next == "/auth" || strings.HasPrefix(next, "/auth/") {
		return DefaultNext
	}
	return next
}
