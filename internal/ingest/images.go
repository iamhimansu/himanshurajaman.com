package ingest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/iamhimansu/himanshurajaman.com/internal/source"
)

// ImageManager owns the on-disk image set served at /images/*. Each ingest
// writes the new images into a fresh directory and then atomically swaps the
// "current" pointer, so the HTTP handler always serves a complete set and a
// failed ingest never corrupts what's being served.
type ImageManager struct {
	baseDir string

	mu      sync.RWMutex
	current string
}

// NewImageManager prepares the base directory that holds image sets.
func NewImageManager(baseDir string) (*ImageManager, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create image base dir: %w", err)
	}
	return &ImageManager{baseDir: baseDir}, nil
}

// Prepare writes image files (paths under "images/") into a new directory and
// returns its path plus the count written. It does NOT activate the set.
func (m *ImageManager) Prepare(files []source.File) (dir string, n int, err error) {
	dir, err = os.MkdirTemp(m.baseDir, "set-")
	if err != nil {
		return "", 0, err
	}
	for _, f := range files {
		rel := strings.TrimPrefix(f.Path, "images/")
		if rel == "" || strings.Contains(rel, "..") {
			continue
		}
		dest := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			_ = os.RemoveAll(dir)
			return "", 0, err
		}
		if err := os.WriteFile(dest, f.Data, 0o644); err != nil {
			_ = os.RemoveAll(dir)
			return "", 0, err
		}
		n++
	}
	return dir, n, nil
}

// Activate makes dir the served set and removes the previous one.
func (m *ImageManager) Activate(dir string) {
	m.mu.Lock()
	old := m.current
	m.current = dir
	m.mu.Unlock()
	if old != "" && old != dir {
		_ = os.RemoveAll(old)
	}
}

// Discard removes an un-activated prepared directory (used when ingest fails
// after Prepare).
func (m *ImageManager) Discard(dir string) {
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

// Stage writes a single image into the currently active served directory so an
// admin upload previews immediately at /images/<rel>. It applies the same
// path-safety as Prepare (reject empty or "..") and is a no-op (logged by the
// caller) when there is no active dir yet — the image then appears after the
// next ingest. Stage never activates or swaps directories.
func (m *ImageManager) Stage(rel string, data []byte) error {
	if rel == "" || strings.Contains(rel, "..") {
		return fmt.Errorf("unsafe image path %q", rel)
	}
	dir := m.Current()
	if dir == "" {
		// No active set yet; skip. The image lands on the next ingest.
		return nil
	}
	dest := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}

// Current returns the active image directory, or "" before the first ingest.
func (m *ImageManager) Current() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}
