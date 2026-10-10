package source

import (
	"context"
	"fmt"

	"github.com/iamhimansu/himanshurajaman.com/internal/github"
)

// GitHubSource fetches the content repo as a tarball of the latest commit of a
// ref via the GitHub API.
type GitHubSource struct {
	client *github.Client
	owner  string
	repo   string
	ref    string
}

// NewGitHub builds a GitHub-backed source.
func NewGitHub(client *github.Client, owner, repo, ref string) *GitHubSource {
	return &GitHubSource{client: client, owner: owner, repo: repo, ref: ref}
}

// Describe implements Source. It deliberately omits the token.
func (s *GitHubSource) Describe() string {
	return fmt.Sprintf("github:%s/%s@%s", s.owner, s.repo, s.ref)
}

// Fetch resolves the ref to a commit SHA, downloads the tarball, and extracts
// it in memory. The SHA is resolved via a dedicated call so we record the full
// commit hash (the archive wrapper only carries a short SHA).
func (s *GitHubSource) Fetch(ctx context.Context) (*Snapshot, error) {
	sha, err := s.client.ResolveCommitSHA(ctx, s.owner, s.repo, s.ref)
	if err != nil {
		return nil, fmt.Errorf("resolve commit sha: %w", err)
	}
	body, err := s.client.Tarball(ctx, s.owner, s.repo, s.ref)
	if err != nil {
		return nil, fmt.Errorf("download tarball: %w", err)
	}
	defer body.Close()

	archiveFiles, err := github.ExtractTarGz(body)
	if err != nil {
		return nil, fmt.Errorf("extract tarball: %w", err)
	}

	files := make([]File, 0, len(archiveFiles))
	for _, af := range archiveFiles {
		files = append(files, File{Path: af.Path, Data: af.Data})
	}
	return &Snapshot{CommitSHA: sha, Ref: s.ref, Files: files}, nil
}
