package content

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
)

var slugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// PostMeta is the metadata block of a post JSON file. Dates are decoded as
// strings so we can validate RFC3339 strictly ourselves. The JSON tags also
// drive the canonical assemble order (see AssemblePostJSON): optional fields
// (updated/tags/cover) are omitted when empty, but published is always emitted
// so published:false survives the round-trip.
type PostMeta struct {
	Title     string   `json:"title"`
	Slug      string   `json:"slug"`
	Date      string   `json:"date"`
	Updated   string   `json:"updated,omitempty"`
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags,omitempty"`
	Published bool     `json:"published"`
	Cover     string   `json:"cover,omitempty"`
}

// EditorDocument is the Editor.js OutputData persisted as a post's content: an
// ordered list of blocks plus optional time/version metadata.
type EditorDocument struct {
	Time    int64         `json:"time,omitempty"`
	Blocks  []EditorBlock `json:"blocks"`
	Version string        `json:"version,omitempty"`
}

// EditorBlock is a single Editor.js block. Data is kept raw so the renderer can
// decode it per block type and unknown types never break parsing.
type EditorBlock struct {
	ID   string          `json:"id,omitempty"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// postFile is the on-disk shape of posts/<slug>.json.
type postFile struct {
	Meta    PostMeta        `json:"meta"`
	Content json.RawMessage `json:"content"`
}

// ParsedPost is validated metadata plus the parsed Editor.js document and the
// compacted canonical content JSON (stored as the editable "source").
type ParsedPost struct {
	Title     string
	Slug      string
	Summary   string
	Date      string // RFC3339 UTC
	Updated   string // RFC3339 UTC or ""
	Tags      []Tag
	Published bool
	Cover     string

	Document     EditorDocument // parsed blocks, rendered by the server
	DocumentJSON []byte         // compacted content OutputData (the edit source)
}

// ParsePost validates a post file. The slug must match the required pattern and
// equal the filename stem. On any validation failure it returns an error; the
// caller skips (and logs) the file rather than aborting the whole ingest.
func ParsePost(filePath string, src []byte) (*ParsedPost, error) {
	var pf postFile
	dec := json.NewDecoder(strings.NewReader(string(src)))
	if err := dec.Decode(&pf); err != nil {
		return nil, fmt.Errorf("parse post json: %w", err)
	}

	title := strings.TrimSpace(pf.Meta.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	slug := strings.TrimSpace(pf.Meta.Slug)
	if slug == "" {
		return nil, fmt.Errorf("slug is required")
	}
	if !slugRE.MatchString(slug) {
		return nil, fmt.Errorf("slug %q must match ^[a-z0-9]+(?:-[a-z0-9]+)*$", slug)
	}
	if stem := fileStem(filePath); slug != stem {
		return nil, fmt.Errorf("slug %q must equal filename stem %q", slug, stem)
	}

	summary := strings.TrimSpace(pf.Meta.Summary)
	if summary == "" {
		return nil, fmt.Errorf("summary is required")
	}

	if strings.TrimSpace(pf.Meta.Date) == "" {
		return nil, fmt.Errorf("date is required")
	}
	date, err := time.Parse(time.RFC3339, strings.TrimSpace(pf.Meta.Date))
	if err != nil {
		return nil, fmt.Errorf("date must be RFC3339: %w", err)
	}

	var updated string
	if u := strings.TrimSpace(pf.Meta.Updated); u != "" {
		t, err := time.Parse(time.RFC3339, u)
		if err != nil {
			return nil, fmt.Errorf("updated must be RFC3339: %w", err)
		}
		updated = t.UTC().Format(time.RFC3339)
	}

	doc, docJSON, err := parseContent(pf.Content)
	if err != nil {
		return nil, err
	}

	return &ParsedPost{
		Title:   title,
		Slug:    slug,
		Summary: summary,
		// Normalize to UTC so RFC3339 strings sort lexicographically in true
		// chronological order (keyset pagination relies on this).
		Date:         date.UTC().Format(time.RFC3339),
		Updated:      updated,
		Tags:         normalizeTags(pf.Meta.Tags),
		Published:    pf.Meta.Published,
		Cover:        strings.TrimSpace(pf.Meta.Cover),
		Document:     doc,
		DocumentJSON: docJSON,
	}, nil
}

// parseContent decodes an Editor.js OutputData document and returns the parsed
// form plus the compacted canonical JSON. An empty/absent content defaults to a
// valid empty document so downstream code always has a renderable, storable
// value.
func parseContent(raw json.RawMessage) (EditorDocument, []byte, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return EditorDocument{Blocks: []EditorBlock{}}, []byte(emptyDocumentJSON), nil
	}
	var doc EditorDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return EditorDocument{}, nil, fmt.Errorf("content must be an Editor.js document: %w", err)
	}
	if doc.Blocks == nil {
		doc.Blocks = []EditorBlock{}
	}
	compact, err := json.Marshal(doc)
	if err != nil {
		return EditorDocument{}, nil, fmt.Errorf("compact content: %w", err)
	}
	return doc, compact, nil
}

const emptyDocumentJSON = `{"blocks":[]}`

// ParseDocument decodes a standalone Editor.js content document (used by the
// admin preview endpoint). An empty/absent document is a valid empty document.
func ParseDocument(raw json.RawMessage) (EditorDocument, error) {
	doc, _, err := parseContent(raw)
	return doc, err
}

// normalizeTags trims, slugifies, and de-duplicates tags (keyed by slug).
func normalizeTags(raw []string) []Tag {
	seen := map[string]bool{}
	var tags []Tag
	for _, r := range raw {
		name := strings.TrimSpace(r)
		if name == "" {
			continue
		}
		slug := Slugify(name)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		tags = append(tags, Tag{Name: name, Slug: slug})
	}
	return tags
}

func fileStem(p string) string {
	b := path.Base(p)
	return strings.TrimSuffix(b, path.Ext(b))
}
