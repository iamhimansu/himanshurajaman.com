// Package content parses post JSON (metadata + an Editor.js document), renders
// the document to sanitized HTML on the server, and defines the domain types
// ingest hands to the store.
package content

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

// Post is a fully parsed, rendered post ready to be written to the cache.
type Post struct {
	Slug           string
	Title          string
	Summary        string
	Document       string // canonical Editor.js content OutputData (the edit source)
	HTML           string // rendered then sanitized
	PlainText      string // body as plain text, for the FTS index
	Published      bool
	PublishedAt    string // RFC3339 (UTC), from the post meta `date`
	UpdatedAt      string // RFC3339 (UTC) or "", from the post meta `updated`
	CoverImage     string
	ReadingMinutes int
	Page           string // owning page slug, or "" (unfiled)
	Tags           []Tag
	FilePath       string
	FileSHA        string // git blob SHA (matches GitHub's for Phase 4 concurrency)
}

// Page is a fully parsed, rendered page ready to be written to the cache
// (parallel to Post). Pages are not time-ordered and are not in the FTS index,
// so there is no date or PlainText.
type Page struct {
	Slug      string
	Title     string
	Nav       bool
	NavLabel  string
	NavOrder  int
	Published bool
	Summary   string
	Updated   string // RFC3339 (UTC) or ""
	HTML      string // rendered then sanitized
	Document  string // canonical Editor.js content OutputData (the edit source)
	FilePath  string
	FileSHA   string // git blob SHA (matches GitHub's for concurrency)
}

// Tag is a post tag: a display name plus its slug.
type Tag struct {
	Name string
	Slug string
}

// Slugify normalizes a string to a URL/identifier slug: lowercase, with runs of
// spaces/underscores/hyphens collapsed to a single hyphen and all other
// characters dropped. Used for both tags and (validated against) post slugs.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastHyphen := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		case r == ' ' || r == '-' || r == '_':
			if b.Len() > 0 && !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// GitBlobSHA computes the git blob object ID of data: sha1("blob <len>\0" + data).
// This matches the `sha` GitHub returns for a file, so the value stored now is
// usable for optimistic-concurrency writes in Phase 4.
func GitBlobSHA(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
