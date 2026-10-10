package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LocalSource reads the content repo from a directory on disk so the backend
// runs with no GitHub credentials.
type LocalSource struct {
	dir string
}

// NewLocal builds a directory-backed source.
func NewLocal(dir string) *LocalSource {
	return &LocalSource{dir: dir}
}

// Describe implements Source.
func (s *LocalSource) Describe() string {
	return "local:" + s.dir
}

// Fetch walks the directory and reads every regular file, skipping dotfiles
// (e.g. a stray .DS_Store or VCS metadata). The "commit SHA" is a content hash
// so readiness/sync_state still carry a stable, change-sensitive identifier.
func (s *LocalSource) Fetch(_ context.Context) (*Snapshot, error) {
	var files []File
	err := filepath.WalkDir(s.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(s.dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, ".") || strings.Contains(rel, "/.") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, File{Path: rel, Data: data})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read local content dir: %w", err)
	}
	return &Snapshot{CommitSHA: contentHash(files), Ref: "local", Files: files}, nil
}

func contentHash(files []File) string {
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	h := sha256.New()
	for _, f := range sorted {
		fmt.Fprintf(h, "%s\n%d\n", f.Path, len(f.Data))
		h.Write(f.Data)
	}
	return "local-" + hex.EncodeToString(h.Sum(nil))[:12]
}
