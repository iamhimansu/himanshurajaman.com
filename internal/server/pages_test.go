package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
)

type pageResponse struct {
	Page struct {
		Slug    string  `json:"slug"`
		Title   string  `json:"title"`
		HTML    string  `json:"html"`
		Summary *string `json:"summary"`
		Updated *string `json:"updated"`
	} `json:"page"`
	Posts []struct {
		Slug string `json:"slug"`
	} `json:"posts"`
}

func decodePage(t *testing.T, rr *httptest.ResponseRecorder) pageResponse {
	t.Helper()
	var pr pageResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &pr); err != nil {
		t.Fatalf("decode page response: %v (body=%s)", err, rr.Body.String())
	}
	return pr
}

func TestPublicGetPage_PublishedWithPosts(t *testing.T) {
	// The default fixtures publish the "dsa" nav page and file "hello-world"
	// under it. AC-16: a published page returns 200 with its published posts.
	h := newServer(t, options{})
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/pages/dsa", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	pr := decodePage(t, rr)
	if pr.Page.Slug != "dsa" || pr.Page.Title != "DSA" {
		t.Fatalf("page = %+v, want slug=dsa title=DSA", pr.Page)
	}
	if len(pr.Posts) != 1 || pr.Posts[0].Slug != "hello-world" {
		t.Fatalf("posts = %+v, want [hello-world]", pr.Posts)
	}
}

func TestPublicGetPage_UnknownAndDraft404(t *testing.T) {
	h := newServer(t, options{})
	// AC-18: unknown slug -> 404.
	if rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/pages/no-such-page", nil)); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown page = %d, want 404", rr.Code)
	}
	// AC-17: unpublished (draft) page -> 404.
	if rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/pages/scratch", nil)); rr.Code != http.StatusNotFound {
		t.Fatalf("draft page = %d, want 404", rr.Code)
	}
}

func TestPublicGetPage_ZeroPostsNonNil(t *testing.T) {
	// AC-18: a published page with no filed posts returns the page plus a
	// non-nil empty posts array. Add such a page directly to the cache.
	h := newServer(t, options{})
	ctx := context.Background()
	if err := h.store.UpsertPage(ctx, content.Page{
		Slug: "empty-page", Title: "Empty", Nav: false, NavOrder: content.NavOrderDefault,
		Published: true, HTML: "<p>none</p>", Document: `{"blocks":[]}`,
		FilePath: "pages/empty-page.json", FileSHA: "sha",
	}); err != nil {
		t.Fatalf("upsert page: %v", err)
	}

	rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/pages/empty-page", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	if !bytesContainsPostsArray(rr) {
		t.Errorf("response must carry a non-nil posts array:\n%s", rr.Body.String())
	}
	pr := decodePage(t, rr)
	if len(pr.Posts) != 0 {
		t.Fatalf("posts = %+v, want empty", pr.Posts)
	}
}

// bytesContainsPostsArray confirms the JSON encodes posts as [] (not null),
// proving the handler sent a non-nil slice.
func bytesContainsPostsArray(rr *httptest.ResponseRecorder) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		return false
	}
	v, ok := raw["posts"]
	return ok && len(v) > 0 && v[0] == '['
}
