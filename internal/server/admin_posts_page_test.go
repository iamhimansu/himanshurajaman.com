package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

func TestAdminListPosts_PageFilter(t *testing.T) {
	// AC-31: ?page= scopes the admin list; absent page preserves the full list.
	// The default fixtures file "hello-world" under page "dsa".
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})

	// No page param: the full admin list (published + drafts) is returned.
	req := httptest.NewRequest(http.MethodGet, "/api/admin/posts", nil)
	req.AddCookie(h.sessionCookie("admin@example.com"))
	rr := h.serve(req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list all = %d, want 200", rr.Code)
	}
	all := decodeAdminPostList(t, rr)
	if len(all) < 2 {
		t.Fatalf("all posts = %d, want the full fixture set", len(all))
	}

	// Scoped to page=dsa: only hello-world.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/posts?page=dsa", nil)
	req.AddCookie(h.sessionCookie("admin@example.com"))
	rr = h.serve(req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list page=dsa = %d, want 200", rr.Code)
	}
	scoped := decodeAdminPostList(t, rr)
	if len(scoped) != 1 || scoped[0] != "hello-world" {
		t.Fatalf("page=dsa posts = %v, want [hello-world]", scoped)
	}
}

func TestAdminGetPost_CarriesPage(t *testing.T) {
	// AC-28: a post's meta.page is surfaced in the admin get response.
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/posts/hello-world", nil)
	req.AddCookie(h.sessionCookie("admin@example.com"))
	rr := h.serve(req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get post = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Meta struct {
			Page string `json:"page"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Meta.Page != "dsa" {
		t.Fatalf("meta.page = %q, want dsa", resp.Meta.Page)
	}
}

func TestAdminCreatePost_PersistsPage(t *testing.T) {
	// AC-28: a created post carries meta.page through to the cache; a subsequent
	// admin get reflects it.
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	h.gh.OnPutContents("posts/filed-post.json", testutil.GHWrite(201, "bbbb111122223333444455556666777788889999", "commitsha"))

	body := adminPostBodyWithPage("filed-post", "Filed Post", "dsa")
	req := h.authedWrite(http.MethodPost, "/api/admin/posts", body)
	if rr := h.serve(req); rr.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201 (body=%s)", rr.Code, rr.Body.String())
	}

	// It now appears under the page scope.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/posts?page=dsa", nil)
	req.AddCookie(h.sessionCookie("admin@example.com"))
	rr := h.serve(req)
	scoped := decodeAdminPostList(t, rr)
	if !contains(scoped, "filed-post") {
		t.Fatalf("page=dsa posts = %v, want to include filed-post", scoped)
	}
}

func adminPostBodyWithPage(slug, title, page string) string {
	meta := map[string]any{
		"title":     title,
		"slug":      slug,
		"date":      "2024-06-01T00:00:00Z",
		"summary":   "s",
		"published": true,
	}
	if page != "" {
		meta["page"] = page
	}
	content := map[string]any{
		"blocks": []any{
			map[string]any{"type": "paragraph", "data": map[string]any{"text": "body text here"}},
		},
	}
	b, _ := json.Marshal(map[string]any{"meta": meta, "content": content})
	return string(b)
}

func decodeAdminPostList(t *testing.T, rr *httptest.ResponseRecorder) []string {
	t.Helper()
	var resp struct {
		Items []struct {
			Slug string `json:"slug"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode admin post list: %v (body=%s)", err, rr.Body.String())
	}
	out := make([]string, 0, len(resp.Items))
	for _, it := range resp.Items {
		out = append(out, it.Slug)
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
