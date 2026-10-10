package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type listResponse struct {
	Items []struct {
		Slug string `json:"slug"`
	} `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

func decodeList(t *testing.T, rr *httptest.ResponseRecorder) listResponse {
	t.Helper()
	var lr listResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &lr); err != nil {
		t.Fatalf("decode list response: %v (body=%s)", err, rr.Body.String())
	}
	return lr
}

func TestPublicListPosts_PublishedOnly_Pagination(t *testing.T) {
	h := newServer(t, options{})

	rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/posts", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	all := decodeList(t, rr)
	if len(all.Items) != 6 {
		t.Fatalf("expected 6 published posts, got %d", len(all.Items))
	}
	for _, it := range all.Items {
		if it.Slug == "draft-note" {
			t.Fatal("draft must not appear in the public list")
		}
	}

	// Page through with limit=2 using the returned cursor.
	first := decodeList(t, h.serve(httptest.NewRequest(http.MethodGet, "/api/posts?limit=2", nil)))
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page: items=%d nextCursor=%v", len(first.Items), first.NextCursor)
	}
	second := decodeList(t, h.serve(httptest.NewRequest(http.MethodGet, "/api/posts?limit=2&cursor="+*first.NextCursor, nil)))
	if len(second.Items) != 2 {
		t.Fatalf("second page items = %d, want 2", len(second.Items))
	}
	if first.Items[0].Slug == second.Items[0].Slug {
		t.Error("second page should not repeat the first page's first item")
	}
}

func TestPublicGetPost_Draft404(t *testing.T) {
	h := newServer(t, options{})
	if rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/posts/draft-note", nil)); rr.Code != http.StatusNotFound {
		t.Fatalf("GET draft via API = %d, want 404", rr.Code)
	}
	if rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/posts/hello-world", nil)); rr.Code != http.StatusOK {
		t.Fatalf("GET published via API = %d, want 200", rr.Code)
	}
}

func TestPublicTags_PublishedOnly(t *testing.T) {
	h := newServer(t, options{})
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/tags", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp struct {
		Items []struct {
			Slug  string `json:"slug"`
			Count int    `json:"count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, tag := range resp.Items {
		if tag.Slug == "drafts" {
			t.Error("draft-only tag must not appear")
		}
	}
}

func TestPublicSearch_PublishedOnlyBM25(t *testing.T) {
	h := newServer(t, options{})

	// quokka only exists in the draft -> no public results.
	if got := decodeList(t, h.serve(httptest.NewRequest(http.MethodGet, "/api/search?q=quokka", nil))); len(got.Items) != 0 {
		t.Fatalf("search quokka returned %d, want 0", len(got.Items))
	}
	// lorem matches four published posts.
	if got := decodeList(t, h.serve(httptest.NewRequest(http.MethodGet, "/api/search?q=lorem", nil))); len(got.Items) != 4 {
		t.Fatalf("search lorem returned %d, want 4", len(got.Items))
	}
}

func TestPublic_WarmingReturns503(t *testing.T) {
	h := newServer(t, options{notReady: true})
	for _, path := range []string{"/api/posts", "/api/tags", "/api/search?q=x", "/api/posts/hello-world"} {
		rr := h.serve(httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("%s while warming = %d, want 503", path, rr.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body["status"] != "warming" {
			t.Errorf("%s warming body = %s", path, rr.Body.String())
		}
	}
}
