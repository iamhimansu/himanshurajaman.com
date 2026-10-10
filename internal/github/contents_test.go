package github_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/github"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

func TestGetContents(t *testing.T) {
	m := testutil.NewGitHubMock(t)
	m.OnGetContents("posts/x.json", testutil.GHContents([]byte("hello body"), "shaOK"))
	m.OnGetContents("posts/missing.json", testutil.GHStatus(404))
	m.OnGetContents("posts/boom.json", testutil.GHStatus(500))
	c := github.NewClient("tok", github.WithAPIBase(m.APIBase()))

	t.Run("ok decodes content and sha", func(t *testing.T) {
		data, sha, err := c.GetContents(context.Background(), m.Owner, m.Repo, "posts/x.json", "main")
		if err != nil {
			t.Fatalf("GetContents: %v", err)
		}
		if string(data) != "hello body" || sha != "shaOK" {
			t.Errorf("got data=%q sha=%q", data, sha)
		}
	})

	t.Run("404 maps to ErrNotFound", func(t *testing.T) {
		if _, _, err := c.GetContents(context.Background(), m.Owner, m.Repo, "posts/missing.json", "main"); !errors.Is(err, github.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("other status is a generic error", func(t *testing.T) {
		_, _, err := c.GetContents(context.Background(), m.Owner, m.Repo, "posts/boom.json", "main")
		if err == nil || errors.Is(err, github.ErrNotFound) {
			t.Fatalf("expected generic error, got %v", err)
		}
	})
}

func TestPutContents_StatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		resp       testutil.GHResponse
		sha        string // prevSHA passed in
		wantErr    error
		wantBlob   string
		wantNonNil bool // generic (non-sentinel) error expected
	}{
		{name: "create 201", resp: testutil.GHWrite(201, "blobNew", "commitNew"), sha: "", wantBlob: "blobNew"},
		{name: "update 200", resp: testutil.GHWrite(200, "blobUpd", "commitUpd"), sha: "oldsha", wantBlob: "blobUpd"},
		{name: "409 conflict", resp: testutil.GHStatus(409), sha: "stale", wantErr: github.ErrConflict},
		{name: "422 conflict", resp: testutil.GHStatus(422), sha: "stale", wantErr: github.ErrConflict},
		{name: "404 not found", resp: testutil.GHStatus(404), sha: "x", wantErr: github.ErrNotFound},
		{name: "500 generic error", resp: testutil.GHStatus(500), sha: "x", wantNonNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testutil.NewGitHubMock(t)
			m.OnPutContents("posts/x.json", tt.resp)
			c := github.NewClient("tok", github.WithAPIBase(m.APIBase()))

			blob, _, err := c.PutContents(context.Background(), m.Owner, m.Repo, "posts/x.json", "main", "msg", []byte("data"), tt.sha)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			case tt.wantNonNil:
				if err == nil || errors.Is(err, github.ErrConflict) || errors.Is(err, github.ErrNotFound) {
					t.Fatalf("expected a generic error, got %v", err)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if blob != tt.wantBlob {
					t.Errorf("blob sha = %q, want %q", blob, tt.wantBlob)
				}
			}
		})
	}
}

func TestPutContents_TransportError(t *testing.T) {
	c := github.NewClient("tok",
		github.WithAPIBase("http://unused.invalid"),
		github.WithHTTPClient(testutil.FailingHTTPClient()),
	)
	_, _, err := c.PutContents(context.Background(), "o", "r", "posts/x.json", "main", "msg", []byte("data"), "")
	if err == nil || errors.Is(err, github.ErrConflict) || errors.Is(err, github.ErrNotFound) {
		t.Fatalf("expected a transport error, got %v", err)
	}
}

func TestDeleteContents_StatusMapping(t *testing.T) {
	tests := []struct {
		name    string
		resp    testutil.GHResponse
		wantErr error // nil means success
	}{
		{name: "200 ok", resp: testutil.GHWrite(200, "", "commit"), wantErr: nil},
		{name: "409 conflict", resp: testutil.GHStatus(409), wantErr: github.ErrConflict},
		{name: "422 conflict", resp: testutil.GHStatus(422), wantErr: github.ErrConflict},
		{name: "404 not found", resp: testutil.GHStatus(404), wantErr: github.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testutil.NewGitHubMock(t)
			m.OnDeleteContents("posts/x.json", tt.resp)
			c := github.NewClient("tok", github.WithAPIBase(m.APIBase()))

			err := c.DeleteContents(context.Background(), m.Owner, m.Repo, "posts/x.json", "main", "msg", "sha")
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
