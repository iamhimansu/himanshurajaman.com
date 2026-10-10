package github_test

import (
	"context"
	"io"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/github"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

func TestResolveCommitSHA(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	m := testutil.NewGitHubMock(t)
	m.OnCommitSHA("main", sha)
	c := github.NewClient("tok", github.WithAPIBase(m.APIBase()))

	t.Run("ok", func(t *testing.T) {
		got, err := c.ResolveCommitSHA(context.Background(), m.Owner, m.Repo, "main")
		if err != nil {
			t.Fatalf("ResolveCommitSHA: %v", err)
		}
		if got != sha {
			t.Errorf("sha = %q, want %q", got, sha)
		}
	})

	t.Run("non-200 is an error", func(t *testing.T) {
		// "missing" ref has no stub -> mock returns 500.
		if _, err := c.ResolveCommitSHA(context.Background(), m.Owner, m.Repo, "missing"); err == nil {
			t.Fatal("expected error for non-200 response")
		}
	})
}

func TestTarball_CrossHostRedirectStripsAuth(t *testing.T) {
	payload := testutil.MakeTarGz("octocat-content-abc", map[string][]byte{"posts/x.json": []byte("{}")})
	tr := testutil.NewTarballRedirect(t, payload)

	c := github.NewClient("secret-token",
		github.WithAPIBase(tr.APIBase),
		github.WithHTTPClient(tr.Client),
	)
	rc, err := c.Tarball(context.Background(), "octocat", "content", "main")
	if err != nil {
		t.Fatalf("Tarball: %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read tarball body: %v", err)
	}

	if !tr.CodeloadReached() {
		t.Fatal("redirect to codeload host was not followed")
	}
	if string(body) != string(payload) {
		t.Errorf("tarball body mismatch: got %d bytes, want %d", len(body), len(payload))
	}
	// The initial api request carries the PAT; the cross-host codeload hop must NOT.
	if tr.APIAuthorization() != "Bearer secret-token" {
		t.Errorf("api request lost its Authorization header: %q", tr.APIAuthorization())
	}
	if tr.CodeloadAuthorization() != "" {
		t.Errorf("codeload hop should NOT carry Authorization, got %q", tr.CodeloadAuthorization())
	}
}
