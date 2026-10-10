package store_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/ingest"
	"github.com/iamhimansu/himanshurajaman.com/internal/source"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cache.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// ingestLocal opens a fresh store and ingests the default content fixture dir.
func ingestLocal(t *testing.T) *store.Store {
	t.Helper()
	st := openStore(t)
	imgs, err := ingest.NewImageManager(t.TempDir())
	if err != nil {
		t.Fatalf("image manager: %v", err)
	}
	ing := ingest.New(source.NewLocal(testutil.WriteContentDir(t)), st, content.NewRenderer(), imgs, discardLogger())
	if _, err := ing.Run(context.Background()); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	return st
}

func TestOpen_FreshStoreNotReady(t *testing.T) {
	st := openStore(t)
	_, ok, err := st.SyncState(context.Background())
	if err != nil {
		t.Fatalf("SyncState: %v", err)
	}
	if ok {
		t.Error("a brand-new store must report not-ready (no committed ingest)")
	}
}

func TestReplace_RollbackOnError(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)

	good := &store.Dataset{
		Posts:     []content.Post{makePost("keep", true)},
		CommitSHA: "commit-good",
		Ref:       "local",
		SyncedAt:  time.Now(),
	}
	if err := st.Replace(ctx, good); err != nil {
		t.Fatalf("initial Replace: %v", err)
	}

	// Two posts share a slug: the second INSERT violates UNIQUE(slug), so Replace
	// must roll back the whole transaction (including its opening DELETEs).
	bad := &store.Dataset{
		Posts:     []content.Post{makePost("dup", true), makePost("dup", true)},
		CommitSHA: "commit-bad",
		Ref:       "local",
		SyncedAt:  time.Now(),
	}
	if err := st.Replace(ctx, bad); err == nil {
		t.Fatal("expected Replace to fail on the duplicate slug")
	}

	// Prior good data survived intact.
	if _, err := st.GetPost(ctx, "keep"); err != nil {
		t.Errorf("good post should survive a failed Replace: %v", err)
	}
	if _, err := st.GetPost(ctx, "dup"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("failed Replace must not leave partial rows; got %v", err)
	}
	ss, ok, _ := st.SyncState(ctx)
	if !ok || ss.CommitSHA != "commit-good" {
		t.Errorf("sync_state should still reflect the good ingest, got ok=%v commit=%q", ok, ss.CommitSHA)
	}
}

func TestListPosts_DraftsExcluded(t *testing.T) {
	st := ingestLocal(t)
	posts, _, err := st.ListPosts(context.Background(), store.ListParams{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 6 {
		t.Fatalf("expected 6 published posts, got %d", len(posts))
	}
	for _, p := range posts {
		if p.Slug == "draft-note" {
			t.Error("draft-note must not appear in the public list")
		}
	}
}

func TestGetPost_DraftNotFound(t *testing.T) {
	st := ingestLocal(t)
	if _, err := st.GetPost(context.Background(), "draft-note"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetPost(draft) = %v, want ErrNotFound", err)
	}
}

func TestTagsWithCounts_PublishedOnly(t *testing.T) {
	st := ingestLocal(t)
	tags, err := st.TagsWithCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, tc := range tags {
		counts[tc.Slug] = tc.Count
	}
	if _, ok := counts["drafts"]; ok {
		t.Error("the draft-only tag must not appear in published tag counts")
	}
	if counts["writing"] != 4 {
		t.Errorf("writing tag count = %d, want 4 published posts", counts["writing"])
	}
}

func TestListPosts_KeysetPaginationStableOrder(t *testing.T) {
	st := ingestLocal(t)
	ctx := context.Background()

	full, _, err := st.ListPosts(ctx, store.ListParams{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := slugsOf(full)

	// Page through 2 at a time via the keyset cursor.
	var got []string
	cursor := ""
	for i := 0; i < 100; i++ {
		items, next, err := st.ListPosts(ctx, store.ListParams{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		got = append(got, slugsOf(items)...)
		if next == "" {
			break
		}
		cursor = next
	}

	if len(got) != len(wantOrder) {
		t.Fatalf("paged %d posts, single-shot had %d", len(got), len(wantOrder))
	}
	seen := map[string]bool{}
	for i, slug := range got {
		if slug != wantOrder[i] {
			t.Errorf("order drift at %d: paged %q, single-shot %q", i, slug, wantOrder[i])
		}
		if seen[slug] {
			t.Errorf("duplicate %q across pages (gap/overlap in keyset)", slug)
		}
		seen[slug] = true
	}
}

func TestSearch_PublishedOnlyBM25(t *testing.T) {
	st := ingestLocal(t)
	ctx := context.Background()

	// "quokka" exists only in the draft -> never surfaced by public search.
	if hits, _, err := st.Search(ctx, "quokka", "", 50); err != nil || len(hits) != 0 {
		t.Fatalf("search(quokka) = %d hits err=%v, want 0 (draft excluded)", len(hits), err)
	}

	// "lorem" matches four published posts (alpha/beta/delta body, gamma title).
	hits, _, err := st.Search(ctx, "lorem", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, h := range hits {
		got[h.Slug] = true
	}
	want := []string{"alpha-post", "beta-post", "gamma-post", "delta-post"}
	if len(hits) != len(want) {
		t.Fatalf("search(lorem) returned %d hits, want %d: %v", len(hits), len(want), got)
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("search(lorem) missing %q; got %v", w, got)
		}
	}

	// A single-term query returns exactly the matching post.
	if single, _, _ := st.Search(ctx, "epsilon", "", 50); len(single) != 1 || single[0].Slug != "epsilon-post" {
		t.Errorf("search(epsilon) should return only epsilon-post, got %+v", single)
	}

	// bm25 ordering is stable across keyset cursor paging.
	wantOrder := slugsOf(hits)
	var paged []string
	cursor := ""
	for i := 0; i < 100; i++ {
		items, next, err := st.Search(ctx, "lorem", cursor, 2)
		if err != nil {
			t.Fatalf("search page %d: %v", i, err)
		}
		paged = append(paged, slugsOf(items)...)
		if next == "" {
			break
		}
		cursor = next
	}
	if len(paged) != len(wantOrder) {
		t.Fatalf("paged search %d, single-shot %d", len(paged), len(wantOrder))
	}
	for i := range paged {
		if paged[i] != wantOrder[i] {
			t.Errorf("search order drift at %d: %q vs %q", i, paged[i], wantOrder[i])
		}
	}
}

func TestCursor_BadCursorRejected(t *testing.T) {
	st := ingestLocal(t)
	ctx := context.Background()
	if _, _, err := st.ListPosts(ctx, store.ListParams{Limit: 5, Cursor: "@@not-base64@@"}); !errors.Is(err, store.ErrBadCursor) {
		t.Errorf("ListPosts bad cursor = %v, want ErrBadCursor", err)
	}
	if _, _, err := st.Search(ctx, "lorem", "@@not-base64@@", 5); !errors.Is(err, store.ErrBadCursor) {
		t.Errorf("Search bad cursor = %v, want ErrBadCursor", err)
	}
}

// --- helpers ---

func makePost(slug string, published bool) content.Post {
	return content.Post{
		Slug:           slug,
		Title:          "Title " + slug,
		Summary:        "summary",
		Document:       `{"blocks":[]}`,
		HTML:           "<p>body</p>",
		PlainText:      "body",
		Published:      published,
		PublishedAt:    "2024-01-01T00:00:00Z",
		ReadingMinutes: 1,
		FilePath:       "posts/" + slug + ".json",
		FileSHA:        "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		Tags:           []content.Tag{{Name: "Tag", Slug: "tag"}},
	}
}

func slugsOf(items []store.PostSummary) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Slug
	}
	return out
}
