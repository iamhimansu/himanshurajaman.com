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

func makePage(slug string, published, nav bool) content.Page {
	return content.Page{
		Slug: slug, Title: "Page " + slug, Nav: nav, NavOrder: content.NavOrderDefault,
		Published: published, HTML: "<p>page</p>", Document: `{"blocks":[]}`,
		FilePath: "pages/" + slug + ".json", FileSHA: "cafebabecafebabecafebabecafebabecafebabe",
	}
}

func TestUpsertPage_GetPublishedVsDraft(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)

	if err := st.UpsertPage(ctx, makePage("pub", true, true)); err != nil {
		t.Fatalf("upsert pub: %v", err)
	}
	if err := st.UpsertPage(ctx, makePage("draft", false, false)); err != nil {
		t.Fatalf("upsert draft: %v", err)
	}

	if pg, err := st.GetPage(ctx, "pub"); err != nil || pg.Slug != "pub" {
		t.Fatalf("GetPage(pub) = %+v err=%v, want published page", pg, err)
	}
	if _, err := st.GetPage(ctx, "draft"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetPage(draft) = %v, want ErrNotFound", err)
	}
	if _, err := st.GetPage(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetPage(missing) = %v, want ErrNotFound", err)
	}

	// GetAdminPage returns drafts too.
	if ap, err := st.GetAdminPage(ctx, "draft"); err != nil || ap.Published {
		t.Errorf("GetAdminPage(draft) = %+v err=%v, want draft present", ap, err)
	}

	// Upsert is an update when the slug already exists.
	updated := makePage("pub", true, true)
	updated.Title = "Renamed"
	if err := st.UpsertPage(ctx, updated); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if ap, _ := st.GetAdminPage(ctx, "pub"); ap.Title != "Renamed" {
		t.Errorf("update not applied: title=%q", ap.Title)
	}
}

// AC-23: ListPages post counts span all statuses; AC-11/12: ListNavPages
// filters+orders published nav pages.
func TestListPages_And_ListNavPages(t *testing.T) {
	st := ingestLocal(t)
	ctx := context.Background()

	pages, err := st.ListPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]store.AdminPageSummary{}
	for _, p := range pages {
		counts[p.Slug] = p
	}
	// The default fixture has a published "dsa" (nav) and a draft "scratch".
	if _, ok := counts["dsa"]; !ok {
		t.Fatalf("ListPages missing dsa: %+v", pages)
	}
	if _, ok := counts["scratch"]; !ok {
		t.Fatalf("ListPages missing draft scratch (admin sees drafts): %+v", pages)
	}
	// hello-world is filed under dsa, so dsa's post count is 1 (across statuses).
	if counts["dsa"].PostCount != 1 {
		t.Errorf("dsa post_count = %d, want 1", counts["dsa"].PostCount)
	}
	if counts["scratch"].PostCount != 0 {
		t.Errorf("scratch post_count = %d, want 0", counts["scratch"].PostCount)
	}

	nav, err := st.ListNavPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Only the published nav page shows; the draft scratch is excluded.
	if len(nav) != 1 || nav[0].Slug != "dsa" {
		t.Fatalf("ListNavPages = %+v, want only dsa", nav)
	}
	// Label falls back from nav_label (DSA) which was set in the fixture.
	if nav[0].Label != "DSA" {
		t.Errorf("nav label = %q, want DSA", nav[0].Label)
	}
}

func TestListNavPages_OrderingAndLabelFallback(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)

	ordB := 5
	ordA := 20
	pB := makePage("bbb", true, true)
	pB.NavOrder = ordB
	pB.NavLabel = "" // falls back to title "Page bbb"
	pA := makePage("aaa", true, true)
	pA.NavOrder = ordA
	pA.NavLabel = "Custom A"
	// A published non-nav page must be excluded.
	pNoNav := makePage("ccc", true, false)
	// A draft nav page must be excluded.
	pDraft := makePage("ddd", false, true)

	for _, p := range []content.Page{pA, pB, pNoNav, pDraft} {
		if err := st.UpsertPage(ctx, p); err != nil {
			t.Fatalf("upsert %s: %v", p.Slug, err)
		}
	}

	nav, err := st.ListNavPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nav) != 2 {
		t.Fatalf("ListNavPages len = %d, want 2 (%+v)", len(nav), nav)
	}
	// Ordered by nav_order ASC: bbb(5) before aaa(20).
	if nav[0].Slug != "bbb" || nav[1].Slug != "aaa" {
		t.Errorf("nav order = %+v, want bbb then aaa", nav)
	}
	if nav[0].Label != "Page bbb" {
		t.Errorf("bbb label = %q, want title fallback 'Page bbb'", nav[0].Label)
	}
	if nav[1].Label != "Custom A" {
		t.Errorf("aaa label = %q, want 'Custom A'", nav[1].Label)
	}
}

// AC-9 / PostsByPage: a dangling meta.page post still lists in /blog; PostsByPage
// honors the publishedOnly filter.
func TestPostsByPage_PublishedOnlyVsAll(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)

	pub := makePost("filed-pub", true)
	pub.Page = "dsa"
	draft := makePost("filed-draft", false)
	draft.Page = "dsa"
	unfiled := makePost("unfiled", true)

	ds := &store.Dataset{
		Posts:     []content.Post{pub, draft, unfiled},
		CommitSHA: "c", Ref: "local", SyncedAt: time.Now(),
	}
	if err := st.Replace(ctx, ds); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// published-only returns only the published filed post.
	only, err := st.PostsByPage(ctx, "dsa", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].Slug != "filed-pub" {
		t.Errorf("PostsByPage(dsa, published) = %+v, want [filed-pub]", slugsOf(only))
	}

	// all statuses returns both.
	all, err := st.PostsByPage(ctx, "dsa", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("PostsByPage(dsa, all) len = %d, want 2 (%v)", len(all), slugsOf(all))
	}

	if n, err := st.CountPostsByPage(ctx, "dsa"); err != nil || n != 2 {
		t.Errorf("CountPostsByPage(dsa) = %d err=%v, want 2", n, err)
	}

	// The dangling-page post (no matching page row) still appears in /blog.
	list, _, err := st.ListPosts(ctx, store.ListParams{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range list {
		seen[p.Slug] = true
	}
	if !seen["filed-pub"] {
		t.Error("a post with meta.page should still appear in the public blog list")
	}
}

func TestSitemapPages_PublishedOnly(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	if err := st.UpsertPage(ctx, makePage("pub", true, true)); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertPage(ctx, makePage("draft", false, false)); err != nil {
		t.Fatal(err)
	}
	pages, err := st.SitemapPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Slug != "pub" {
		t.Errorf("SitemapPages = %+v, want only pub", pages)
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
