package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

// adminPageBody builds a create/update page request JSON.
func adminPageBody(slug, title string, published, nav bool) string {
	meta := map[string]any{
		"title":     title,
		"slug":      slug,
		"nav":       nav,
		"published": published,
	}
	content := map[string]any{
		"blocks": []any{
			map[string]any{"type": "paragraph", "data": map[string]any{"text": "body"}},
		},
	}
	b, _ := json.Marshal(map[string]any{"meta": meta, "content": content})
	return string(b)
}

// authedWrite builds a write request carrying a valid session + matching CSRF.
func (h *harness) authedWrite(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(h.sessionCookie("admin@example.com"))
	csrfCookie, token := h.csrfPair()
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", token)
	return req
}

func TestAdminPages_AuthMatrix(t *testing.T) {
	// AC-29: the five page endpoints enforce the same middleware contract as
	// posts. No session on any of them -> 401 (auth-before-readiness).
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/admin/pages"},
		{http.MethodGet, "/api/admin/pages/dsa"},
		{http.MethodPost, "/api/admin/pages"},
		{http.MethodPut, "/api/admin/pages/dsa"},
		{http.MethodDelete, "/api/admin/pages/dsa"},
	}
	for _, c := range cases {
		rr := h.serve(httptest.NewRequest(c.method, c.path, strings.NewReader("{}")))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s %s no session = %d, want 401", c.method, c.path, rr.Code)
		}
	}
}

func TestAdminPages_WriteRequiresCSRF(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/pages", strings.NewReader(adminPageBody("x", "X", true, false)))
	req.AddCookie(h.sessionCookie("admin@example.com"))
	if rr := h.serve(req); rr.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF = %d, want 403", rr.Code)
	}
}

func TestAdminPages_WriteRequiresWritesEnabled(t *testing.T) {
	// admin enabled but local content (writes disabled) -> 503 on writes.
	h := newServer(t, options{adminEnabled: true, writesEnabled: false})
	req := h.authedWrite(http.MethodPost, "/api/admin/pages", adminPageBody("x", "X", true, false))
	if rr := h.serve(req); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("writes disabled = %d, want 503", rr.Code)
	}
}

func TestAdminPages_ListAndGet(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})

	// List includes drafts (dsa + scratch from the default fixtures).
	req := httptest.NewRequest(http.MethodGet, "/api/admin/pages", nil)
	req.AddCookie(h.sessionCookie("admin@example.com"))
	rr := h.serve(req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list pages = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var listed struct {
		Items []struct {
			Slug      string `json:"slug"`
			PostCount int    `json:"post_count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	foundDSA := false
	for _, it := range listed.Items {
		if it.Slug == "dsa" {
			foundDSA = true
			if it.PostCount != 1 {
				t.Errorf("dsa post_count = %d, want 1", it.PostCount)
			}
		}
	}
	if !foundDSA {
		t.Fatalf("list missing dsa: %+v", listed.Items)
	}

	// Get the draft page (admin sees drafts).
	req = httptest.NewRequest(http.MethodGet, "/api/admin/pages/scratch", nil)
	req.AddCookie(h.sessionCookie("admin@example.com"))
	rr = h.serve(req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get draft page = %d, want 200", rr.Code)
	}

	// Unknown slug -> 404.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/pages/nope", nil)
	req.AddCookie(h.sessionCookie("admin@example.com"))
	if rr := h.serve(req); rr.Code != http.StatusNotFound {
		t.Fatalf("get unknown page = %d, want 404", rr.Code)
	}
}

func TestAdminPages_CreateSuccess(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	h.gh.OnPutContents("pages/newpage.json", testutil.GHWrite(201, "aaaa111122223333444455556666777788889999", "commitsha"))

	req := h.authedWrite(http.MethodPost, "/api/admin/pages", adminPageBody("newpage", "New Page", true, true))
	rr := h.serve(req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create page = %d, want 201 (body=%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Slug    string `json:"slug"`
		FileSha string `json:"fileSha"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Slug != "newpage" || resp.FileSha == "" {
		t.Fatalf("resp = %+v, want slug=newpage with fileSha", resp)
	}
}

func TestAdminPages_CreateReservedSlug400(t *testing.T) {
	// AC-14: a reserved slug is rejected at validation (400) before any GitHub
	// call. "api" is one of the reserved words.
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	req := h.authedWrite(http.MethodPost, "/api/admin/pages", adminPageBody("api", "Reserved", true, false))
	if rr := h.serve(req); rr.Code != http.StatusBadRequest {
		t.Fatalf("reserved slug = %d, want 400", rr.Code)
	}
	if h.gh.RequestCount() != 0 {
		t.Errorf("reserved slug must not reach GitHub, got %d requests", h.gh.RequestCount())
	}
}

func TestAdminPages_CreateFieldLimits400(t *testing.T) {
	// AC-30: title over the cap is a 400.
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	longTitle := strings.Repeat("a", 201)
	req := h.authedWrite(http.MethodPost, "/api/admin/pages", adminPageBody("longtitle", longTitle, true, false))
	if rr := h.serve(req); rr.Code != http.StatusBadRequest {
		t.Fatalf("over-long title = %d, want 400", rr.Code)
	}
}

func TestAdminPages_CreateMalformed400(t *testing.T) {
	// AC-10: a syntactically invalid body is a 400.
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	req := h.authedWrite(http.MethodPost, "/api/admin/pages", "{not json")
	if rr := h.serve(req); rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed body = %d, want 400", rr.Code)
	}
}

func TestAdminPages_CreateCollision409(t *testing.T) {
	// ErrPageExists -> 409 "page already exists". "dsa" is already cached, so the
	// create short-circuits before any GitHub call.
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	req := h.authedWrite(http.MethodPost, "/api/admin/pages", adminPageBody("dsa", "Dup", true, true))
	rr := h.serve(req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("collision = %d, want 409 (body=%s)", rr.Code, rr.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body["error"] != "page already exists" {
		t.Errorf("error = %q, want \"page already exists\"", body["error"])
	}
}

func TestAdminPages_UpdateSlugImmutable400(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	// Body slug differs from the path slug.
	req := h.authedWrite(http.MethodPut, "/api/admin/pages/dsa", adminPageBody("other", "Other", true, true))
	if rr := h.serve(req); rr.Code != http.StatusBadRequest {
		t.Fatalf("slug mismatch = %d, want 400", rr.Code)
	}
}

func TestAdminPages_DeleteSuccess(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, writesEnabled: true})
	// A GitHub 404 on delete is idempotent success; the handler returns 200.
	h.gh.OnDeleteContents("pages/scratch.json", testutil.GHStatus(http.StatusNotFound))
	req := h.authedWrite(http.MethodDelete, "/api/admin/pages/scratch", "")
	rr := h.serve(req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete page = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var body map[string]bool
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if !body["deleted"] {
		t.Errorf("delete response = %s, want {deleted:true}", rr.Body.String())
	}
}
