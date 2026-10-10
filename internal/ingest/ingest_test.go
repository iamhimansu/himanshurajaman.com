package ingest_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/ingest"
	"github.com/iamhimansu/himanshurajaman.com/internal/source"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newIngester binds an ingester to an existing store and a local content dir.
func newIngester(t *testing.T, st *store.Store, dir string) *ingest.Ingester {
	t.Helper()
	imgs, err := ingest.NewImageManager(t.TempDir())
	if err != nil {
		t.Fatalf("image manager: %v", err)
	}
	return ingest.New(source.NewLocal(dir), st, content.NewRenderer(), imgs, discardLogger())
}

// openStore opens a disposable store under t.TempDir().
func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cache.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// ingestLocal opens a fresh store and runs a full ingest of dir, failing the
// test on error.
func ingestLocal(t *testing.T, dir string) (*store.Store, *ingest.Result) {
	t.Helper()
	st := openStore(t)
	res, err := newIngester(t, st, dir).Run(context.Background())
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	return st, res
}

func TestRun_FreshIngestPopulatesEverything(t *testing.T) {
	st, res := ingestLocal(t, testutil.WriteContentDir(t))
	ctx := context.Background()

	if res.Published != 6 || res.Drafts != 1 || res.Skipped != 0 {
		t.Errorf("counts: published=%d drafts=%d skipped=%d, want 6/1/0", res.Published, res.Drafts, res.Skipped)
	}
	if res.Images != 1 {
		t.Errorf("images = %d, want 1", res.Images)
	}

	posts, _, err := st.ListPosts(ctx, store.ListParams{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 6 {
		t.Errorf("ListPosts returned %d published posts, want 6", len(posts))
	}

	resume, err := st.Resume(ctx)
	if err != nil || len(resume) == 0 {
		t.Errorf("resume not populated: %d sections, err=%v", len(resume), err)
	}

	settings, err := st.Settings(ctx)
	if err != nil || settings["postsPerPage"] == nil {
		t.Errorf("settings not populated: %v err=%v", settings, err)
	}

	// FTS is populated: a term from a published post is found.
	hits, _, err := st.Search(ctx, "lorem", "", 50)
	if err != nil || len(hits) == 0 {
		t.Errorf("search index empty: %d hits err=%v", len(hits), err)
	}

	ss, ok, err := st.SyncState(ctx)
	if err != nil || !ok || ss.CommitSHA == "" {
		t.Errorf("sync state missing after ingest: ok=%v err=%v", ok, err)
	}
}

func TestRun_RestartRebuildsIdentically(t *testing.T) {
	dir := testutil.WriteContentDir(t)
	path := filepath.Join(t.TempDir(), "cache.sqlite")

	st1, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store 1: %v", err)
	}
	if _, err := newIngester(t, st1, dir).Run(context.Background()); err != nil {
		t.Fatalf("ingest 1: %v", err)
	}
	snap1 := capture(t, st1)
	_ = st1.Close()

	// Reopen a brand-new store at the SAME path (store.Open wipes the stale file)
	// and re-ingest from the same source: the cache is disposable and must rebuild
	// to byte-identical public results.
	st2, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store 2: %v", err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	if _, err := newIngester(t, st2, dir).Run(context.Background()); err != nil {
		t.Fatalf("ingest 2: %v", err)
	}
	snap2 := capture(t, st2)

	snap1.assertEqual(t, snap2)
}

func TestRun_FailedRefreshPreservesCache(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)

	// First good ingest.
	if _, err := newIngester(t, st, testutil.WriteContentDir(t)).Run(ctx); err != nil {
		t.Fatalf("initial ingest: %v", err)
	}
	before := capture(t, st)
	ssBefore, _, _ := st.SyncState(ctx)

	assertPreserved := func(label string) {
		ssAfter, ok, _ := st.SyncState(ctx)
		if !ok || ssBefore.CommitSHA != ssAfter.CommitSHA {
			t.Errorf("%s: sync state changed (%q -> %q)", label, ssBefore.CommitSHA, ssAfter.CommitSHA)
		}
		capture(t, st).assertEqual(t, before)
	}

	// Refresh from a dir missing the required resume file -> error, cache intact.
	missing := testutil.DefaultContentDir()
	missing.Resume = nil
	if _, err := newIngester(t, st, missing.Write(t)).Run(ctx); err == nil {
		t.Fatal("expected error for missing resume")
	}
	assertPreserved("missing resume")

	// Refresh from a dir with an invalid resume -> error, cache intact.
	invalid := testutil.DefaultContentDir()
	invalid.Resume = []byte(`{"basics":{}}`) // basics present but missing name/email
	if _, err := newIngester(t, st, invalid.Write(t)).Run(ctx); err == nil {
		t.Fatal("expected error for invalid resume")
	}
	assertPreserved("invalid resume")
}

func TestRun_InvalidPostSkipped(t *testing.T) {
	cd := testutil.DefaultContentDir()
	// Empty title makes ParsePost fail; ingest should skip it, not abort.
	cd.Posts["invalid.json"] = []byte(`{"meta":{"title":"","slug":"invalid","date":"2024-01-01T00:00:00Z","summary":"s"},"content":{}}`)
	st, res := ingestLocal(t, cd.Write(t))

	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	if res.Published != 6 {
		t.Errorf("Published = %d, want 6 (invalid skipped, not counted)", res.Published)
	}
	if _, err := st.GetPost(context.Background(), "invalid"); err == nil {
		t.Error("invalid post should not be queryable")
	}
}

func TestRun_DuplicateSlugSkipped(t *testing.T) {
	cd := testutil.DefaultContentDir()
	cd.Posts["dup.json"] = testutil.PostJSON(testutil.PostInput{
		Slug: "dup", Title: "First Dup", Summary: "s", Date: "2024-06-01T00:00:00Z",
		Published: true, Body: "first dup body",
	})
	// A second file in a subdir shares the stem "dup" (and thus the slug), so
	// ingest's first-seen de-duplication skips it.
	cd.Posts["nested/dup.json"] = testutil.PostJSON(testutil.PostInput{
		Slug: "dup", Title: "Second Dup", Summary: "s", Date: "2024-06-02T00:00:00Z",
		Published: true, Body: "second dup body",
	})
	st, res := ingestLocal(t, cd.Write(t))

	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (the duplicate slug)", res.Skipped)
	}
	post, err := st.GetPost(context.Background(), "dup")
	if err != nil {
		t.Fatalf("the first dup should be present: %v", err)
	}
	if post.Title != "First Dup" {
		t.Errorf("first-seen should win: got title %q", post.Title)
	}
}

func TestBuildResumeValidation(t *testing.T) {
	t.Run("valid flattens basics first then sections", func(t *testing.T) {
		secs, err := ingest.BuildResume(testutil.ResumeJSON())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(secs) < 1 || secs[0].Kind != "basics" || secs[0].Position != 0 {
			t.Fatalf("expected basics at position 0, got %+v", secs)
		}
	})

	t.Run("unknown section kind kept generically", func(t *testing.T) {
		raw, _ := json.Marshal(map[string]any{
			"basics":   map[string]any{"name": "N", "email": "e@x.com"},
			"sections": []any{map[string]any{"title": "No Kind"}},
		})
		secs, err := ingest.BuildResume(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(secs) != 2 || secs[1].Kind != "unknown" {
			t.Errorf("unknown section kind should be stored as 'unknown', got %+v", secs)
		}
	})

	invalid := map[string][]byte{
		"not json":      []byte(`{nope`),
		"missing basics": []byte(`{"sections":[]}`),
		"no name":       []byte(`{"basics":{"email":"e@x.com"}}`),
		"no email":      []byte(`{"basics":{"name":"N"}}`),
	}
	for name, data := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := ingest.BuildResume(data); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}

func TestBuildSettingsValidation(t *testing.T) {
	t.Run("valid compacts values", func(t *testing.T) {
		m, err := ingest.BuildSettings(testutil.SettingsJSON())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m["title"] == nil {
			t.Error("expected title key present")
		}
	})

	for name, data := range map[string][]byte{
		"not json":     []byte(`{bad`),
		"empty object": []byte(`{}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ingest.BuildSettings(data); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}

// --- snapshot comparison helpers ---

type cacheSnapshot struct {
	list     string
	tags     string
	search   string
	detail   string
	resume   string
	settings string
}

// capture serializes the public query outputs so two ingests can be compared
// for identical results (the unexported row ids are excluded by JSON).
func capture(t *testing.T, st *store.Store) cacheSnapshot {
	t.Helper()
	ctx := context.Background()
	list, _, err := st.ListPosts(ctx, store.ListParams{Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	tags, err := st.TagsWithCounts(ctx)
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	search, _, err := st.Search(ctx, "lorem", "", 50)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	detail, err := st.GetPost(ctx, "hello-world")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	resume, err := st.Resume(ctx)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	settings, err := st.Settings(ctx)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	return cacheSnapshot{
		list: mustJSON(t, list), tags: mustJSON(t, tags), search: mustJSON(t, search),
		detail: mustJSON(t, detail), resume: mustJSON(t, resume), settings: mustJSON(t, settings),
	}
}

func (a cacheSnapshot) assertEqual(t *testing.T, b cacheSnapshot) {
	t.Helper()
	if a.list != b.list {
		t.Errorf("list results differ:\n%s\n---\n%s", a.list, b.list)
	}
	if a.tags != b.tags {
		t.Errorf("tags differ:\n%s\n---\n%s", a.tags, b.tags)
	}
	if a.search != b.search {
		t.Errorf("search results differ:\n%s\n---\n%s", a.search, b.search)
	}
	if a.detail != b.detail {
		t.Errorf("detail differs:\n%s\n---\n%s", a.detail, b.detail)
	}
	if a.resume != b.resume {
		t.Errorf("resume differs")
	}
	if a.settings != b.settings {
		t.Errorf("settings differ")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
