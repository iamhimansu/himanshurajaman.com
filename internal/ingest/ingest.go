// Package ingest fetches the content snapshot, parses and renders it, and swaps
// it into the SQLite cache in a single transaction.
package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/source"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
)

// Ingester runs the fetch -> parse -> render -> store pipeline.
type Ingester struct {
	src      source.Source
	store    *store.Store
	renderer *content.Renderer
	images   *ImageManager
	log      *slog.Logger
}

// New builds an Ingester.
func New(src source.Source, st *store.Store, r *content.Renderer, imgs *ImageManager, log *slog.Logger) *Ingester {
	return &Ingester{src: src, store: st, renderer: r, images: imgs, log: log}
}

// Result summarizes an ingest for logging.
type Result struct {
	CommitSHA string
	Ref       string
	Published int
	Drafts    int
	Pages     int
	Skipped   int
	Images    int
}

// Run performs a full ingest. A single invalid post is skipped and logged;
// a missing/invalid resume or settings file aborts the whole ingest so a
// previously good cache (if any) is preserved.
func (ing *Ingester) Run(ctx context.Context) (*Result, error) {
	snap, err := ing.src.Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch content: %w", err)
	}

	var (
		postFiles    []source.File
		pageFiles    []source.File
		imageFiles   []source.File
		resumeFile   *source.File
		settingsFile *source.File
	)
	for i := range snap.Files {
		f := snap.Files[i]
		switch {
		case strings.HasPrefix(f.Path, "posts/") && strings.HasSuffix(f.Path, ".json"):
			postFiles = append(postFiles, f)
		case strings.HasPrefix(f.Path, "pages/") && strings.HasSuffix(f.Path, ".json"):
			pageFiles = append(pageFiles, f)
		case f.Path == "resume/resume.json":
			rf := f
			resumeFile = &rf
		case f.Path == "settings/site.json":
			sf := f
			settingsFile = &sf
		case strings.HasPrefix(f.Path, "images/"):
			imageFiles = append(imageFiles, f)
		}
	}

	if resumeFile == nil {
		return nil, fmt.Errorf("required file missing: resume/resume.json")
	}
	if settingsFile == nil {
		return nil, fmt.Errorf("required file missing: settings/site.json")
	}

	resume, err := BuildResume(resumeFile.Data)
	if err != nil {
		return nil, fmt.Errorf("resume/resume.json: %w", err)
	}
	settings, err := BuildSettings(settingsFile.Data)
	if err != nil {
		return nil, fmt.Errorf("settings/site.json: %w", err)
	}

	// Deterministic order so duplicate-slug "first seen" is stable.
	sort.Slice(postFiles, func(i, j int) bool { return postFiles[i].Path < postFiles[j].Path })

	var (
		posts     []content.Post
		published int
		drafts    int
		skipped   int
	)
	seen := map[string]string{} // slug -> file path
	for _, pf := range postFiles {
		parsed, err := content.ParsePost(pf.Path, pf.Data)
		if err != nil {
			ing.log.Warn("skipping invalid post", "file", pf.Path, "error", err.Error())
			skipped++
			continue
		}
		if first, dup := seen[parsed.Slug]; dup {
			ing.log.Warn("skipping duplicate slug", "file", pf.Path, "slug", parsed.Slug, "firstSeen", first)
			skipped++
			continue
		}
		html, plain, err := ing.renderer.RenderDocument(parsed.Document)
		if err != nil {
			ing.log.Warn("skipping post: render failed", "file", pf.Path, "error", err.Error())
			skipped++
			continue
		}
		seen[parsed.Slug] = pf.Path

		posts = append(posts, content.Post{
			Slug:           parsed.Slug,
			Title:          parsed.Title,
			Summary:        parsed.Summary,
			Document:       string(parsed.DocumentJSON),
			HTML:           html,
			PlainText:      plain,
			Published:      parsed.Published,
			PublishedAt:    parsed.Date,
			UpdatedAt:      parsed.Updated,
			CoverImage:     parsed.Cover,
			ReadingMinutes: content.ReadingMinutes(plain),
			Tags:           parsed.Tags,
			Page:           parsed.Page,
			FilePath:       pf.Path,
			FileSHA:        content.GitBlobSHA(pf.Data),
		})
		if parsed.Published {
			published++
		} else {
			drafts++
		}
	}

	// Pages are OPTIONAL: a repo with no pages/ dir ingests successfully (nav
	// falls back to the core items). Each invalid/reserved/duplicate page is
	// skipped and logged, never aborting the ingest — same semantics as posts.
	sort.Slice(pageFiles, func(i, j int) bool { return pageFiles[i].Path < pageFiles[j].Path })
	var pages []content.Page
	seenPages := map[string]string{} // slug -> file path
	for _, pf := range pageFiles {
		parsed, err := content.ParsePage(pf.Path, pf.Data)
		if err != nil {
			ing.log.Warn("skipping invalid page", "file", pf.Path, "error", err.Error())
			skipped++
			continue
		}
		if first, dup := seenPages[parsed.Slug]; dup {
			ing.log.Warn("skipping duplicate page slug", "file", pf.Path, "slug", parsed.Slug, "firstSeen", first)
			skipped++
			continue
		}
		html, _, err := ing.renderer.RenderDocument(parsed.Document)
		if err != nil {
			ing.log.Warn("skipping page: render failed", "file", pf.Path, "error", err.Error())
			skipped++
			continue
		}
		seenPages[parsed.Slug] = pf.Path

		pages = append(pages, content.Page{
			Slug:      parsed.Slug,
			Title:     parsed.Title,
			Nav:       parsed.Nav,
			NavLabel:  parsed.NavLabel,
			NavOrder:  parsed.NavOrder,
			Published: parsed.Published,
			Summary:   parsed.Summary,
			Updated:   parsed.Updated,
			HTML:      html,
			Document:  string(parsed.DocumentJSON),
			FilePath:  pf.Path,
			FileSHA:   content.GitBlobSHA(pf.Data),
		})
	}

	// Stage images to a fresh directory BEFORE the DB swap. If the DB write
	// fails we discard them; if it succeeds we activate them.
	imgDir, imgCount, err := ing.images.Prepare(imageFiles)
	if err != nil {
		ing.log.Warn("preparing images failed; continuing without image refresh", "error", err.Error())
		imgDir, imgCount = "", 0
	}

	ds := &store.Dataset{
		Posts:     posts,
		Pages:     pages,
		Resume:    resume,
		Settings:  settings,
		CommitSHA: snap.CommitSHA,
		Ref:       snap.Ref,
		SyncedAt:  time.Now(),
	}
	if err := ing.store.Replace(ctx, ds); err != nil {
		ing.images.Discard(imgDir)
		return nil, fmt.Errorf("write cache: %w", err)
	}
	if imgDir != "" {
		ing.images.Activate(imgDir)
	}

	return &Result{
		CommitSHA: snap.CommitSHA,
		Ref:       snap.Ref,
		Published: published,
		Drafts:    drafts,
		Pages:     len(pages),
		Skipped:   skipped,
		Images:    imgCount,
	}, nil
}

// BuildResume validates the resume document and flattens it into stored rows:
// "basics" at position 0, then each entry in "sections" at positions 1..N.
// Unknown section kinds are kept (stored generically), not rejected. It is the
// single source of truth for resume validation, reused by the admin layer.
func BuildResume(data []byte) ([]store.ResumeSection, error) {
	var doc struct {
		Basics   json.RawMessage   `json:"basics"`
		Sections []json.RawMessage `json:"sections"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(doc.Basics) == 0 {
		return nil, fmt.Errorf("basics is required")
	}

	var basics struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(doc.Basics, &basics); err != nil {
		return nil, fmt.Errorf("invalid basics: %w", err)
	}
	if strings.TrimSpace(basics.Name) == "" {
		return nil, fmt.Errorf("basics.name is required")
	}
	if strings.TrimSpace(basics.Email) == "" {
		return nil, fmt.Errorf("basics.email is required")
	}

	sections := []store.ResumeSection{
		{Kind: "basics", Position: 0, Data: compactJSON(doc.Basics)},
	}
	for i, raw := range doc.Sections {
		var meta struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(raw, &meta)
		kind := strings.TrimSpace(meta.Kind)
		if kind == "" {
			kind = "unknown"
		}
		sections = append(sections, store.ResumeSection{Kind: kind, Position: i + 1, Data: compactJSON(raw)})
	}
	return sections, nil
}

// BuildSettings validates settings JSON and compacts each top-level value. It
// is exported for symmetry with BuildResume and reuse by the admin layer.
func BuildSettings(data []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("settings is empty")
	}
	out := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		out[k] = json.RawMessage(compactJSON(v))
	}
	return out, nil
}

func compactJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
