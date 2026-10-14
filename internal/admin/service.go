// Package admin implements the write-through CMS orchestration: every mutation
// is written to GitHub FIRST and only then applied to the disposable SQLite
// cache, and all writes are serialized by a single in-process lock. A GitHub
// failure therefore changes nothing locally, keeping GitHub the single source
// of truth. The HTTP surface (handlers + middleware) lives in internal/server;
// this package owns the ordering and conflict handling.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/github"
	"github.com/iamhimansu/himanshurajaman.com/internal/ingest"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
)

// Sentinel errors the HTTP handlers map to status codes. StaleError (a value
// type) carries the live repo sha after an optimistic-concurrency conflict.
var (
	// ErrPostExists → 409 "post already exists" (create collision).
	ErrPostExists = errors.New("post already exists")
	// ErrPageExists → 409 "page already exists" (create collision).
	ErrPageExists = errors.New("page already exists")
	// ErrGitHubWrite → 502: the GitHub write failed; nothing changed locally.
	ErrGitHubWrite = errors.New("github write failed")
	// ErrCacheAfterGitHub → 500: GitHub succeeded but the local cache update
	// failed; GitHub remains authoritative and the next sync reconciles.
	ErrCacheAfterGitHub = errors.New("saved to GitHub but local cache update failed; it will reconcile on next sync")
)

// StaleError is returned when a PUT/DELETE carried a stale fileSha. The handler
// surfaces it as 409 {"error":"stale","currentFileSha":<CurrentFileSHA>}; an
// empty CurrentFileSHA means the file was deleted out-of-band on GitHub.
type StaleError struct {
	CurrentFileSHA string
}

func (e *StaleError) Error() string { return "stale" }

// Config carries the repo coordinates the write client targets.
type Config struct {
	RepoOwner  string
	RepoName   string
	ContentRef string
}

// Service orchestrates GitHub-first, cache-after writes under one write lock.
// gh is the GitHub write client; it is nil in local-content mode, in which case
// the GitHub-write methods are never reached (the requireWrites middleware
// returns 503 first) and only GetResource reads / Resync / Preview run.
type Service struct {
	store    *store.Store
	gh       *github.Client // write client; nil when writes are disabled (local source)
	renderer *content.Renderer
	ingester *ingest.Ingester
	images   *ingest.ImageManager
	cfg      Config
	log      *slog.Logger

	// mu serializes every write path (posts/resume/image/resync), matching the
	// single SQLite writer. Reads (list/get/preview/get-resume) do not take it.
	mu sync.Mutex

	// markReady is injected after the server is constructed (DR5 late binding)
	// so resync can flip readiness without a construction cycle.
	markReady func(commit string)
}

// New builds the Service. markReady is wired separately via SetMarkReady once
// the server exists.
func New(st *store.Store, gh *github.Client, renderer *content.Renderer, ingester *ingest.Ingester, images *ingest.ImageManager, cfg Config, log *slog.Logger) *Service {
	return &Service{
		store:    st,
		gh:       gh,
		renderer: renderer,
		ingester: ingester,
		images:   images,
		cfg:      cfg,
		log:      log,
	}
}

// SetMarkReady late-binds the readiness callback (DR5). It is called once at
// boot, before the server starts serving, so no synchronization is required.
func (s *Service) SetMarkReady(fn func(commit string)) { s.markReady = fn }

// CreatePost writes a new post to GitHub and, only on success, upserts it into
// the cache. p is the already-validated parse of data (data == the assembled
// file bytes). Returns the new blob sha.
func (s *Service) CreatePost(ctx context.Context, p *content.ParsedPost, data []byte) (fileSha string, err error) {
	path := postPath(p.Slug)
	s.mu.Lock()
	defer s.mu.Unlock()

	exists, err := s.store.PostExists(ctx, p.Slug)
	if err != nil {
		return "", err
	}
	if exists {
		return "", ErrPostExists
	}

	newSHA, _, gerr := s.gh.PutContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef, "Create post: "+p.Slug, data, "")
	if errors.Is(gerr, github.ErrConflict) {
		// The file already exists on GitHub even though the cache didn't know.
		return "", ErrPostExists
	}
	if gerr != nil {
		return "", fmt.Errorf("%w: %v", ErrGitHubWrite, gerr)
	}

	if cerr := s.cachePost(ctx, p, path, newSHA); cerr != nil {
		s.log.Error("post cache update failed after github write", "slug", p.Slug, "error", cerr.Error())
		return "", ErrCacheAfterGitHub
	}
	return newSHA, nil
}

// UpdatePost updates an existing post. prevSHA is the client's expected blob
// sha; a mismatch yields *StaleError after syncing the cache to the live repo
// content. Slug immutability is enforced by the handler.
func (s *Service) UpdatePost(ctx context.Context, p *content.ParsedPost, data []byte, prevSHA string) (fileSha string, err error) {
	path := postPath(p.Slug)
	s.mu.Lock()
	defer s.mu.Unlock()

	newSHA, _, gerr := s.gh.PutContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef, "Update post: "+p.Slug, data, prevSHA)
	if errors.Is(gerr, github.ErrConflict) {
		return "", s.syncAfterConflict(ctx, path, p.Slug)
	}
	if gerr != nil {
		return "", fmt.Errorf("%w: %v", ErrGitHubWrite, gerr)
	}

	if cerr := s.cachePost(ctx, p, path, newSHA); cerr != nil {
		s.log.Error("post cache update failed after github write", "slug", p.Slug, "error", cerr.Error())
		return "", ErrCacheAfterGitHub
	}
	return newSHA, nil
}

// DeletePost deletes a post. sha is the client's expected blob sha; when empty
// the cached sha is used. A stale sha yields *StaleError. A GitHub 404 (already
// gone) is treated as success and the cache row is removed.
func (s *Service) DeletePost(ctx context.Context, slug, sha string) error {
	path := postPath(slug)
	s.mu.Lock()
	defer s.mu.Unlock()

	if sha == "" {
		cached, cerr := s.store.GetPostFileSHA(ctx, slug)
		if cerr != nil && !errors.Is(cerr, store.ErrNotFound) {
			return cerr
		}
		sha = cached // "" if absent; GitHub then 422s → conflict path
	}

	gerr := s.gh.DeleteContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef, "Delete post: "+slug, sha)
	switch {
	case errors.Is(gerr, github.ErrConflict):
		return s.syncAfterConflict(ctx, path, slug)
	case errors.Is(gerr, github.ErrNotFound):
		// Already gone on GitHub: idempotent, fall through to cache removal.
	case gerr != nil:
		return fmt.Errorf("%w: %v", ErrGitHubWrite, gerr)
	}

	if derr := s.store.DeletePost(ctx, slug); derr != nil {
		s.log.Error("post cache delete failed after github delete", "slug", slug, "error", derr.Error())
		return ErrCacheAfterGitHub
	}
	return nil
}

// CreatePage writes a new page to GitHub and, only on success, upserts it into
// the cache. p is the already-validated parse of data (data == the assembled
// file bytes). Returns the new blob sha.
func (s *Service) CreatePage(ctx context.Context, p *content.ParsedPage, data []byte) (fileSha string, err error) {
	path := pagePath(p.Slug)
	s.mu.Lock()
	defer s.mu.Unlock()

	exists, err := s.store.PageExists(ctx, p.Slug)
	if err != nil {
		return "", err
	}
	if exists {
		return "", ErrPageExists
	}

	newSHA, _, gerr := s.gh.PutContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef, "Create page: "+p.Slug, data, "")
	if errors.Is(gerr, github.ErrConflict) {
		return "", ErrPageExists
	}
	if gerr != nil {
		return "", fmt.Errorf("%w: %v", ErrGitHubWrite, gerr)
	}

	if cerr := s.cachePage(ctx, p, path, newSHA); cerr != nil {
		s.log.Error("page cache update failed after github write", "slug", p.Slug, "error", cerr.Error())
		return "", ErrCacheAfterGitHub
	}
	return newSHA, nil
}

// UpdatePage updates an existing page. prevSHA is the client's expected blob
// sha; a mismatch yields *StaleError after syncing the cache to the live repo
// content. Slug immutability is enforced by the handler.
func (s *Service) UpdatePage(ctx context.Context, p *content.ParsedPage, data []byte, prevSHA string) (fileSha string, err error) {
	path := pagePath(p.Slug)
	s.mu.Lock()
	defer s.mu.Unlock()

	newSHA, _, gerr := s.gh.PutContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef, "Update page: "+p.Slug, data, prevSHA)
	if errors.Is(gerr, github.ErrConflict) {
		return "", s.syncPageAfterConflict(ctx, path, p.Slug)
	}
	if gerr != nil {
		return "", fmt.Errorf("%w: %v", ErrGitHubWrite, gerr)
	}

	if cerr := s.cachePage(ctx, p, path, newSHA); cerr != nil {
		s.log.Error("page cache update failed after github write", "slug", p.Slug, "error", cerr.Error())
		return "", ErrCacheAfterGitHub
	}
	return newSHA, nil
}

// DeletePage deletes a page. sha is the client's expected blob sha; when empty
// the cached sha is used. A stale sha yields *StaleError. A GitHub 404 (already
// gone) is treated as success and the cache row is removed.
func (s *Service) DeletePage(ctx context.Context, slug, sha string) error {
	path := pagePath(slug)
	s.mu.Lock()
	defer s.mu.Unlock()

	if sha == "" {
		cached, cerr := s.store.GetPageFileSHA(ctx, slug)
		if cerr != nil && !errors.Is(cerr, store.ErrNotFound) {
			return cerr
		}
		sha = cached
	}

	gerr := s.gh.DeleteContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef, "Delete page: "+slug, sha)
	switch {
	case errors.Is(gerr, github.ErrConflict):
		return s.syncPageAfterConflict(ctx, path, slug)
	case errors.Is(gerr, github.ErrNotFound):
		// Already gone on GitHub: idempotent, fall through to cache removal.
	case gerr != nil:
		return fmt.Errorf("%w: %v", ErrGitHubWrite, gerr)
	}

	if derr := s.store.DeletePage(ctx, slug); derr != nil {
		s.log.Error("page cache delete failed after github delete", "slug", slug, "error", derr.Error())
		return ErrCacheAfterGitHub
	}
	return nil
}

// GetResume returns the authoritative resume JSON and its blob sha. In GitHub
// mode it reads the live file; in local mode it reconstructs the document from
// the cache and returns an empty sha (writes are disabled anyway).
func (s *Service) GetResume(ctx context.Context) (resume json.RawMessage, fileSha string, err error) {
	if s.gh != nil {
		data, sha, gerr := s.gh.GetContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, "resume/resume.json", s.cfg.ContentRef)
		if gerr != nil {
			return nil, "", gerr
		}
		return json.RawMessage(data), sha, nil
	}

	secs, rerr := s.store.Resume(ctx)
	if rerr != nil {
		return nil, "", rerr
	}
	doc := struct {
		Basics   json.RawMessage   `json:"basics"`
		Sections []json.RawMessage `json:"sections"`
	}{Sections: []json.RawMessage{}}
	for _, rs := range secs {
		if rs.Kind == "basics" {
			doc.Basics = rs.Data
			continue
		}
		doc.Sections = append(doc.Sections, rs.Data)
	}
	b, merr := json.Marshal(doc)
	if merr != nil {
		return nil, "", merr
	}
	return json.RawMessage(b), "", nil
}

// PutResume writes the resume to GitHub and, on success, replaces the cached
// sections. data is the canonical file bytes; sections is the validated parse.
func (s *Service) PutResume(ctx context.Context, data []byte, sections []store.ResumeSection, prevSHA string) (fileSha string, err error) {
	const path = "resume/resume.json"
	s.mu.Lock()
	defer s.mu.Unlock()

	newSHA, _, gerr := s.gh.PutContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef, "Update resume", data, prevSHA)
	if errors.Is(gerr, github.ErrConflict) {
		_, liveSHA, ferr := s.gh.GetContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef)
		if ferr != nil {
			s.log.Error("resume conflict re-fetch failed", "error", ferr.Error())
			return "", &StaleError{CurrentFileSHA: ""}
		}
		return "", &StaleError{CurrentFileSHA: liveSHA}
	}
	if gerr != nil {
		return "", fmt.Errorf("%w: %v", ErrGitHubWrite, gerr)
	}

	if rerr := s.store.ReplaceResume(ctx, sections); rerr != nil {
		s.log.Error("resume cache update failed after github write", "error", rerr.Error())
		return "", ErrCacheAfterGitHub
	}
	return newSHA, nil
}

// UploadImage writes a content-hashed image to GitHub and stages it into the
// active served directory for immediate preview. A conflict (identical name ⇒
// identical content already present) is idempotent success. name is the safe
// content-hashed filename (no directory component); data is the raw bytes.
func (s *Service) UploadImage(ctx context.Context, name string, data []byte) (url, fileSha string, err error) {
	path := "images/" + name
	s.mu.Lock()
	defer s.mu.Unlock()

	newSHA, _, gerr := s.gh.PutContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef, "Add image: "+name, data, "")
	switch {
	case errors.Is(gerr, github.ErrConflict):
		// Same hashed name means same content: the image is already present.
		if _, sha, ferr := s.gh.GetContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef); ferr == nil {
			newSHA = sha
		}
	case gerr != nil:
		return "", "", fmt.Errorf("%w: %v", ErrGitHubWrite, gerr)
	}

	if serr := s.images.Stage(name, data); serr != nil {
		// The image is safely on GitHub; a staging failure only delays the local
		// preview until the next ingest.
		s.log.Error("stage image failed", "name", name, "error", serr.Error())
	}
	return "/images/" + name, newSHA, nil
}

// Preview renders an Editor.js content document with the same pipeline as
// publish, so the preview is byte-identical (and already sanitized) to the
// stored HTML. It takes no lock.
func (s *Service) Preview(doc json.RawMessage) (html string, err error) {
	parsed, perr := content.ParseDocument(doc)
	if perr != nil {
		return "", perr
	}
	html, _, err = s.renderer.RenderDocument(parsed)
	return html, err
}

// Resync runs the full ingest under the write lock and, on success, flips
// readiness via the injected callback. A failure preserves the prior cache
// (store.Replace rolls back).
func (s *Service) Resync(ctx context.Context) (*ingest.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.ingester.Run(ctx)
	if err != nil {
		return nil, err
	}
	if s.markReady != nil {
		s.markReady(res.CommitSHA)
	}
	return res, nil
}

// syncAfterConflict re-fetches the live repo content after an optimistic-
// concurrency conflict and reconciles the cache to it, then returns a
// *StaleError carrying the live sha. [DR9] A live ErrNotFound (concurrent
// external delete) removes the cache row and returns an empty currentFileSha.
// The caller holds the write lock.
func (s *Service) syncAfterConflict(ctx context.Context, path, slug string) error {
	liveData, liveSHA, gerr := s.gh.GetContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef)
	if errors.Is(gerr, github.ErrNotFound) {
		_ = s.store.DeletePost(ctx, slug)
		return &StaleError{CurrentFileSHA: ""}
	}
	if gerr != nil {
		s.log.Error("conflict re-fetch failed", "slug", slug, "error", gerr.Error())
		return &StaleError{CurrentFileSHA: ""}
	}
	if parsed, perr := content.ParsePost(path, liveData); perr == nil {
		if cerr := s.cachePost(ctx, parsed, path, liveSHA); cerr != nil {
			s.log.Error("cache sync after conflict failed", "slug", slug, "error", cerr.Error())
		}
	} else {
		s.log.Warn("live content invalid during conflict sync", "slug", slug, "error", perr.Error())
	}
	return &StaleError{CurrentFileSHA: liveSHA}
}

// cachePost renders a parsed post and upserts it into the cache with the given
// repo path + blob sha, matching ingest's row shape.
func (s *Service) cachePost(ctx context.Context, p *content.ParsedPost, path, fileSHA string) error {
	html, plain, err := s.renderer.RenderDocument(p.Document)
	if err != nil {
		return err
	}
	post := content.Post{
		Slug:           p.Slug,
		Title:          p.Title,
		Summary:        p.Summary,
		Document:       string(p.DocumentJSON),
		HTML:           html,
		PlainText:      plain,
		Published:      p.Published,
		PublishedAt:    p.Date,
		UpdatedAt:      p.Updated,
		CoverImage:     p.Cover,
		ReadingMinutes: content.ReadingMinutes(plain),
		Page:           p.Page,
		Tags:           p.Tags,
		FilePath:       path,
		FileSHA:        fileSHA,
	}
	return s.store.UpsertPost(ctx, post)
}

func postPath(slug string) string { return "posts/" + slug + ".json" }

func pagePath(slug string) string { return "pages/" + slug + ".json" }

// cachePage renders a parsed page and upserts it into the cache with the given
// repo path + blob sha, matching ingest's row shape.
func (s *Service) cachePage(ctx context.Context, p *content.ParsedPage, path, fileSHA string) error {
	html, _, err := s.renderer.RenderDocument(p.Document)
	if err != nil {
		return err
	}
	page := content.Page{
		Slug:      p.Slug,
		Title:     p.Title,
		Nav:       p.Nav,
		NavLabel:  p.NavLabel,
		NavOrder:  p.NavOrder,
		Published: p.Published,
		Summary:   p.Summary,
		Updated:   p.Updated,
		HTML:      html,
		Document:  string(p.DocumentJSON),
		FilePath:  path,
		FileSHA:   fileSHA,
	}
	return s.store.UpsertPage(ctx, page)
}

// syncPageAfterConflict re-fetches the live repo content after an optimistic-
// concurrency conflict and reconciles the cache to it, then returns a
// *StaleError carrying the live sha. A live ErrNotFound (concurrent external
// delete) removes the cache row and returns an empty currentFileSha. The caller
// holds the write lock.
func (s *Service) syncPageAfterConflict(ctx context.Context, path, slug string) error {
	liveData, liveSHA, gerr := s.gh.GetContents(ctx, s.cfg.RepoOwner, s.cfg.RepoName, path, s.cfg.ContentRef)
	if errors.Is(gerr, github.ErrNotFound) {
		_ = s.store.DeletePage(ctx, slug)
		return &StaleError{CurrentFileSHA: ""}
	}
	if gerr != nil {
		s.log.Error("page conflict re-fetch failed", "slug", slug, "error", gerr.Error())
		return &StaleError{CurrentFileSHA: ""}
	}
	if parsed, perr := content.ParsePage(path, liveData); perr == nil {
		if cerr := s.cachePage(ctx, parsed, path, liveSHA); cerr != nil {
			s.log.Error("page cache sync after conflict failed", "slug", slug, "error", cerr.Error())
		}
	} else {
		s.log.Warn("live page content invalid during conflict sync", "slug", slug, "error", perr.Error())
	}
	return &StaleError{CurrentFileSHA: liveSHA}
}
