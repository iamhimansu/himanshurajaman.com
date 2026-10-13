// Command server boots the portfolio/blog backend: it opens the disposable
// SQLite cache, binds the HTTP port immediately, and ingests the content repo
// in the background so /healthz answers while warming.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/iamhimansu/himanshurajaman.com/internal/admin"
	"github.com/iamhimansu/himanshurajaman.com/internal/ai"
	"github.com/iamhimansu/himanshurajaman.com/internal/auth"
	"github.com/iamhimansu/himanshurajaman.com/internal/config"
	"github.com/iamhimansu/himanshurajaman.com/internal/contact"
	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/github"
	"github.com/iamhimansu/himanshurajaman.com/internal/ingest"
	"github.com/iamhimansu/himanshurajaman.com/internal/server"
	"github.com/iamhimansu/himanshurajaman.com/internal/source"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
	"github.com/iamhimansu/himanshurajaman.com/web"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("fatal", "error", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	dbPath := store.DefaultPath(cfg.SQLitePath)
	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()
	log.Info("sqlite ready", "path", dbPath, "schemaVersion", store.SchemaVersion)

	src, err := buildSource(cfg)
	if err != nil {
		return err
	}

	imagesDir := filepath.Join(filepath.Dir(dbPath), fmt.Sprintf("portfolio-images-v%d", store.SchemaVersion))
	imgs, err := ingest.NewImageManager(imagesDir)
	if err != nil {
		return fmt.Errorf("init images: %w", err)
	}

	ing := ingest.New(src, st, content.NewRenderer(), imgs, log)

	index, err := web.Index()
	if err != nil {
		return fmt.Errorf("read embedded index.html: %w", err)
	}
	dist, err := web.DistFS()
	if err != nil {
		return fmt.Errorf("open embedded dist: %w", err)
	}

	// Build the admin surface when configured. Auth (login/session) exists
	// whenever admin is enabled; the GitHub write client (and thus GitHub
	// writes) only when the content source is GitHub.
	var (
		sealer   *auth.Sealer
		authr    *auth.Authenticator
		adminSvc *admin.Service
	)
	if cfg.AdminEnabled() {
		sealer, err = auth.NewSealer([]byte(cfg.SessionSecret), cfg.CookieSecure)
		if err != nil {
			return fmt.Errorf("init auth: %w", err)
		}
		authr = auth.NewAuthenticator(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL, cfg.AdminEmail)

		var writeClient *github.Client
		if cfg.WritesEnabled() {
			writeClient = github.NewClient(cfg.GitHubToken)
		}
		adminSvc = admin.New(st, writeClient, content.NewRenderer(), ing, imgs,
			admin.Config{RepoOwner: cfg.RepoOwner, RepoName: cfg.RepoName, ContentRef: cfg.ContentRef}, log)
	}

	// Build the contact-form email service when configured. Fail-safe like the
	// admin surface: enabled only when all SMTP_* credentials are present (see
	// cfg.ContactEnabled). To falls back to ADMIN_EMAIL, From to SMTP_USERNAME.
	var contactSvc *contact.Service
	if cfg.ContactEnabled() {
		to := cfg.ContactTo
		if to == "" {
			to = cfg.AdminEmail
		}
		from := cfg.ContactFrom
		if from == "" {
			from = cfg.SMTPUsername
		}
		// Prefer Resend (HTTP API) when configured — it works on hosts that block
		// outbound SMTP (e.g. Render free). Fall back to SMTP otherwise.
		var mailer contact.Mailer
		if cfg.ResendEnabled() {
			mailer = contact.NewResendMailer(cfg.ResendAPIKey)
			if from == "" {
				log.Warn("contact (resend) enabled but no sender: set CONTACT_FROM to an address on a Resend-verified domain")
			}
		} else {
			mailer = contact.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword)
		}
		contactSvc = contact.NewService(mailer, to, from, log)
		if to == "" {
			log.Warn("contact enabled but no recipient: set CONTACT_TO or ADMIN_EMAIL")
		}
	}

	// Build the Groq AI-drafting client when configured (GROQ_API_KEY set). The
	// key is passed only to the client (server-side); GROQ_BASE_URL overrides the
	// API base for tests/self-hosting.
	var aiClient *ai.Client
	if cfg.AIEnabled() {
		var opts []ai.Option
		if cfg.GroqBaseURL != "" {
			opts = append(opts, ai.WithBaseURL(cfg.GroqBaseURL))
		}
		aiClient = ai.NewClient(cfg.GroqAPIKey, opts...)
	}

	srv := server.New(server.Deps{
		Store:         st,
		Images:        imgs,
		Index:         index,
		Dist:          dist,
		Log:           log,
		ConfigBaseURL: cfg.BaseURL,
		Sealer:        sealer,
		Auth:          authr,
		Admin:         adminSvc,
		AdminEnabled:  cfg.AdminEnabled(),
		WritesEnabled: cfg.WritesEnabled(),
		Ingester:      ing,
		WebhookSecret: cfg.GitHubWebhookSecret,
		Contact:       contactSvc,
		AI:            aiClient,
		AIEnabled:     cfg.AIEnabled(),
		AIModels:      cfg.GroqModels,
		AIMaxModels:   cfg.GroqMaxModels,
	})

	// [DR5] Late-bind the readiness callback into the admin service now that the
	// server exists, avoiding a construction cycle (resync flips readiness).
	if adminSvc != nil {
		adminSvc.SetMarkReady(srv.MarkReady)
	}

	// One startup line, no secrets.
	if cfg.AdminEnabled() {
		log.Info("admin configured",
			"enabled", true,
			"writes", cfg.WritesEnabled(),
			"cookieSecure", cfg.CookieSecure,
			"redirect", cfg.GoogleRedirectURL)
	} else {
		log.Info("admin configured",
			"enabled", false,
			"hint", "set GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, ADMIN_EMAIL, SESSION_SECRET to enable")
	}

	// One line reporting whether the push-to-resync webhook is enabled; never
	// log the secret value itself.
	log.Info("webhook configured", "enabled", cfg.GitHubWebhookSecret != "")

	// One line reporting whether the public contact form is enabled; mirrors the
	// admin-disabled line and never logs SMTP credentials.
	if cfg.ContactEnabled() {
		transport := "smtp"
		if cfg.ResendEnabled() {
			transport = "resend"
		}
		log.Info("contact configured", "enabled", true, "transport", transport)
	} else {
		log.Info("contact configured",
			"enabled", false,
			"hint", "set RESEND_API_KEY (recommended; works on Render free) or SMTP_HOST+SMTP_USERNAME+SMTP_PASSWORD to enable the contact form")
	}

	// One line reporting whether admin AI drafting is enabled; the Groq API key
	// is NEVER logged (only the non-secret model allowlist + cap).
	if cfg.AIEnabled() {
		log.Info("ai configured", "enabled", true, "models", cfg.GroqModels, "maxPerRequest", cfg.GroqMaxModels)
	} else {
		log.Info("ai configured", "enabled", false, "hint", "set GROQ_API_KEY to enable admin AI drafting")
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Kick off the first ingest in the background so the port binds and
	// /healthz answers immediately. Readiness flips true only on success.
	go bootstrapIngest(ctx, ing, srv, src.Describe(), log)

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", httpSrv.Addr, "source", src.Describe())
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func buildSource(cfg *config.Config) (source.Source, error) {
	switch cfg.Source {
	case config.SourceGitHub:
		client := github.NewClient(cfg.GitHubToken)
		return source.NewGitHub(client, cfg.RepoOwner, cfg.RepoName, cfg.ContentRef), nil
	case config.SourceLocal:
		return source.NewLocal(cfg.LocalDir), nil
	default:
		return nil, fmt.Errorf("unknown content source %q", cfg.Source)
	}
}

// bootstrapIngest retries the first ingest with capped exponential backoff
// until it succeeds or the process is shutting down. Without a successful
// ingest there is no cache, so readiness stays false and public routes keep
// returning 503 — retrying lets a transient GitHub error recover on its own.
func bootstrapIngest(ctx context.Context, ing *ingest.Ingester, srv *server.Server, sourceDesc string, log *slog.Logger) {
	backoff := 2 * time.Second
	const maxBackoff = 60 * time.Second
	for {
		res, err := ing.Run(ctx)
		if err == nil {
			srv.MarkReady(res.CommitSHA)
			log.Info("ingest complete",
				"commit", res.CommitSHA, "ref", res.Ref,
				"published", res.Published, "drafts", res.Drafts,
				"skipped", res.Skipped, "images", res.Images,
				"source", sourceDesc)
			return
		}
		if ctx.Err() != nil {
			return
		}
		log.Error("ingest failed; retrying", "error", err.Error(), "retryIn", backoff.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
