package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMeta_ArticleForPublishedSlug(t *testing.T) {
	h := newServer(t, options{})
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/blog/hello-world", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`<meta property="og:type" content="article">`,
		`<meta property="article:published_time"`,
		"Hello, World", // the post title appears in <title>/og:title
	} {
		if !strings.Contains(body, want) {
			t.Errorf("article meta missing %q", want)
		}
	}
}

func TestMeta_UnknownSlug404(t *testing.T) {
	h := newServer(t, options{})
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/blog/no-such-post", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown slug status = %d, want 404", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `content="website"`) {
		t.Errorf("expected default (website) meta for unknown slug:\n%s", body)
	}
}

func TestMeta_DraftSlug404(t *testing.T) {
	h := newServer(t, options{})
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/blog/draft-note", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("draft slug status = %d, want 404", rr.Code)
	}
}

func TestMeta_DefaultForRoot(t *testing.T) {
	h := newServer(t, options{})
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("root status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `<meta property="og:type" content="website">`) {
		t.Errorf("root should have default website meta:\n%s", body)
	}
	// The static shell <title> was stripped and replaced with the resolved one.
	if strings.Count(body, "<title>") != 1 {
		t.Errorf("expected exactly one <title> after meta injection:\n%s", body)
	}
}
