// Package source abstracts "the whole content repo at one commit" behind a
// single interface so ingest doesn't care whether content came from GitHub (a
// tarball of the latest commit) or a local directory (development).
package source

import "context"

// File is one logical file from the content repo.
type File struct {
	Path string // forward-slash, repo-relative, e.g. "posts/hello.json"
	Data []byte
	SHA  string // git blob SHA when known; left empty here and filled by ingest
}

// Snapshot is an immutable view of the content repo at a single commit.
type Snapshot struct {
	CommitSHA string
	Ref       string
	Files     []File
}

// Source fetches the entire content repo as one in-memory snapshot. Fetching
// the whole repo as one archive (rather than file-by-file) keeps ingest a
// single atomic operation and avoids per-file API round-trips.
type Source interface {
	Fetch(ctx context.Context) (*Snapshot, error)
	// Describe returns a short, non-sensitive label for logs (no tokens).
	Describe() string
}
