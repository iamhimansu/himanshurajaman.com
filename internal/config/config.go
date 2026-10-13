// Package config loads and validates all runtime configuration from the
// environment. There are no config files: Render injects env vars, and failing
// fast on a missing/invalid value is clearer than booting into a broken state.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SourceKind selects where ingest reads content from.
type SourceKind string

const (
	// SourceGitHub fetches the content repo as a tarball over the GitHub API.
	SourceGitHub SourceKind = "github"
	// SourceLocal reads the same logical content from a local directory. It lets
	// the backend run with no GitHub credentials (development / verification).
	SourceLocal SourceKind = "local"
)

// Config is the fully validated configuration for the process.
type Config struct {
	Source      SourceKind
	GitHubToken string // sensitive: never log this
	ContentRepo string // "owner/repo"
	RepoOwner   string
	RepoName    string
	ContentRef  string
	LocalDir    string
	Port        string
	SQLitePath  string // optional override of the temp-file path

	// Admin / CMS (optional, all-or-nothing — see AdminEnabled). These are
	// derived from {GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, ADMIN_EMAIL,
	// SESSION_SECRET}: none set → admin disabled; all set → admin enabled and
	// validated; partial → fail-fast.
	GoogleClientID     string
	GoogleClientSecret string // sensitive: never log this
	GoogleRedirectURL  string
	AdminEmail         string
	SessionSecret      string // sensitive: never log this
	CookieSecure       bool
	BaseURL            string

	// GitHubWebhookSecret enables the POST /webhook/github push-to-resync
	// endpoint when set. Optional and independent of admin / CONTENT_SOURCE;
	// empty means the endpoint is disabled (404). sensitive: never log this.
	GitHubWebhookSecret string

	// Contact form (optional, fail-safe — see ContactEnabled). The public
	// POST /api/contact endpoint and the home-page form are enabled when EITHER
	// RESEND_API_KEY (HTTP email API — works where outbound SMTP is blocked, e.g.
	// Render free) OR a full SMTP set {SMTP_HOST, SMTP_USERNAME, SMTP_PASSWORD} is
	// present; otherwise the endpoint returns 503 and the SPA shows a mailto
	// fallback. Resend is preferred when both are set.
	ResendAPIKey string // sensitive: never log this (Resend HTTP API, option A)
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string // sensitive: never log this
	ContactTo    string // recipient; falls back to AdminEmail
	ContactFrom  string // envelope/From header; falls back to SMTPUsername

	// AI drafting (optional, fail-safe — see AIEnabled). The admin-only
	// POST /api/admin/assist + GET /api/admin/models endpoints are enabled when
	// GROQ_API_KEY is set; otherwise they return 503 and the admin UI hides the
	// AI panel. The key is sensitive and SERVER-SIDE ONLY (never sent to the
	// browser or logged). GroqModels is the curated allowlist of selectable chat
	// models (don't offer non-chat models like whisper/tts); GroqMaxModels caps
	// how many models one /assist request may fan out to (cost/latency);
	// GroqBaseURL overrides the API base and is used only by tests.
	GroqAPIKey    string // sensitive: never log this
	GroqModels    []string
	GroqMaxModels int
	GroqBaseURL   string

	adminEnabled bool
}

// defaultGroqModels is the current chat-model allowlist shipped by default.
// Groq is OpenAI-compatible; these are the gpt-oss chat models (the old llama-*
// models were decommissioned). Override with GROQ_MODELS; the operator confirms
// what is live in their Groq console.
const defaultGroqModels = "openai/gpt-oss-120b,openai/gpt-oss-20b"

// AdminEnabled reports whether the admin/CMS surface is configured (all of the
// admin env vars were present and validated at Load). When false the process
// boots public-only.
func (c *Config) AdminEnabled() bool { return c.adminEnabled }

// WritesEnabled reports whether GitHub-backed admin writes are possible: admin
// must be enabled AND the content source must be GitHub (a write client only
// exists in github mode).
func (c *Config) WritesEnabled() bool { return c.adminEnabled && c.Source == SourceGitHub }

// Load reads configuration from the environment and validates the subset
// required by the selected CONTENT_SOURCE. It returns an error (never calls
// os.Exit) so the caller controls process exit.
func Load() (*Config, error) {
	c := &Config{
		Source:      SourceKind(getEnv("CONTENT_SOURCE", "github")),
		GitHubToken: os.Getenv("GITHUB_TOKEN"),
		ContentRepo: os.Getenv("CONTENT_REPO"),
		ContentRef:  getEnv("CONTENT_REF", "main"),
		LocalDir:    os.Getenv("CONTENT_LOCAL_DIR"),
		Port:        getEnv("PORT", "8080"),
		SQLitePath:  os.Getenv("SQLITE_PATH"),

		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		AdminEmail:         os.Getenv("ADMIN_EMAIL"),
		SessionSecret:      os.Getenv("SESSION_SECRET"),
		BaseURL:            os.Getenv("BASE_URL"),

		GitHubWebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),

		ResendAPIKey: os.Getenv("RESEND_API_KEY"),
		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPPort:     getEnv("SMTP_PORT", "587"),
		SMTPUsername: os.Getenv("SMTP_USERNAME"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		ContactTo:    os.Getenv("CONTACT_TO"),
		ContactFrom:  os.Getenv("CONTACT_FROM"),

		GroqAPIKey:  os.Getenv("GROQ_API_KEY"),
		GroqBaseURL: os.Getenv("GROQ_BASE_URL"),
	}

	// Curated chat-model allowlist. An empty/whitespace GROQ_MODELS falls back to
	// the shipped default so the models endpoint always has a usable set.
	c.GroqModels = splitCSV(getEnv("GROQ_MODELS", defaultGroqModels))
	if len(c.GroqModels) == 0 {
		c.GroqModels = splitCSV(defaultGroqModels)
	}

	switch c.Source {
	case SourceGitHub:
		if c.GitHubToken == "" {
			return nil, fmt.Errorf("GITHUB_TOKEN is required when CONTENT_SOURCE=github")
		}
		if c.ContentRepo == "" {
			return nil, fmt.Errorf(`CONTENT_REPO is required when CONTENT_SOURCE=github (expected "owner/repo")`)
		}
		owner, name, ok := strings.Cut(c.ContentRepo, "/")
		if !ok || owner == "" || name == "" {
			return nil, fmt.Errorf(`CONTENT_REPO must be "owner/repo", got %q`, c.ContentRepo)
		}
		c.RepoOwner, c.RepoName = owner, name
	case SourceLocal:
		if c.LocalDir == "" {
			return nil, fmt.Errorf("CONTENT_LOCAL_DIR is required when CONTENT_SOURCE=local")
		}
		info, err := os.Stat(c.LocalDir)
		if err != nil {
			return nil, fmt.Errorf("CONTENT_LOCAL_DIR %q: %w", c.LocalDir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("CONTENT_LOCAL_DIR %q is not a directory", c.LocalDir)
		}
	default:
		return nil, fmt.Errorf(`CONTENT_SOURCE must be "github" or "local", got %q`, c.Source)
	}

	if _, err := strconv.Atoi(c.Port); err != nil {
		return nil, fmt.Errorf("PORT must be numeric, got %q", c.Port)
	}

	if _, err := strconv.Atoi(c.SMTPPort); err != nil {
		return nil, fmt.Errorf("SMTP_PORT must be numeric, got %q", c.SMTPPort)
	}

	maxModels, err := strconv.Atoi(getEnv("GROQ_MAX_MODELS", "4"))
	if err != nil {
		return nil, fmt.Errorf("GROQ_MAX_MODELS must be numeric, got %q", os.Getenv("GROQ_MAX_MODELS"))
	}
	if maxModels < 1 {
		return nil, fmt.Errorf("GROQ_MAX_MODELS must be >= 1, got %d", maxModels)
	}
	c.GroqMaxModels = maxModels

	if err := c.loadAdmin(); err != nil {
		return nil, err
	}

	return c, nil
}

// ContactEnabled reports whether the public contact form is configured. Like
// the admin surface it is fail-safe: enabled when EITHER the Resend HTTP API
// (RESEND_API_KEY) OR a full SMTP credential set is present, so the endpoint
// and form stay off (503 / mailto fallback) until email delivery is configured.
func (c *Config) ContactEnabled() bool {
	return c.ResendEnabled() || (c.SMTPHost != "" && c.SMTPUsername != "" && c.SMTPPassword != "")
}

// ResendEnabled reports whether the Resend HTTP email transport is configured
// (RESEND_API_KEY set). Preferred over SMTP when both are present because it
// works on hosts that block outbound SMTP ports (e.g. Render's free tier).
func (c *Config) ResendEnabled() bool { return c.ResendAPIKey != "" }

// AIEnabled reports whether the admin AI-drafting surface is configured
// (GROQ_API_KEY set). Fail-safe like ContactEnabled: when false the
// GET /api/admin/models + POST /api/admin/assist endpoints return 503 and the
// admin UI hides the AI panel. The key stays server-side only.
func (c *Config) AIEnabled() bool { return c.GroqAPIKey != "" }

// loadAdmin applies the all-or-nothing admin enablement rule over
// {GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, ADMIN_EMAIL, SESSION_SECRET}:
// none set → disabled; all set → enabled + validated; partial → fail-fast
// naming the missing vars.
func (c *Config) loadAdmin() error {
	required := []struct {
		name  string
		value string
	}{
		{"GOOGLE_CLIENT_ID", c.GoogleClientID},
		{"GOOGLE_CLIENT_SECRET", c.GoogleClientSecret},
		{"ADMIN_EMAIL", c.AdminEmail},
		{"SESSION_SECRET", c.SessionSecret},
	}

	var set, missing []string
	for _, r := range required {
		if r.value != "" {
			set = append(set, r.name)
		} else {
			missing = append(missing, r.name)
		}
	}

	switch len(set) {
	case 0:
		// None set: admin disabled, boot public-only.
		c.adminEnabled = false
		return nil
	case len(required):
		// All set: enable + validate below.
	default:
		return fmt.Errorf("admin config is partially set: provide all of {GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, ADMIN_EMAIL, SESSION_SECRET} to enable admin, or none to disable it; missing: %s", strings.Join(missing, ", "))
	}

	if len(c.SessionSecret) < 32 {
		return fmt.Errorf("SESSION_SECRET must be at least 32 bytes, got %d", len(c.SessionSecret))
	}

	// Derive the OAuth redirect URL: explicit override, else BaseURL+/auth/callback.
	switch {
	case c.GoogleRedirectURL != "":
		// use as-is
	case c.BaseURL != "":
		c.GoogleRedirectURL = strings.TrimRight(c.BaseURL, "/") + "/auth/callback"
	default:
		return fmt.Errorf("admin enabled but no redirect URL: set GOOGLE_REDIRECT_URL or BASE_URL")
	}

	secure, err := parseBool(os.Getenv("COOKIE_SECURE"), false)
	if err != nil {
		return fmt.Errorf("COOKIE_SECURE must be true/false/1/0, got %q", os.Getenv("COOKIE_SECURE"))
	}
	c.CookieSecure = secure
	c.adminEnabled = true
	return nil
}

// parseBool accepts true/false/1/0 (case-insensitive); an empty string yields
// def.
func parseBool(s string, def bool) (bool, error) {
	if s == "" {
		return def, nil
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool %q", s)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// splitCSV splits a comma-separated list, trimming surrounding whitespace from
// each entry and dropping empty entries. Used for the GROQ_MODELS allowlist.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
