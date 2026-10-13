package ai

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
)

// editorJSVersion stamps the generated content envelope. It matches the
// @editorjs/editorjs version the frontend bundles (web/package.json); any "2.x"
// is accepted by the editor, but keeping it in sync avoids a migration notice.
const editorJSVersion = "2.31.7"

// Field caps mirror the admin post limits (internal/server/admin_handlers.go) so
// a generated draft fills the form with values that already pass validation.
const (
	maxTitleChars   = 200
	maxSummaryChars = 500
	maxSlugChars    = 100
	maxTagChars     = 40
	maxTags         = 20
)

// Draft is the raw JSON object the model is instructed to return. Blocks are
// kept loosely typed (RawBlock) because json_object mode does not guarantee the
// schema; SanitizeDraft validates and normalizes them.
type Draft struct {
	Title   string     `json:"title"`
	Summary string     `json:"summary"`
	Slug    string     `json:"slug"`
	Tags    []string   `json:"tags"`
	Blocks  []RawBlock `json:"blocks"`
}

// RawBlock is a block as received from the model: a type plus an opaque data
// payload decoded per type during sanitization.
type RawBlock struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Candidate is a sanitized draft for one model, ready to hand to the editor.
// Content is a full Editor.js OutputData envelope ({time, blocks, version}).
type Candidate struct {
	Model   string   `json:"model"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Slug    string   `json:"slug"`
	Tags    []string `json:"tags"`
	Content Document `json:"content"`
}

// Document is the Editor.js content envelope returned to the client.
type Document struct {
	Time    int64   `json:"time"`
	Blocks  []Block `json:"blocks"`
	Version string  `json:"version"`
}

// Block is one sanitized Editor.js block ({type, data}).
type Block struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// SanitizeDraft normalizes a model's raw Draft into a Candidate: it slugifies the
// slug to ^[a-z0-9]+(?:-[a-z0-9]+)*$, clips text fields to the admin limits,
// de-duplicates/caps tags, and drops any block whose type/shape is not in the
// supported set. If no valid blocks survive it returns an error so the caller
// records the model as a failure rather than a candidate.
func SanitizeDraft(model string, d Draft) (Candidate, error) {
	blocks := sanitizeBlocks(d.Blocks)
	if len(blocks) == 0 {
		return Candidate{}, errors.New("model returned no usable content blocks")
	}

	slug := content.Slugify(d.Slug)
	if slug == "" {
		slug = content.Slugify(d.Title)
	}
	if len(slug) > maxSlugChars {
		slug = strings.TrimRight(slug[:maxSlugChars], "-")
	}

	return Candidate{
		Model:   model,
		Title:   clip(strings.TrimSpace(d.Title), maxTitleChars),
		Summary: clip(strings.TrimSpace(d.Summary), maxSummaryChars),
		Slug:    slug,
		Tags:    sanitizeTags(d.Tags),
		Content: Document{
			Time:    time.Now().UnixMilli(),
			Blocks:  blocks,
			Version: editorJSVersion,
		},
	}, nil
}

// sanitizeBlocks keeps only supported, well-formed blocks and re-serializes each
// with just the fields the renderer/editor use (dropping any extras the model
// added). Image blocks and unknown types are dropped.
func sanitizeBlocks(raw []RawBlock) []Block {
	out := []Block{}
	for _, b := range raw {
		switch b.Type {
		case "header":
			var d struct {
				Text  string `json:"text"`
				Level int    `json:"level"`
			}
			_ = json.Unmarshal(b.Data, &d)
			text := strings.TrimSpace(d.Text)
			if text == "" {
				continue
			}
			level := 2
			if d.Level >= 3 {
				level = 3
			}
			out = append(out, newBlock("header", map[string]any{"text": text, "level": level}))
		case "paragraph":
			var d struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(b.Data, &d)
			if strings.TrimSpace(d.Text) == "" {
				continue
			}
			out = append(out, newBlock("paragraph", map[string]any{"text": strings.TrimSpace(d.Text)}))
		case "list":
			items := parseListItems(b.Data)
			if len(items) == 0 {
				// An empty items array would crash @editorjs/list's normalizer, so
				// a list with no usable items is dropped entirely.
				continue
			}
			var d struct {
				Style string `json:"style"`
			}
			_ = json.Unmarshal(b.Data, &d)
			style := "unordered"
			if d.Style == "ordered" {
				style = "ordered"
			}
			out = append(out, newBlock("list", map[string]any{"style": style, "items": items}))
		case "quote":
			var d struct {
				Text    string `json:"text"`
				Caption string `json:"caption"`
			}
			_ = json.Unmarshal(b.Data, &d)
			if strings.TrimSpace(d.Text) == "" {
				continue
			}
			out = append(out, newBlock("quote", map[string]any{
				"text":    strings.TrimSpace(d.Text),
				"caption": strings.TrimSpace(d.Caption),
			}))
		case "code":
			var d struct {
				Code     string `json:"code"`
				Language string `json:"language"`
			}
			_ = json.Unmarshal(b.Data, &d)
			if strings.TrimSpace(d.Code) == "" {
				continue
			}
			out = append(out, newBlock("code", map[string]any{
				"code":     d.Code,
				"language": normalizeLang(d.Language),
			}))
		case "table":
			rows := parseTable(b.Data)
			if len(rows) == 0 {
				continue
			}
			var d struct {
				WithHeadings bool `json:"withHeadings"`
			}
			_ = json.Unmarshal(b.Data, &d)
			out = append(out, newBlock("table", map[string]any{
				"withHeadings": d.WithHeadings,
				"content":      rows,
			}))
		case "delimiter":
			out = append(out, newBlock("delimiter", map[string]any{}))
		default:
			// Unsupported (e.g. "image"): drop.
		}
	}
	return out
}

// parseListItems extracts plain-string list items. It tolerates both the string
// form the prompt requests and the nested-list object form ({content|text}),
// keeping only non-empty strings.
func parseListItems(data json.RawMessage) []string {
	var d struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(data, &d)
	out := []string{}
	for _, raw := range d.Items {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
			continue
		}
		var obj struct {
			Content string `json:"content"`
			Text    string `json:"text"`
		}
		if json.Unmarshal(raw, &obj) == nil {
			c := strings.TrimSpace(obj.Content)
			if c == "" {
				c = strings.TrimSpace(obj.Text)
			}
			if c != "" {
				out = append(out, c)
			}
		}
	}
	return out
}

// parseTable extracts a rectangular-ish string grid, dropping empty rows. A
// payload whose content is not an array of string arrays yields no rows (the
// block is then dropped).
func parseTable(data json.RawMessage) [][]string {
	var d struct {
		Content [][]string `json:"content"`
	}
	if json.Unmarshal(data, &d) != nil {
		return nil
	}
	rows := [][]string{}
	for _, row := range d.Content {
		if len(row) == 0 {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// codeLanguages is the set the editor's code tool exposes (web/.../codeTool.ts).
// Keeping the generated language within this set means the editor's language
// <select> shows the right option; chroma (server-side) tolerates any value.
var codeLanguages = map[string]bool{
	"plaintext": true, "bash": true, "c": true, "cpp": true, "csharp": true,
	"css": true, "go": true, "html": true, "java": true, "javascript": true,
	"json": true, "kotlin": true, "markdown": true, "php": true, "python": true,
	"ruby": true, "rust": true, "sql": true, "swift": true, "typescript": true,
	"yaml": true,
}

// normalizeLang coerces a model-supplied code language into the editor's known
// set, mapping a few common aliases and defaulting to "plaintext".
func normalizeLang(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	if codeLanguages[l] {
		return l
	}
	switch l {
	case "shell", "sh", "zsh", "console":
		return "bash"
	case "js", "node", "jsx":
		return "javascript"
	case "ts", "tsx":
		return "typescript"
	case "py", "python3":
		return "python"
	case "golang":
		return "go"
	case "yml":
		return "yaml"
	case "c++":
		return "cpp"
	case "c#":
		return "csharp"
	}
	return "plaintext"
}

// sanitizeTags trims, lowercases, length-caps, de-duplicates and count-caps tags
// to match the form's tag handling and the admin limits.
func sanitizeTags(raw []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range raw {
		t = strings.ToLower(strings.TrimSpace(t))
		if len(t) > maxTagChars {
			t = strings.TrimSpace(t[:maxTagChars])
		}
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= maxTags {
			break
		}
	}
	return out
}

// newBlock marshals a block's data into a Block. The map shapes are fixed and
// always marshal, so the error is ignored.
func newBlock(typ string, data map[string]any) Block {
	raw, _ := json.Marshal(data)
	return Block{Type: typ, Data: raw}
}

// clip truncates s to at most max bytes, trimming trailing space left by a cut.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max])
}
