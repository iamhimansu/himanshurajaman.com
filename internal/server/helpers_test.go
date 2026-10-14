package server_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/iamhimansu/himanshurajaman.com/internal/admin"
	"github.com/iamhimansu/himanshurajaman.com/internal/ai"
	"github.com/iamhimansu/himanshurajaman.com/internal/auth"
	"github.com/iamhimansu/himanshurajaman.com/internal/contact"
	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/github"
	"github.com/iamhimansu/himanshurajaman.com/internal/ingest"
	"github.com/iamhimansu/himanshurajaman.com/internal/server"
	"github.com/iamhimansu/himanshurajaman.com/internal/source"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

const testSessionSecret = "0123456789abcdef0123456789abcdef"

// indexShell is a minimal SPA shell with a splittable <head> so the server can
// inject per-request meta (splitShell needs </head> and strips the static
// title/description).
const indexShell = `<!doctype html><html><head><title>Shell</title>` +
	`<meta name="description" content="shell description"></head>` +
	`<body><div id="root"></div></body></html>`

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// options selects which optional surfaces the test server exposes.
type options struct {
	adminEnabled  bool
	writesEnabled bool
	contact       bool
	webhookSecret string
	notReady      bool

	// contentDir overrides the ingested content tree. When nil the default
	// fixture tree (testutil.WriteContentDir) is used.
	contentDir *testutil.ContentDir

	// AI drafting. When aiEnabled, the server gets a Groq client pointed at
	// aiBaseURL (an httptest mock). aiModels/aiMaxModels default to a 1-model
	// allowlist and a cap of 4 when left zero.
	aiEnabled   bool
	aiModels    []string
	aiMaxModels int
	aiBaseURL   string
}

// fakeMailer records whether/what the contact service tried to send.
type fakeMailer struct {
	called int
	last   contact.Message
}

func (f *fakeMailer) Send(_ context.Context, msg contact.Message) error {
	f.called++
	f.last = msg
	return nil
}

// harness bundles a built server with the collaborators tests reach into.
type harness struct {
	t      *testing.T
	srv    *server.Server
	sealer *auth.Sealer
	store  *store.Store
	mailer *fakeMailer
	gh     *testutil.GitHubMock // non-nil only when writesEnabled
}

func newServer(t *testing.T, opt options) *harness {
	t.Helper()
	log := discardLogger()

	st, err := store.Open(t.TempDir() + "/cache.sqlite")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	imgs, err := ingest.NewImageManager(t.TempDir())
	if err != nil {
		t.Fatalf("image manager: %v", err)
	}
	var dir string
	if opt.contentDir != nil {
		dir = opt.contentDir.Write(t)
	} else {
		dir = testutil.WriteContentDir(t)
	}
	ing := ingest.New(source.NewLocal(dir), st, content.NewRenderer(), imgs, log)
	if _, err := ing.Run(context.Background()); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	h := &harness{t: t, store: st}

	var (
		sealer     *auth.Sealer
		authr      *auth.Authenticator
		adminSvc   *admin.Service
		contactSvc *contact.Service
	)
	if opt.adminEnabled {
		sealer, err = auth.NewSealer([]byte(testSessionSecret), false)
		if err != nil {
			t.Fatalf("new sealer: %v", err)
		}
		h.sealer = sealer
		authr = auth.NewAuthenticator("client-id", "client-secret", "http://app.test/auth/callback", "admin@example.com")

		var ghc *github.Client
		if opt.writesEnabled {
			m := testutil.NewGitHubMock(t)
			h.gh = m
			ghc = github.NewClient("tok", github.WithAPIBase(m.APIBase()))
		}
		adminSvc = admin.New(st, ghc, content.NewRenderer(), ing, imgs,
			admin.Config{RepoOwner: "octocat", RepoName: "content", ContentRef: "main"}, log)
	}
	if opt.contact {
		h.mailer = &fakeMailer{}
		contactSvc = contact.NewService(h.mailer, "owner@example.com", "noreply@example.com", log)
	}

	var aiClient *ai.Client
	aiModels := opt.aiModels
	aiMax := opt.aiMaxModels
	if opt.aiEnabled {
		if len(aiModels) == 0 {
			aiModels = []string{"openai/gpt-oss-120b"}
		}
		if aiMax == 0 {
			aiMax = 4
		}
		aiClient = ai.NewClient("test-key", ai.WithBaseURL(opt.aiBaseURL))
	}

	dist := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte(indexShell)},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('app')")},
	}

	h.srv = server.New(server.Deps{
		Store:         st,
		Images:        imgs,
		Index:         []byte(indexShell),
		Dist:          dist,
		Log:           log,
		ConfigBaseURL: "",
		Sealer:        sealer,
		Auth:          authr,
		Admin:         adminSvc,
		AdminEnabled:  opt.adminEnabled,
		WritesEnabled: opt.writesEnabled,
		Ingester:      ing,
		WebhookSecret: opt.webhookSecret,
		Contact:       contactSvc,
		AI:            aiClient,
		AIEnabled:     opt.aiEnabled,
		AIModels:      aiModels,
		AIMaxModels:   aiMax,
	})
	if adminSvc != nil {
		adminSvc.SetMarkReady(h.srv.MarkReady)
	}
	if !opt.notReady {
		h.srv.MarkReady("test-commit")
	}
	return h
}

// serve runs a request through the server and returns the recorder.
func (h *harness) serve(req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.srv.ServeHTTP(rr, req)
	return rr
}

// sessionCookie mints a valid session cookie for email.
func (h *harness) sessionCookie(email string) *http.Cookie {
	h.t.Helper()
	tok, err := h.sealer.SealSession(email, time.Now())
	if err != nil {
		h.t.Fatalf("seal session: %v", err)
	}
	return &http.Cookie{Name: h.sealer.SessionCookieName(), Value: tok}
}

// expiredSessionCookie mints a session cookie whose 12h TTL has already lapsed.
func (h *harness) expiredSessionCookie(email string) *http.Cookie {
	h.t.Helper()
	tok, err := h.sealer.SealSession(email, time.Now().Add(-13*time.Hour))
	if err != nil {
		h.t.Fatalf("seal session: %v", err)
	}
	return &http.Cookie{Name: h.sealer.SessionCookieName(), Value: tok}
}

// csrfPair returns a matching CSRF cookie and header token.
func (h *harness) csrfPair() (*http.Cookie, string) {
	const token = "csrf-token-value"
	return &http.Cookie{Name: h.sealer.CSRFCookieName(), Value: token}, token
}

// oauthCookie seals an OAuth-temp cookie carrying state.
func (h *harness) oauthCookie(state string) *http.Cookie {
	h.t.Helper()
	tok, err := h.sealer.SealOAuth(auth.OAuthState{State: state, Nonce: "n", Verifier: "v", Next: "/admin"}, time.Now())
	if err != nil {
		h.t.Fatalf("seal oauth: %v", err)
	}
	return &http.Cookie{Name: h.sealer.OAuthCookieName(), Value: tok}
}

// hasSessionCookie reports whether the response set a session cookie.
func (h *harness) hasSessionCookie(rr *httptest.ResponseRecorder) bool {
	name := h.sealer.SessionCookieName()
	for _, c := range rr.Result().Cookies() {
		if c.Name == name && c.Value != "" && c.MaxAge >= 0 {
			return true
		}
	}
	return false
}
