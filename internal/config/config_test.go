package config_test

import (
	"strings"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/config"
)

// clearEnv neutralizes every config-relevant variable so a test is independent
// of the ambient environment. t.Setenv restores the previous values on cleanup.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"CONTENT_SOURCE", "GITHUB_TOKEN", "CONTENT_REPO", "CONTENT_REF", "CONTENT_LOCAL_DIR",
		"PORT", "SQLITE_PATH", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOOGLE_REDIRECT_URL",
		"ADMIN_EMAIL", "SESSION_SECRET", "BASE_URL", "GITHUB_WEBHOOK_SECRET", "RESEND_API_KEY",
		"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "CONTACT_TO", "CONTACT_FROM",
		"COOKIE_SECURE",
	} {
		t.Setenv(k, "")
	}
}

// localSource points config at CONTENT_SOURCE=local with a valid temp dir so the
// source validation passes and the test can focus on another dimension.
func localSource(t *testing.T) {
	t.Helper()
	t.Setenv("CONTENT_SOURCE", "local")
	t.Setenv("CONTENT_LOCAL_DIR", t.TempDir())
}

const goodSecret = "0123456789abcdef0123456789abcdef" // 32 bytes

func setAdminAll(t *testing.T) {
	t.Helper()
	t.Setenv("GOOGLE_CLIENT_ID", "cid")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("ADMIN_EMAIL", "admin@example.com")
	t.Setenv("SESSION_SECRET", goodSecret)
	t.Setenv("BASE_URL", "https://example.test")
}

func TestLoad_AdminAllOrNothing(t *testing.T) {
	t.Run("none set disables admin", func(t *testing.T) {
		clearEnv(t)
		localSource(t)
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.AdminEnabled() {
			t.Error("expected admin disabled when no admin vars set")
		}
	})

	t.Run("all set enables admin", func(t *testing.T) {
		clearEnv(t)
		localSource(t)
		setAdminAll(t)
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.AdminEnabled() {
			t.Error("expected admin enabled when all admin vars set")
		}
		if cfg.GoogleRedirectURL != "https://example.test/auth/callback" {
			t.Errorf("redirect not derived from BASE_URL: %q", cfg.GoogleRedirectURL)
		}
	})

	t.Run("partial set fails naming missing vars", func(t *testing.T) {
		clearEnv(t)
		localSource(t)
		t.Setenv("GOOGLE_CLIENT_ID", "cid")
		_, err := config.Load()
		if err == nil {
			t.Fatal("expected error for partial admin config")
		}
		if !strings.Contains(err.Error(), "GOOGLE_CLIENT_SECRET") ||
			!strings.Contains(err.Error(), "SESSION_SECRET") {
			t.Errorf("error should name the missing vars, got: %v", err)
		}
	})
}

func TestLoad_SessionSecretMinLength(t *testing.T) {
	clearEnv(t)
	localSource(t)
	setAdminAll(t)
	t.Setenv("SESSION_SECRET", "tooshort")
	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatalf("expected SESSION_SECRET length error, got: %v", err)
	}
}

func TestLoad_SourceValidation(t *testing.T) {
	t.Run("github requires token", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("CONTENT_SOURCE", "github")
		t.Setenv("CONTENT_REPO", "owner/repo")
		if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
			t.Fatalf("expected GITHUB_TOKEN error, got %v", err)
		}
	})

	t.Run("github requires repo", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("CONTENT_SOURCE", "github")
		t.Setenv("GITHUB_TOKEN", "tok")
		if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "CONTENT_REPO") {
			t.Fatalf("expected CONTENT_REPO error, got %v", err)
		}
	})

	t.Run("github rejects malformed repo", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("CONTENT_SOURCE", "github")
		t.Setenv("GITHUB_TOKEN", "tok")
		t.Setenv("CONTENT_REPO", "noslash")
		if _, err := config.Load(); err == nil {
			t.Fatal("expected malformed CONTENT_REPO error")
		}
	})

	t.Run("github valid parses owner and repo", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("CONTENT_SOURCE", "github")
		t.Setenv("GITHUB_TOKEN", "tok")
		t.Setenv("CONTENT_REPO", "octocat/content")
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.RepoOwner != "octocat" || cfg.RepoName != "content" {
			t.Errorf("owner/repo not parsed: %q / %q", cfg.RepoOwner, cfg.RepoName)
		}
	})

	t.Run("local requires existing dir", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("CONTENT_SOURCE", "local")
		if _, err := config.Load(); err == nil {
			t.Fatal("expected error when CONTENT_LOCAL_DIR unset")
		}
		t.Setenv("CONTENT_LOCAL_DIR", "/no/such/dir/really")
		if _, err := config.Load(); err == nil {
			t.Fatal("expected error for missing dir")
		}
	})

	t.Run("unknown source rejected", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("CONTENT_SOURCE", "ftp")
		if _, err := config.Load(); err == nil {
			t.Fatal("expected error for unknown source")
		}
	})
}

func TestLoad_ContactEnabled_ResendOrSMTP(t *testing.T) {
	t.Run("neither configured", func(t *testing.T) {
		clearEnv(t)
		localSource(t)
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ContactEnabled() || cfg.ResendEnabled() {
			t.Error("contact should be disabled with no transport")
		}
	})

	t.Run("resend only", func(t *testing.T) {
		clearEnv(t)
		localSource(t)
		t.Setenv("RESEND_API_KEY", "re_123")
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.ContactEnabled() || !cfg.ResendEnabled() {
			t.Error("contact should be enabled via Resend")
		}
	})

	t.Run("full smtp set", func(t *testing.T) {
		clearEnv(t)
		localSource(t)
		t.Setenv("SMTP_HOST", "smtp.example.com")
		t.Setenv("SMTP_USERNAME", "user")
		t.Setenv("SMTP_PASSWORD", "pass")
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.ContactEnabled() || cfg.ResendEnabled() {
			t.Error("contact should be enabled via SMTP, Resend off")
		}
	})

	t.Run("partial smtp disabled", func(t *testing.T) {
		clearEnv(t)
		localSource(t)
		t.Setenv("SMTP_HOST", "smtp.example.com")
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ContactEnabled() {
			t.Error("partial SMTP set should keep contact disabled")
		}
	})
}

func TestWritesEnabled(t *testing.T) {
	t.Run("admin plus github enables writes", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("CONTENT_SOURCE", "github")
		t.Setenv("GITHUB_TOKEN", "tok")
		t.Setenv("CONTENT_REPO", "octocat/content")
		setAdminAll(t)
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.WritesEnabled() {
			t.Error("expected writes enabled for admin + github")
		}
	})

	t.Run("admin plus local disables writes", func(t *testing.T) {
		clearEnv(t)
		localSource(t)
		setAdminAll(t)
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.WritesEnabled() {
			t.Error("expected writes disabled for local source")
		}
	})

	t.Run("no admin disables writes", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("CONTENT_SOURCE", "github")
		t.Setenv("GITHUB_TOKEN", "tok")
		t.Setenv("CONTENT_REPO", "octocat/content")
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.WritesEnabled() {
			t.Error("expected writes disabled when admin off")
		}
	})
}

func TestLoad_PortValidation(t *testing.T) {
	t.Run("non-numeric PORT", func(t *testing.T) {
		clearEnv(t)
		localSource(t)
		t.Setenv("PORT", "notaport")
		if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "PORT") {
			t.Fatalf("expected PORT error, got %v", err)
		}
	})

	t.Run("non-numeric SMTP_PORT", func(t *testing.T) {
		clearEnv(t)
		localSource(t)
		t.Setenv("SMTP_PORT", "abc")
		if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "SMTP_PORT") {
			t.Fatalf("expected SMTP_PORT error, got %v", err)
		}
	})
}
