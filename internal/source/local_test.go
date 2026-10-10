package source_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/source"
)

func writeFile(t *testing.T, dir, rel, data string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestLocalSource_Fetch_SkipsDotfiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "posts/a.json", `{"a":1}`)
	writeFile(t, dir, "normal.txt", "hi")
	writeFile(t, dir, ".hidden", "secret")
	writeFile(t, dir, "sub/.git/config", "[core]")

	snap, err := source.NewLocal(dir).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	got := map[string]bool{}
	for _, f := range snap.Files {
		got[f.Path] = true
	}
	if !got["posts/a.json"] || !got["normal.txt"] {
		t.Errorf("expected regular files kept, got %v", got)
	}
	if got[".hidden"] || got["sub/.git/config"] {
		t.Errorf("dotfiles should be skipped, got %v", got)
	}
}

func TestLocalSource_ContentHashStable(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "posts/a.json", `{"a":1}`)
	writeFile(t, dir, "settings/site.json", `{"title":"x"}`)

	src := source.NewLocal(dir)
	first, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	second, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch again: %v", err)
	}
	if first.CommitSHA == "" || first.CommitSHA != second.CommitSHA {
		t.Errorf("content hash should be stable across fetches: %q vs %q", first.CommitSHA, second.CommitSHA)
	}

	// Changing a file changes the hash.
	writeFile(t, dir, "posts/a.json", `{"a":2}`)
	third, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch after change: %v", err)
	}
	if third.CommitSHA == first.CommitSHA {
		t.Errorf("content hash should change when a file changes: %q", third.CommitSHA)
	}
}
