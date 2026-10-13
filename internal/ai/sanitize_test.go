package ai

import (
	"encoding/json"
	"testing"
)

// rawBlock is a small helper to build a RawBlock from a type + data map.
func rawBlock(typ string, data map[string]any) RawBlock {
	raw, _ := json.Marshal(data)
	return RawBlock{Type: typ, Data: raw}
}

// decodeHeader pulls text/level back out of a sanitized header block.
func decodeData(t *testing.T, b Block) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b.Data, &m); err != nil {
		t.Fatalf("decode block data: %v", err)
	}
	return m
}

func TestSanitizeDraft_SlugAndFields(t *testing.T) {
	cand, err := SanitizeDraft("m1", Draft{
		Title:   "  A Great Title ",
		Summary: " A summary. ",
		Slug:    "Hello World! Foo",
		Tags:    []string{"Go", "go", " Web ", ""},
		Blocks:  []RawBlock{rawBlock("paragraph", map[string]any{"text": "Body text."})},
	})
	if err != nil {
		t.Fatalf("SanitizeDraft: %v", err)
	}
	if cand.Model != "m1" {
		t.Errorf("model = %q", cand.Model)
	}
	if cand.Title != "A Great Title" {
		t.Errorf("title = %q, want trimmed", cand.Title)
	}
	if cand.Slug != "hello-world-foo" {
		t.Errorf("slug = %q, want hello-world-foo", cand.Slug)
	}
	// Tags: trimmed, lowercased, de-duplicated.
	if len(cand.Tags) != 2 || cand.Tags[0] != "go" || cand.Tags[1] != "web" {
		t.Errorf("tags = %v, want [go web]", cand.Tags)
	}
	if cand.Content.Version == "" || cand.Content.Time == 0 {
		t.Errorf("content envelope not stamped: %+v", cand.Content)
	}
}

func TestSanitizeDraft_SlugFallbackToTitle(t *testing.T) {
	cand, err := SanitizeDraft("m", Draft{
		Title:  "Fallback Title",
		Slug:   "!!!", // slugifies to empty
		Blocks: []RawBlock{rawBlock("paragraph", map[string]any{"text": "x"})},
	})
	if err != nil {
		t.Fatalf("SanitizeDraft: %v", err)
	}
	if cand.Slug != "fallback-title" {
		t.Errorf("slug = %q, want fallback-title", cand.Slug)
	}
}

func TestSanitizeDraft_DropsUnsupportedAndEmptyBlocks(t *testing.T) {
	cand, err := SanitizeDraft("m", Draft{
		Title: "T",
		Slug:  "t",
		Blocks: []RawBlock{
			rawBlock("image", map[string]any{"file": map[string]any{"url": "x.png"}}), // unsupported -> dropped
			rawBlock("paragraph", map[string]any{"text": "  "}),                       // empty -> dropped
			rawBlock("header", map[string]any{"text": "Section", "level": 9}),         // level clamped to 3
			rawBlock("paragraph", map[string]any{"text": "Keep me"}),
			rawBlock("list", map[string]any{"style": "ordered", "items": []any{}}), // empty list -> dropped
			rawBlock("delimiter", map[string]any{}),
		},
	})
	if err != nil {
		t.Fatalf("SanitizeDraft: %v", err)
	}
	// header + paragraph + delimiter survive; image, empty paragraph, empty list dropped.
	if len(cand.Content.Blocks) != 3 {
		t.Fatalf("surviving blocks = %d, want 3: %+v", len(cand.Content.Blocks), cand.Content.Blocks)
	}
	if cand.Content.Blocks[0].Type != "header" {
		t.Errorf("block[0] = %q, want header", cand.Content.Blocks[0].Type)
	}
	hdr := decodeData(t, cand.Content.Blocks[0])
	if hdr["level"].(float64) != 3 {
		t.Errorf("header level = %v, want clamped to 3", hdr["level"])
	}
	if cand.Content.Blocks[2].Type != "delimiter" {
		t.Errorf("block[2] = %q, want delimiter", cand.Content.Blocks[2].Type)
	}
}

func TestSanitizeDraft_NoValidBlocksIsError(t *testing.T) {
	_, err := SanitizeDraft("m", Draft{
		Title:  "T",
		Slug:   "t",
		Blocks: []RawBlock{rawBlock("image", map[string]any{"file": map[string]any{"url": "x.png"}})},
	})
	if err == nil {
		t.Fatal("expected an error when no supported blocks survive")
	}
}

func TestSanitizeDraft_ListItemsNormalized(t *testing.T) {
	cand, err := SanitizeDraft("m", Draft{
		Title: "T",
		Slug:  "t",
		Blocks: []RawBlock{
			rawBlock("list", map[string]any{
				"style": "unordered",
				// Mixed: plain strings and the nested-list object form.
				"items": []any{"one", map[string]any{"content": "two"}, "  ", map[string]any{"content": ""}},
			}),
		},
	})
	if err != nil {
		t.Fatalf("SanitizeDraft: %v", err)
	}
	data := decodeData(t, cand.Content.Blocks[0])
	items, _ := data["items"].([]any)
	if len(items) != 2 || items[0] != "one" || items[1] != "two" {
		t.Errorf("list items = %v, want [one two]", items)
	}
}

func TestNormalizeLang(t *testing.T) {
	cases := map[string]string{
		"go": "go", "Go": "go", "golang": "go", "js": "javascript",
		"TS": "typescript", "sh": "bash", "": "plaintext", "cobol": "plaintext",
	}
	for in, want := range cases {
		if got := normalizeLang(in); got != want {
			t.Errorf("normalizeLang(%q) = %q, want %q", in, got, want)
		}
	}
}
