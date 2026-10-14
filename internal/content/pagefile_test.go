package content_test

import (
	"encoding/json"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
)

const validPageJSON = `{
  "meta": {
    "title": "Data Structures",
    "slug": "dsa",
    "nav": true,
    "navLabel": "DSA",
    "navOrder": 10,
    "published": true,
    "summary": "A page about DSA.",
    "updated": "2024-03-05T12:30:00+05:30"
  },
  "content": {"blocks": [{"type":"paragraph","data":{"text":"hi"}}]}
}`

func TestParsePage_Valid(t *testing.T) {
	p, err := content.ParsePage("pages/dsa.json", []byte(validPageJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Title != "Data Structures" || p.Slug != "dsa" {
		t.Errorf("unexpected title/slug: %q/%q", p.Title, p.Slug)
	}
	if !p.Nav || p.NavLabel != "DSA" || p.NavOrder != 10 || !p.Published {
		t.Errorf("unexpected nav fields: %+v", p)
	}
	if p.Summary != "A page about DSA." {
		t.Errorf("summary = %q", p.Summary)
	}
	// Updated normalized to UTC (the +05:30 offset becomes 07:00Z).
	if p.Updated != "2024-03-05T07:00:00Z" {
		t.Errorf("updated not normalized to UTC: %q", p.Updated)
	}
}

func TestParsePage_SlugErrors(t *testing.T) {
	invalid := []struct {
		name string
		path string
		src  string
	}{
		{"missing title", "pages/x.json", `{"meta":{"slug":"x"},"content":{}}`},
		{"missing slug", "pages/x.json", `{"meta":{"title":"T"},"content":{}}`},
		{"bad slug shape", "pages/Bad_Slug.json", `{"meta":{"title":"T","slug":"Bad_Slug"},"content":{}}`},
		{"slug not equal to stem", "pages/other.json", `{"meta":{"title":"T","slug":"dsa"},"content":{}}`},
		{"bad updated", "pages/x.json", `{"meta":{"title":"T","slug":"x","updated":"nope"},"content":{}}`},
		{"malformed json", "pages/x.json", `{not json`},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := content.ParsePage(tt.path, []byte(tt.src)); err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

// AC-14: every reserved word is rejected as a page slug (case-insensitive).
func TestParsePage_ReservedSlugRejected(t *testing.T) {
	reserved := []string{
		"blog", "resume", "works", "contact", "admin", "api", "rss",
		"sitemap", "robots", "healthz", "search", "auth", "webhook",
		"images", "assets",
	}
	if len(reserved) != len(content.ReservedSlugs) {
		t.Fatalf("reserved test list (%d) out of sync with ReservedSlugs (%d)", len(reserved), len(content.ReservedSlugs))
	}
	for _, slug := range reserved {
		t.Run(slug, func(t *testing.T) {
			src := `{"meta":{"title":"T","slug":"` + slug + `"},"content":{}}`
			if _, err := content.ParsePage("pages/"+slug+".json", []byte(src)); err == nil {
				t.Errorf("expected reserved slug %q to be rejected", slug)
			}
			if !content.IsReservedSlug(slug) {
				t.Errorf("IsReservedSlug(%q) = false, want true", slug)
			}
		})
	}
	// Case/whitespace insensitivity.
	if !content.IsReservedSlug("  ADMIN ") {
		t.Error("IsReservedSlug should trim+lowercase")
	}
	if content.IsReservedSlug("dsa") {
		t.Error("a normal slug must not be reserved")
	}
}

func TestParsePage_OptionalFields(t *testing.T) {
	// summary and updated omitted entirely: both resolve to "".
	src := `{"meta":{"title":"T","slug":"minimal","nav":false,"published":false},"content":{"blocks":[]}}`
	p, err := content.ParsePage("pages/minimal.json", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Summary != "" || p.Updated != "" {
		t.Errorf("optional fields should be empty, got summary=%q updated=%q", p.Summary, p.Updated)
	}
	if p.Nav || p.Published {
		t.Errorf("nav/published should be false, got %+v", p)
	}
}

func TestParsePage_NavOrderDefaultAndExplicitZero(t *testing.T) {
	// Absent navOrder -> NavOrderDefault.
	src := `{"meta":{"title":"T","slug":"a","nav":true,"published":true},"content":{}}`
	p, err := content.ParsePage("pages/a.json", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.NavOrder != content.NavOrderDefault {
		t.Errorf("absent navOrder = %d, want %d", p.NavOrder, content.NavOrderDefault)
	}

	// Explicit 0 is preserved (distinguishable from absent).
	src0 := `{"meta":{"title":"T","slug":"b","nav":true,"navOrder":0,"published":true},"content":{}}`
	p0, err := content.ParsePage("pages/b.json", []byte(src0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p0.NavOrder != 0 {
		t.Errorf("explicit navOrder 0 = %d, want 0", p0.NavOrder)
	}
}

func TestParsePage_EmptyContentDefaults(t *testing.T) {
	src := `{"meta":{"title":"T","slug":"empty-page","nav":false,"published":false},"content":null}`
	p, err := content.ParsePage("pages/empty-page.json", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Document.Blocks == nil || len(p.Document.Blocks) != 0 {
		t.Errorf("expected empty block slice, got %+v", p.Document.Blocks)
	}
	if string(p.DocumentJSON) != `{"blocks":[]}` {
		t.Errorf("empty content should canonicalize to {\"blocks\":[]}, got %q", p.DocumentJSON)
	}
}

func TestAssemblePageJSON_RoundTrip(t *testing.T) {
	cases := []content.PageMeta{
		{Title: "DSA", Slug: "dsa", Nav: true, NavLabel: "DSA", NavOrder: ptrInt(10), Published: true, Summary: "s", Updated: "2024-03-05T12:30:00Z"},
		// Minimal page: nav:false, published:false, all optionals omitted.
		{Title: "Scratch", Slug: "scratch", Nav: false, Published: false},
		// nav:true but no label/order, no summary/updated.
		{Title: "Notes", Slug: "notes", Nav: true, Published: true},
	}
	content0 := json.RawMessage(`{"blocks":[{"type":"paragraph","data":{"text":"body"}}]}`)
	for _, meta := range cases {
		t.Run(meta.Slug, func(t *testing.T) {
			data, err := content.AssemblePageJSON(meta, content0)
			if err != nil {
				t.Fatalf("assemble: %v", err)
			}
			p, err := content.ParsePage("pages/"+meta.Slug+".json", data)
			if err != nil {
				t.Fatalf("reparse: %v", err)
			}
			if p.Title != meta.Title || p.Slug != meta.Slug {
				t.Errorf("title/slug drift: %q/%q", p.Title, p.Slug)
			}
			if p.Nav != meta.Nav || p.Published != meta.Published {
				t.Errorf("nav/published drift: got %v/%v", p.Nav, p.Published)
			}
			wantOrder := content.NavOrderDefault
			if meta.NavOrder != nil {
				wantOrder = *meta.NavOrder
			}
			if p.NavOrder != wantOrder {
				t.Errorf("navOrder drift: got %d want %d", p.NavOrder, wantOrder)
			}
			if p.NavLabel != meta.NavLabel {
				t.Errorf("navLabel drift: got %q want %q", p.NavLabel, meta.NavLabel)
			}
			if p.Summary != meta.Summary {
				t.Errorf("summary drift: got %q want %q", p.Summary, meta.Summary)
			}
		})
	}
}

func ptrInt(n int) *int { return &n }
