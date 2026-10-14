package content

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// NavOrderDefault is the resolved nav_order for a page that omits navOrder: a
// large value so "absent" sorts after any explicit order (an explicit 0 still
// sorts first). The nav tie-break is navOrder ASC, title ASC.
const NavOrderDefault = 1000000

// ReservedSlugs is the lowercased set of slugs a page may not use, because they
// collide with existing routes / asset prefixes. Shared by ParsePage (ingest)
// and the admin validator so the rule is enforced identically on both paths.
var ReservedSlugs = map[string]bool{
	"blog": true, "resume": true, "works": true, "contact": true,
	"admin": true, "api": true, "rss": true, "sitemap": true,
	"robots": true, "healthz": true, "search": true, "auth": true,
	"webhook": true, "images": true, "assets": true,
}

// IsReservedSlug reports whether slug (trimmed, lowercased) is reserved.
func IsReservedSlug(slug string) bool {
	return ReservedSlugs[strings.ToLower(strings.TrimSpace(slug))]
}

// PageMeta is the metadata block of a pages/<slug>.json file. navOrder is a
// *int so "absent" (sort-last) is distinguishable from an explicit 0; all other
// optional fields follow the post convention (omitempty). published and nav are
// always emitted so false survives the round-trip.
type PageMeta struct {
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	Nav       bool   `json:"nav"`
	NavLabel  string `json:"navLabel,omitempty"`
	NavOrder  *int   `json:"navOrder,omitempty"`
	Published bool   `json:"published"`
	Summary   string `json:"summary,omitempty"`
	Updated   string `json:"updated,omitempty"`
}

// pageFile is the on-disk shape of pages/<slug>.json.
type pageFile struct {
	Meta    PageMeta        `json:"meta"`
	Content json.RawMessage `json:"content"`
}

// ParsedPage is validated metadata plus the parsed Editor.js document and the
// compacted canonical content JSON (the editable source), parallel to ParsedPost.
type ParsedPage struct {
	Title     string
	Slug      string
	Nav       bool
	NavLabel  string
	NavOrder  int // resolved: absent -> NavOrderDefault
	Published bool
	Summary   string
	Updated   string // RFC3339 UTC or ""

	Document     EditorDocument // parsed blocks, rendered by the server
	DocumentJSON []byte         // compacted content OutputData (the edit source)
}

// ParsePage validates a page file. It mirrors ParsePost: the slug must match the
// required pattern, equal the filename stem, and NOT be reserved. summary is
// optional (unlike posts) and there is no date field. On any validation failure
// it returns an error; the caller skips (and logs) the file rather than aborting
// the whole ingest.
func ParsePage(filePath string, src []byte) (*ParsedPage, error) {
	var pf pageFile
	dec := json.NewDecoder(strings.NewReader(string(src)))
	if err := dec.Decode(&pf); err != nil {
		return nil, fmt.Errorf("parse page json: %w", err)
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
	if IsReservedSlug(slug) {
		return nil, fmt.Errorf("slug %q is reserved", slug)
	}

	var updated string
	if u := strings.TrimSpace(pf.Meta.Updated); u != "" {
		t, err := time.Parse(time.RFC3339, u)
		if err != nil {
			return nil, fmt.Errorf("updated must be RFC3339: %w", err)
		}
		updated = t.UTC().Format(time.RFC3339)
	}

	navOrder := NavOrderDefault
	if pf.Meta.NavOrder != nil {
		navOrder = *pf.Meta.NavOrder
	}

	doc, docJSON, err := parseContent(pf.Content)
	if err != nil {
		return nil, err
	}

	return &ParsedPage{
		Title:        title,
		Slug:         slug,
		Nav:          pf.Meta.Nav,
		NavLabel:     strings.TrimSpace(pf.Meta.NavLabel),
		NavOrder:     navOrder,
		Published:    pf.Meta.Published,
		Summary:      strings.TrimSpace(pf.Meta.Summary),
		Updated:      updated,
		Document:     doc,
		DocumentJSON: docJSON,
	}, nil
}
