package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
)

// Renderer converts an Editor.js document into sanitized HTML and extracts
// plain text for the search index. It is safe for concurrent use once built.
type Renderer struct {
	policy    *bluemonday.Policy
	stripper  *bluemonday.Policy
	style     *chroma.Style
	formatter *chromahtml.Formatter
}

// NewRenderer builds the render pipeline: a bluemonday policy that preserves the
// markup the block renderer emits, a strict stripper for the FTS plaintext, and
// a chroma highlighter (github style, emitting CSS classes) whose classes match
// web/src/styles/code.css.
func NewRenderer() *Renderer {
	return &Renderer{
		policy:   newSanitizePolicy(),
		stripper: bluemonday.StrictPolicy(),
		// styles.Get never returns nil (chroma falls back internally).
		style:     styles.Get("github"),
		formatter: chromahtml.New(chromahtml.WithClasses(true)),
	}
}

// RenderDocument renders every supported block to HTML, sanitizes the whole
// result through the bluemonday policy, and returns the sanitized HTML plus a
// plain-text projection of the document (for FTS). Unknown block types are
// logged and skipped; a bad block never aborts the render.
func (r *Renderer) RenderDocument(doc EditorDocument) (sanitizedHTML, plainText string, err error) {
	var htmlBuf bytes.Buffer
	var textParts []string
	headingIDs := map[string]int{}

	for _, block := range doc.Blocks {
		switch block.Type {
		case "header":
			r.renderHeader(&htmlBuf, &textParts, block, headingIDs)
		case "paragraph":
			r.renderParagraph(&htmlBuf, &textParts, block)
		case "list":
			r.renderList(&htmlBuf, &textParts, block)
		case "quote":
			r.renderQuote(&htmlBuf, &textParts, block)
		case "code":
			r.renderCode(&htmlBuf, &textParts, block)
		case "table":
			r.renderTable(&htmlBuf, &textParts, block)
		case "image":
			r.renderImage(&htmlBuf, &textParts, block)
		case "delimiter":
			htmlBuf.WriteString("<hr>")
		default:
			slog.Warn("skipping unknown editor.js block type", "type", block.Type)
		}
	}

	sanitizedHTML = string(r.policy.SanitizeBytes(htmlBuf.Bytes()))
	plainText = collapseWS(strings.Join(textParts, " "))
	return sanitizedHTML, plainText, nil
}

// ReadingMinutes estimates reading time at ~200 words/min, rounded up.
func ReadingMinutes(plainText string) int {
	words := len(strings.Fields(plainText))
	if words == 0 {
		return 0
	}
	return (words + 199) / 200
}

// --- block renderers ---

func (r *Renderer) renderHeader(b *bytes.Buffer, text *[]string, block EditorBlock, ids map[string]int) {
	var d struct {
		Text  string `json:"text"`
		Level int    `json:"level"`
	}
	decodeBlock(block, &d)

	var tag string
	switch {
	case d.Level <= 2:
		tag = "h2"
	case d.Level == 3:
		tag = "h3"
	default: // >= 4
		tag = "h4"
	}

	id := r.uniqueHeadingID(d.Text, ids)
	b.WriteString("<" + tag + ` id="` + html.EscapeString(id) + `">`)
	b.WriteString(d.Text) // inline HTML, sanitized by the final policy pass
	b.WriteString("</" + tag + ">")
	*text = append(*text, r.plain(d.Text))
}

func (r *Renderer) renderParagraph(b *bytes.Buffer, text *[]string, block EditorBlock) {
	var d struct {
		Text string `json:"text"`
	}
	decodeBlock(block, &d)
	b.WriteString("<p>" + d.Text + "</p>")
	*text = append(*text, r.plain(d.Text))
}

func (r *Renderer) renderQuote(b *bytes.Buffer, text *[]string, block EditorBlock) {
	var d struct {
		Text    string `json:"text"`
		Caption string `json:"caption"`
	}
	decodeBlock(block, &d)
	b.WriteString("<blockquote><p>" + d.Text + "</p>")
	if strings.TrimSpace(d.Caption) != "" {
		b.WriteString("<cite>" + d.Caption + "</cite>")
		*text = append(*text, r.plain(d.Caption))
	}
	b.WriteString("</blockquote>")
	*text = append(*text, r.plain(d.Text))
}

func (r *Renderer) renderList(b *bytes.Buffer, text *[]string, block EditorBlock) {
	var d struct {
		Style string            `json:"style"`
		Items []json.RawMessage `json:"items"`
	}
	decodeBlock(block, &d)
	tag := "ul"
	if d.Style == "ordered" {
		tag = "ol"
	}
	b.WriteString("<" + tag + ">")
	r.renderListItems(b, text, d.Items, tag)
	b.WriteString("</" + tag + ">")
}

// renderListItems renders Editor.js list items. The nested-list tool emits
// {content, items:[…]} objects; older lists emit plain strings. Both are
// tolerated. Nested lists reuse the parent list's style.
func (r *Renderer) renderListItems(b *bytes.Buffer, text *[]string, items []json.RawMessage, tag string) {
	for _, raw := range items {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			b.WriteString("<li>" + s + "</li>")
			*text = append(*text, r.plain(s))
			continue
		}
		var item struct {
			Content string            `json:"content"`
			Items   []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		b.WriteString("<li>" + item.Content)
		*text = append(*text, r.plain(item.Content))
		if len(item.Items) > 0 {
			b.WriteString("<" + tag + ">")
			r.renderListItems(b, text, item.Items, tag)
			b.WriteString("</" + tag + ">")
		}
		b.WriteString("</li>")
	}
}

func (r *Renderer) renderCode(b *bytes.Buffer, text *[]string, block EditorBlock) {
	var d struct {
		Code     string `json:"code"`
		Language string `json:"language"`
	}
	decodeBlock(block, &d)
	r.highlightCode(b, d.Code, d.Language)
	// Index the raw code verbatim so identifiers inside it are searchable.
	*text = append(*text, d.Code)
}

// highlightCode writes a chroma-highlighted <pre class="chroma"> block whose
// highlighter/style/classes match web/src/styles/code.css. An empty or unknown
// language falls back to a plain (neutral) lexer.
func (r *Renderer) highlightCode(b *bytes.Buffer, code, language string) {
	lx := lexers.Get(strings.TrimSpace(language))
	if lx == nil {
		lx = lexers.Fallback
	}
	lx = chroma.Coalesce(lx)
	// Append a trailing newline so the highlighted block ends on its own line
	// (chroma's standard fenced-block output), keeping the markup stable.
	it, err := lx.Tokenise(nil, code+"\n")
	if err != nil {
		b.WriteString("<pre class=\"chroma\"><code>" + html.EscapeString(code) + "</code></pre>")
		return
	}
	if err := r.formatter.Format(b, r.style, it); err != nil {
		b.WriteString("<pre class=\"chroma\"><code>" + html.EscapeString(code) + "</code></pre>")
	}
}

func (r *Renderer) renderTable(b *bytes.Buffer, text *[]string, block EditorBlock) {
	var d struct {
		WithHeadings bool       `json:"withHeadings"`
		Content      [][]string `json:"content"`
	}
	decodeBlock(block, &d)
	if len(d.Content) == 0 {
		return
	}
	b.WriteString("<table>")
	start := 0
	if d.WithHeadings {
		b.WriteString("<thead><tr>")
		for _, cell := range d.Content[0] {
			b.WriteString("<th>" + cell + "</th>")
			*text = append(*text, r.plain(cell))
		}
		b.WriteString("</tr></thead>")
		start = 1
	}
	b.WriteString("<tbody>")
	for _, row := range d.Content[start:] {
		b.WriteString("<tr>")
		for _, cell := range row {
			b.WriteString("<td>" + cell + "</td>")
			*text = append(*text, r.plain(cell))
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table>")
}

func (r *Renderer) renderImage(b *bytes.Buffer, text *[]string, block EditorBlock) {
	var d struct {
		File struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"file"`
		Caption string `json:"caption"`
	}
	decodeBlock(block, &d)
	if strings.TrimSpace(d.File.URL) == "" {
		return
	}
	alt := r.plain(d.Caption)
	b.WriteString("<figure><img src=\"" + html.EscapeString(d.File.URL) + "\"")
	b.WriteString(" alt=\"" + html.EscapeString(alt) + "\" loading=\"lazy\"")
	if d.File.Width > 0 {
		b.WriteString(" width=\"" + strconv.Itoa(d.File.Width) + "\"")
	}
	if d.File.Height > 0 {
		b.WriteString(" height=\"" + strconv.Itoa(d.File.Height) + "\"")
	}
	b.WriteString(">")
	if strings.TrimSpace(d.Caption) != "" {
		b.WriteString("<figcaption>" + d.Caption + "</figcaption>")
		*text = append(*text, r.plain(d.Caption))
	}
	b.WriteString("</figure>")
}

// --- helpers ---

// decodeBlock unmarshals a block's raw data into v, ignoring errors so a
// malformed block renders as its zero value rather than aborting the document.
func decodeBlock(block EditorBlock, v any) {
	if len(block.Data) == 0 {
		return
	}
	_ = json.Unmarshal(block.Data, v)
}

// plain strips inline markup and unescapes entities so FTS text has no HTML.
func (r *Renderer) plain(s string) string {
	return html.UnescapeString(string(r.stripper.SanitizeBytes([]byte(s))))
}

// uniqueHeadingID derives an anchor id from heading text, de-duplicated within
// a document with -2, -3, … suffixes.
func (r *Renderer) uniqueHeadingID(text string, ids map[string]int) string {
	base := Slugify(r.plain(text))
	if base == "" {
		base = "section"
	}
	n := ids[base]
	ids[base] = n + 1
	if n == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n+1)
}
