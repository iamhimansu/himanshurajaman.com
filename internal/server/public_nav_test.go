package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

type navItem struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

func decodeNav(t *testing.T, rr *httptest.ResponseRecorder) []navItem {
	t.Helper()
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &settings); err != nil {
		t.Fatalf("decode settings: %v (body=%s)", err, rr.Body.String())
	}
	raw, ok := settings["nav"]
	if !ok {
		t.Fatalf("settings has no nav key:\n%s", rr.Body.String())
	}
	var nav []navItem
	if err := json.Unmarshal(raw, &nav); err != nil {
		t.Fatalf("decode nav: %v (raw=%s)", err, string(raw))
	}
	return nav
}

var coreNav = []navItem{
	{Label: "Home", Path: "/"},
	{Label: "Works", Path: "/works"},
	{Label: "Blog", Path: "/blog"},
	{Label: "Resume", Path: "/resume"},
	{Label: "Contact", Path: "/contact"},
}

func assertCorePrefix(t *testing.T, nav []navItem) {
	t.Helper()
	if len(nav) < len(coreNav) {
		t.Fatalf("nav too short: %+v", nav)
	}
	for i, want := range coreNav {
		if nav[i] != want {
			t.Fatalf("nav[%d] = %+v, want %+v (full=%+v)", i, nav[i], want, nav)
		}
	}
}

func TestSettingsNav_CorePlusPagesOrdered(t *testing.T) {
	// AC-11: core five, then published nav pages ordered by navOrder then title.
	// Default fixtures include a published nav page "dsa" (navOrder 10). Add two
	// more to exercise ordering: one with a lower navOrder, one that ties on
	// navOrder and must break by title.
	h := newServer(t, options{})
	seedNavPage(t, h, "alpha", "Alpha", "", true, true, ptrInt(5))
	seedNavPage(t, h, "zed", "Zeta", "", true, true, ptrInt(10)) // ties with dsa(10) on order

	rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	nav := decodeNav(t, rr)
	assertCorePrefix(t, nav)

	pages := nav[len(coreNav):]
	want := []navItem{
		{Label: "Alpha", Path: "/alpha"},  // navOrder 5
		{Label: "DSA", Path: "/dsa"},       // navOrder 10, title "DSA"
		{Label: "Zeta", Path: "/zed"},      // navOrder 10, title "Zeta"
	}
	if len(pages) != len(want) {
		t.Fatalf("page nav items = %+v, want %+v", pages, want)
	}
	for i := range want {
		if pages[i] != want[i] {
			t.Fatalf("page nav[%d] = %+v, want %+v (full=%+v)", i, pages[i], want[i], pages)
		}
	}
}

func TestSettingsNav_ExcludesHiddenAndDraft(t *testing.T) {
	// AC-12: nav:false and unpublished pages never appear in the computed nav.
	h := newServer(t, options{})
	seedNavPage(t, h, "hidden", "Hidden", "", false, true, ptrInt(1))  // nav:false
	seedNavPage(t, h, "wip", "Work In Progress", "", true, false, ptrInt(2)) // draft

	nav := decodeNav(t, h.serve(httptest.NewRequest(http.MethodGet, "/api/settings", nil)))
	for _, it := range nav {
		if it.Path == "/hidden" || it.Path == "/wip" {
			t.Fatalf("nav must exclude hidden/draft pages, got %+v", nav)
		}
	}
}

func TestSettingsNav_IgnoresRawSiteNav(t *testing.T) {
	// AC-13: a raw nav value in the ingested site.json is overwritten by the
	// server-computed nav. Build a content tree whose settings carry a bogus nav.
	cd := testutil.DefaultContentDir()
	cd.Settings = settingsWithNav(t)
	h := newServer(t, options{contentDir: &cd})

	nav := decodeNav(t, h.serve(httptest.NewRequest(http.MethodGet, "/api/settings", nil)))
	assertCorePrefix(t, nav)
	for _, it := range nav {
		if it.Path == "/bogus" {
			t.Fatalf("computed nav must ignore raw site.json nav, got %+v", nav)
		}
	}
}

// settingsWithNav returns a valid site.json that also carries a stray nav array
// the server must ignore.
func settingsWithNav(t *testing.T) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(testutil.SettingsJSON(), &m); err != nil {
		t.Fatalf("decode default settings: %v", err)
	}
	m["nav"] = json.RawMessage(`[{"label":"Bogus","path":"/bogus"}]`)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	return b
}

func TestSettingsNav_NoPagesCoreOnlyNever500(t *testing.T) {
	// AC-15: with no nav pages present the computed nav is exactly the core five
	// and the endpoint never 500s. Build a content tree with no pages/ dir.
	cd := testutil.DefaultContentDir()
	cd.Pages = nil
	h := newServer(t, options{contentDir: &cd})

	rr := h.serve(httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("settings status = %d, want 200", rr.Code)
	}
	nav := decodeNav(t, rr)
	if len(nav) != len(coreNav) {
		t.Fatalf("nav with no pages = %+v, want core five only", nav)
	}
	assertCorePrefix(t, nav)
}

func seedNavPage(t *testing.T, h *harness, slug, title, navLabel string, nav, published bool, navOrder *int) {
	t.Helper()
	order := content.NavOrderDefault
	if navOrder != nil {
		order = *navOrder
	}
	err := h.store.UpsertPage(context.Background(), content.Page{
		Slug: slug, Title: title, Nav: nav, NavLabel: navLabel, NavOrder: order,
		Published: published, HTML: "<p>x</p>", Document: `{"blocks":[]}`,
		FilePath: "pages/" + slug + ".json", FileSHA: "sha-" + slug,
	})
	if err != nil {
		t.Fatalf("seed nav page %s: %v", slug, err)
	}
}

func ptrInt(n int) *int { return &n }
