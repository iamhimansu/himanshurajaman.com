package content_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
)

// blk builds an EditorBlock with the given type and data (marshaled to raw JSON).
func blk(t *testing.T, typ string, data any) content.EditorBlock {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal block data: %v", err)
	}
	return content.EditorBlock{Type: typ, Data: raw}
}

func doc(blocks ...content.EditorBlock) content.EditorDocument {
	return content.EditorDocument{Blocks: blocks}
}

func renderHTML(t *testing.T, d content.EditorDocument) string {
	t.Helper()
	html, _, err := content.NewRenderer().RenderDocument(d)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	return html
}

func TestRenderDocument_Blocks(t *testing.T) {
	t.Run("header levels and anchor ids", func(t *testing.T) {
		html := renderHTML(t, doc(
			blk(t, "header", map[string]any{"text": "Intro Title", "level": 1}),
			blk(t, "header", map[string]any{"text": "Sub Title", "level": 3}),
			blk(t, "header", map[string]any{"text": "Deep Title", "level": 5}),
		))
		// level<=2 -> h2, level 3 -> h3, level>=4 -> h4.
		for _, want := range []string{
			`<h2 id="intro-title">Intro Title</h2>`,
			`<h3 id="sub-title">Sub Title</h3>`,
			`<h4 id="deep-title">Deep Title</h4>`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("missing %q in:\n%s", want, html)
			}
		}
	})

	t.Run("duplicate heading ids are de-duplicated", func(t *testing.T) {
		html := renderHTML(t, doc(
			blk(t, "header", map[string]any{"text": "Notes", "level": 2}),
			blk(t, "header", map[string]any{"text": "Notes", "level": 2}),
		))
		if !strings.Contains(html, `id="notes"`) || !strings.Contains(html, `id="notes-2"`) {
			t.Errorf("expected de-duplicated ids notes / notes-2, got:\n%s", html)
		}
	})

	t.Run("paragraph", func(t *testing.T) {
		html := renderHTML(t, doc(blk(t, "paragraph", map[string]any{"text": "Just a line."})))
		if !strings.Contains(html, "<p>Just a line.</p>") {
			t.Errorf("paragraph not rendered: %s", html)
		}
	})

	t.Run("unordered and ordered lists with nesting", func(t *testing.T) {
		ul := renderHTML(t, doc(blk(t, "list", map[string]any{
			"style": "unordered",
			"items": []any{"one", "two"},
		})))
		if !strings.Contains(ul, "<ul><li>one</li><li>two</li></ul>") {
			t.Errorf("unordered list: %s", ul)
		}
		ol := renderHTML(t, doc(blk(t, "list", map[string]any{
			"style": "ordered",
			"items": []any{
				map[string]any{"content": "parent", "items": []any{"child"}},
			},
		})))
		if !strings.Contains(ol, "<ol><li>parent<ol><li>child</li></ol></li></ol>") {
			t.Errorf("nested ordered list: %s", ol)
		}
	})

	t.Run("quote with caption", func(t *testing.T) {
		html := renderHTML(t, doc(blk(t, "quote", map[string]any{
			"text": "To be, or not to be.", "caption": "Hamlet",
		})))
		if !strings.Contains(html, "<blockquote><p>To be, or not to be.</p>") ||
			!strings.Contains(html, "<cite>Hamlet</cite>") {
			t.Errorf("quote not rendered: %s", html)
		}
	})

	t.Run("table with headings", func(t *testing.T) {
		html := renderHTML(t, doc(blk(t, "table", map[string]any{
			"withHeadings": true,
			"content":      [][]string{{"H1", "H2"}, {"a", "b"}},
		})))
		for _, want := range []string{"<thead><tr><th>H1</th><th>H2</th></tr></thead>", "<tbody><tr><td>a</td><td>b</td></tr></tbody>"} {
			if !strings.Contains(html, want) {
				t.Errorf("missing %q in table:\n%s", want, html)
			}
		}
	})

	t.Run("image figure with dimensions and caption", func(t *testing.T) {
		html := renderHTML(t, doc(blk(t, "image", map[string]any{
			"file":    map[string]any{"url": "/images/x.png", "width": 640, "height": 480},
			"caption": "A caption",
		})))
		for _, want := range []string{
			`<figure>`, `src="/images/x.png"`, `alt="A caption"`, `loading="lazy"`,
			`width="640"`, `height="480"`, `<figcaption>A caption</figcaption>`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("missing %q in image figure:\n%s", want, html)
			}
		}
	})

	t.Run("delimiter renders hr", func(t *testing.T) {
		html := renderHTML(t, doc(blk(t, "delimiter", map[string]any{})))
		if !strings.Contains(html, "<hr>") {
			t.Errorf("delimiter should render <hr>, got: %s", html)
		}
	})

	t.Run("unknown block type is skipped without aborting", func(t *testing.T) {
		html := renderHTML(t, doc(
			blk(t, "mystery", map[string]any{"foo": "bar"}),
			blk(t, "paragraph", map[string]any{"text": "after unknown"}),
		))
		if !strings.Contains(html, "<p>after unknown</p>") {
			t.Errorf("blocks after an unknown type should still render: %s", html)
		}
	})
}

func TestRenderDocument_InlineFormatting(t *testing.T) {
	html := renderHTML(t, doc(blk(t, "paragraph", map[string]any{
		"text": `Mix of <b>bold</b>, <i>italic</i>, a <a href="https://example.com">link</a>, ` +
			`and <code class="inline-code">inline</code>.`,
	})))
	for _, want := range []string{
		"<b>bold</b>", "<i>italic</i>", `href="https://example.com"`, ">link</a>",
		`<code class="inline-code">inline</code>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("inline formatting lost %q in:\n%s", want, html)
		}
	}
}

func TestRenderDocument_CodeHighlightChroma(t *testing.T) {
	html := renderHTML(t, doc(blk(t, "code", map[string]any{
		"code":     "package main\n\nfunc main() {}",
		"language": "go",
	})))
	if !strings.Contains(html, `class="chroma"`) {
		t.Errorf("expected chroma wrapper class, got:\n%s", html)
	}
	if !strings.Contains(html, "<span class=") {
		t.Errorf("expected chroma token spans, got:\n%s", html)
	}
	// The highlighted source text survives (sanitizer keeps the class spans).
	if !strings.Contains(html, "package") || !strings.Contains(html, "main") {
		t.Errorf("highlighted code text missing, got:\n%s", html)
	}
}

func TestRenderDocument_SanitizesInlineScript(t *testing.T) {
	html := renderHTML(t, doc(blk(t, "paragraph", map[string]any{
		"text": `Safe text <script>alert('xss')</script> ` +
			`<a href="javascript:alert(1)">bad link</a> ` +
			`<span onclick="evil()">clicky</span>`,
	})))
	for _, forbidden := range []string{"<script", "onclick", "javascript:"} {
		if strings.Contains(strings.ToLower(html), forbidden) {
			t.Errorf("sanitized HTML still contains %q:\n%s", forbidden, html)
		}
	}
	if !strings.Contains(html, "Safe text") {
		t.Errorf("legitimate text was dropped: %s", html)
	}
}

func TestRenderDocument_PlainTextStripsMarkup(t *testing.T) {
	_, plain, err := content.NewRenderer().RenderDocument(doc(
		blk(t, "paragraph", map[string]any{"text": "<b>bold</b> &amp; stuff"}),
		blk(t, "code", map[string]any{"code": "funcName := 42", "language": "go"}),
	))
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	if strings.Contains(plain, "<b>") || strings.Contains(plain, "&amp;") {
		t.Errorf("plain text retains markup/entities: %q", plain)
	}
	if !strings.Contains(plain, "bold & stuff") {
		t.Errorf("plain text should unescape entities: %q", plain)
	}
	if !strings.Contains(plain, "funcName := 42") {
		t.Errorf("plain text should index raw code verbatim: %q", plain)
	}
}

func TestReadingMinutes(t *testing.T) {
	tests := []struct {
		name  string
		words int
		want  int
	}{
		{"empty", 0, 0},
		{"one word", 1, 1},
		{"exactly 200", 200, 1},
		{"201 rounds up", 201, 2},
		{"exactly 400", 400, 2},
		{"401 rounds up", 401, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := strings.TrimSpace(strings.Repeat("word ", tt.words))
			if got := content.ReadingMinutes(text); got != tt.want {
				t.Errorf("ReadingMinutes(%d words) = %d, want %d", tt.words, got, tt.want)
			}
		})
	}
}
