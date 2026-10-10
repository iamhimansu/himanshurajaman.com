// Package github is a tiny client for the two GitHub REST calls Phase 2 needs:
// resolving a ref to a commit SHA and downloading the repo tarball. The write
// path (Contents API with optimistic concurrency) arrives in Phase 4.
package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultAPIBase = "https://api.github.com"
	apiVersion     = "2022-11-28"
	userAgent      = "himanshurajaman.com-cms"
)

// Client talks to the GitHub REST API with a bearer token.
type Client struct {
	token   string
	apiBase string
	http    *http.Client
}

// Option customizes a Client (used by tests to point at an httptest server).
type Option func(*Client)

// WithAPIBase overrides the API base URL (default https://api.github.com).
func WithAPIBase(base string) Option { return func(c *Client) { c.apiBase = base } }

// WithHTTPClient overrides the underlying *http.Client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// NewClient builds a client. The default *http.Client keeps Go's standard
// redirect policy on purpose: on a cross-host redirect (api.github.com ->
// codeload.github.com) Go strips the Authorization header, which is exactly
// what we want — the codeload Location URL carries its own short-lived token,
// so we must NOT re-attach our PAT to that host.
func NewClient(token string, opts ...Option) *Client {
	c := &Client{
		token:   token,
		apiBase: defaultAPIBase,
		http:    &http.Client{Timeout: 90 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ResolveCommitSHA returns the full 40-char commit SHA a ref points at. It uses
// the commits endpoint with the ".sha" media type, which returns the SHA as a
// plain-text body — cheaper and more reliable than parsing the archive's
// top-level directory name (that only carries a short SHA).
func (c *Client) ResolveCommitSHA(ctx context.Context, owner, repo, ref string) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/commits/%s", c.apiBase, owner, repo, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	c.setCommonHeaders(req)
	req.Header.Set("Accept", "application/vnd.github.sha")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve sha: github returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	sha := strings.TrimSpace(string(body))
	if sha == "" {
		return "", fmt.Errorf("resolve sha: empty response")
	}
	return sha, nil
}

// Tarball fetches the gzipped tarball of a ref. The caller owns the returned
// ReadCloser and must close it. The initial api.github.com request carries the
// bearer token; the 302 to codeload is followed automatically by the client.
func (c *Client) Tarball(ctx context.Context, owner, repo, ref string) (io.ReadCloser, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", c.apiBase, owner, repo, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.setCommonHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		return nil, fmt.Errorf("tarball: github returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

func (c *Client) setCommonHeaders(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)
}
