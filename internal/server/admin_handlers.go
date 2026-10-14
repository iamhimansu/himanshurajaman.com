package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamhimansu/himanshurajaman.com/internal/admin"
	"github.com/iamhimansu/himanshurajaman.com/internal/auth"
	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/github"
	"github.com/iamhimansu/himanshurajaman.com/internal/ingest"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
)

// ctxKey is the private type for request-context keys set by middleware.
type ctxKey int

const ctxKeyEmail ctxKey = iota

// Limits per §10.3.
const (
	maxTitleLen   = 200
	maxSlugLen    = 100
	maxSummaryLen = 500
	maxTags       = 20
	maxTagLen     = 40
	maxBodyBytes  = 256 * 1024 // 256 KiB — serialized content document cap
	maxImageBytes = 10 << 20   // 10 MiB
	maxBlocks     = 1000       // Editor.js block-count cap per document
	// maxPostJSON bounds the create/update request body: the content document
	// (≤256 KiB) plus metadata/JSON overhead. Images are uploaded separately so
	// documents stay small.
	maxPostJSON = 1 << 20 // 1 MiB
)

var fileShaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// --- middleware ---

// requireAdminEnabled short-circuits with 503 "admin disabled" when the admin
// surface is not configured.
func (s *Server) requireAdminEnabled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.adminEnabled {
			writeError(w, http.StatusServiceUnavailable, "admin disabled")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireSession rejects requests without a valid session cookie with 401 and
// otherwise stashes the authenticated email in the request context.
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.sealer.SessionCookieName())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		sess, err := s.sealer.OpenSession(c.Value, time.Now())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyEmail, sess.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireCSRF enforces the double-submit check on writes.
func (s *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cookieVal string
		if c, err := r.Cookie(s.sealer.CSRFCookieName()); err == nil {
			cookieVal = c.Value
		}
		if !auth.CSRFValid(cookieVal, r.Header.Get("X-CSRF-Token")) {
			writeError(w, http.StatusForbidden, "csrf")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireWrites blocks GitHub-write endpoints when the content source is not
// GitHub (admin enabled but local content).
func (s *Server) requireWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.writesEnabled {
			writeError(w, http.StatusServiceUnavailable, "writes disabled: content source is not github")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAIEnabled short-circuits the AI-drafting endpoints with 503 when
// GROQ_API_KEY is unset (s.ai is nil). Mirrors requireWrites' fail-safe shape.
func (s *Server) requireAIEnabled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.aiEnabled || s.ai == nil {
			writeError(w, http.StatusServiceUnavailable, "ai unavailable")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- request/response shapes ---

type adminPostRequest struct {
	Meta    adminPostMeta   `json:"meta"`
	Content json.RawMessage `json:"content"`
	FileSha string          `json:"fileSha"`
}

type adminPostMeta struct {
	Title     string   `json:"title"`
	Slug      string   `json:"slug"`
	Date      string   `json:"date"`
	Updated   string   `json:"updated"`
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags"`
	Published bool     `json:"published"`
	Cover     string   `json:"cover"`
}

// --- post handlers ---

func (s *Server) handleAdminListPosts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := s.store.AdminListPosts(r.Context(), q.Get("status"), q.Get("page"))
	if err != nil {
		s.log.Error("admin list posts", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAdminGetPost(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	ap, err := s.store.GetAdminPost(r.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "post not found")
			return
		}
		s.log.Error("admin get post", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	meta := adminPostMeta{
		Title:     ap.Title,
		Slug:      ap.Slug,
		Date:      ap.PublishedAt,
		Updated:   ap.UpdatedAt,
		Summary:   ap.Summary,
		Tags:      ap.Tags,
		Published: ap.Published,
		Cover:     ap.CoverImage,
	}

	// ap.Document is the canonical Editor.js content OutputData; return it as
	// raw JSON so the editor loads the exact document for editing.
	writeJSON(w, http.StatusOK, map[string]any{
		"slug":    ap.Slug,
		"meta":    meta,
		"content": json.RawMessage(ap.Document),
		"fileSha": ap.FileSHA,
	})
}

func (s *Server) handleAdminCreatePost(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodePostRequest(w, r)
	if !ok {
		return
	}
	parsed, data, ok := s.validatePost(w, req)
	if !ok {
		return
	}

	fileSha, err := s.admin.CreatePost(r.Context(), parsed, data)
	if err != nil {
		s.writeAdminWriteError(w, "create post", parsed.Slug, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"slug": parsed.Slug, "fileSha": fileSha})
}

func (s *Server) handleAdminUpdatePost(w http.ResponseWriter, r *http.Request) {
	pathSlug := chi.URLParam(r, "slug")
	req, ok := s.decodePostRequest(w, r)
	if !ok {
		return
	}
	if req.Meta.Slug != pathSlug {
		writeError(w, http.StatusBadRequest, "slug is immutable")
		return
	}
	if !fileShaRE.MatchString(req.FileSha) {
		writeError(w, http.StatusBadRequest, "fileSha must match ^[0-9a-f]{40}$")
		return
	}
	// On edit, stamp meta.updated to now (RFC3339 UTC) so the public "Updated"
	// date populates. Create leaves updated empty (handleAdminCreatePost), so a
	// post only gains an "Updated" date once it is actually edited. This is the
	// single authoritative source for the field, overriding any client value.
	req.Meta.Updated = time.Now().UTC().Format(time.RFC3339)
	parsed, data, ok := s.validatePost(w, req)
	if !ok {
		return
	}

	fileSha, err := s.admin.UpdatePost(r.Context(), parsed, data, req.FileSha)
	if err != nil {
		s.writeAdminWriteError(w, "update post", parsed.Slug, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slug": parsed.Slug, "fileSha": fileSha})
}

func (s *Server) handleAdminDeletePost(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	// fileSha may arrive in a small JSON body or as a query param; both optional.
	sha := r.URL.Query().Get("fileSha")
	if sha == "" {
		var body struct {
			FileSha string `json:"fileSha"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			sha = body.FileSha
		}
	}
	if sha != "" && !fileShaRE.MatchString(sha) {
		writeError(w, http.StatusBadRequest, "fileSha must match ^[0-9a-f]{40}$")
		return
	}

	if err := s.admin.DeletePost(r.Context(), slug, sha); err != nil {
		s.writeAdminWriteError(w, "delete post", slug, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// --- resume handlers ---

func (s *Server) handleAdminGetResume(w http.ResponseWriter, r *http.Request) {
	raw, fileSha, err := s.admin.GetResume(r.Context())
	if err != nil {
		if errors.Is(err, github.ErrNotFound) {
			writeError(w, http.StatusNotFound, "resume not found")
			return
		}
		s.log.Error("admin get resume", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resume": raw, "fileSha": fileSha})
}

func (s *Server) handleAdminPutResume(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPostJSON)
	var req struct {
		Resume  json.RawMessage `json:"resume"`
		FileSha string          `json:"fileSha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Resume) == 0 {
		writeError(w, http.StatusBadRequest, "resume is required")
		return
	}
	if !fileShaRE.MatchString(req.FileSha) {
		writeError(w, http.StatusBadRequest, "fileSha must match ^[0-9a-f]{40}$")
		return
	}

	sections, err := ingest.BuildResume(req.Resume)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Canonical pretty JSON (stable 2-space indent, preserving key order).
	var buf bytes.Buffer
	if err := json.Indent(&buf, req.Resume, "", "  "); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	buf.WriteByte('\n')

	fileSha, err := s.admin.PutResume(r.Context(), buf.Bytes(), sections, req.FileSha)
	if err != nil {
		s.writeAdminWriteError(w, "update resume", "", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fileSha": fileSha})
}

// --- image / preview / resync handlers ---

func (s *Server) handleAdminUploadImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes)
	file, header, err := r.FormFile("file")
	if err != nil {
		if isTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, "image too large")
			return
		}
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		if isTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, "image too large")
			return
		}
		writeError(w, http.StatusBadRequest, "could not read file")
		return
	}

	ext, ok := sniffImageType(data)
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported image type")
		return
	}
	name := imageName(header.Filename, ext, data)

	url, fileSha, err := s.admin.UploadImage(r.Context(), name, data)
	if err != nil {
		s.writeAdminWriteError(w, "upload image", name, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"url": url, "name": name, "fileSha": fileSha})
}

func (s *Server) handleAdminPreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPostJSON)
	var req struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Content) > maxBodyBytes {
		writeError(w, http.StatusBadRequest, "content too large (max 256KiB)")
		return
	}
	html, err := s.admin.Preview(req.Content)
	if err != nil {
		s.log.Error("admin preview", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "render failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"html": html})
}

func (s *Server) handleAdminResync(w http.ResponseWriter, r *http.Request) {
	res, err := s.admin.Resync(r.Context())
	if err != nil {
		s.log.Error("admin resync", "error", err.Error())
		writeError(w, http.StatusBadGateway, "resync failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"commit":    res.CommitSHA,
		"published": res.Published,
		"drafts":    res.Drafts,
		"skipped":   res.Skipped,
		"images":    res.Images,
	})
}

// --- helpers ---

// decodePostRequest reads and JSON-decodes a bounded post request body.
func (s *Server) decodePostRequest(w http.ResponseWriter, r *http.Request) (adminPostRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPostJSON)
	var req adminPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return adminPostRequest{}, false
	}
	return req, true
}

// validatePost enforces the §10.3 field limits, then assembles the file bytes
// and validates them through content.ParsePost (the ingest validator), so admin
// rules are byte-for-byte identical to ingest rules.
func (s *Server) validatePost(w http.ResponseWriter, req adminPostRequest) (*content.ParsedPost, []byte, bool) {
	m := req.Meta
	if len(m.Title) > maxTitleLen {
		return s.badPost(w, "title too long (max 200)")
	}
	if len(m.Slug) > maxSlugLen {
		return s.badPost(w, "slug too long (max 100)")
	}
	if len(m.Summary) > maxSummaryLen {
		return s.badPost(w, "summary too long (max 500)")
	}
	if len(m.Tags) > maxTags {
		return s.badPost(w, "too many tags (max 20)")
	}
	for _, t := range m.Tags {
		if len(t) > maxTagLen {
			return s.badPost(w, "tag too long (max 40)")
		}
	}
	if m.Cover != "" && !strings.HasPrefix(m.Cover, "images/") {
		return s.badPost(w, "cover must be empty or an images/... path")
	}
	if len(req.Content) > maxBodyBytes {
		return s.badPost(w, "content too large (max 256KiB)")
	}

	// Decode just enough to bound the block count before rendering.
	var doc struct {
		Blocks []json.RawMessage `json:"blocks"`
	}
	if len(req.Content) > 0 {
		if err := json.Unmarshal(req.Content, &doc); err != nil {
			return s.badPost(w, "content must be an Editor.js document")
		}
	}
	if len(doc.Blocks) > maxBlocks {
		return s.badPost(w, "too many blocks (max 1000)")
	}

	meta := content.PostMeta{
		Title:     m.Title,
		Slug:      m.Slug,
		Date:      m.Date,
		Updated:   m.Updated,
		Summary:   m.Summary,
		Tags:      m.Tags,
		Published: m.Published,
		Cover:     m.Cover,
	}
	data, err := content.AssemblePostJSON(meta, req.Content)
	if err != nil {
		return s.badPost(w, "invalid content json")
	}
	parsed, err := content.ParsePost("posts/"+m.Slug+".json", data)
	if err != nil {
		return s.badPost(w, err.Error())
	}
	return parsed, data, true
}

func (s *Server) badPost(w http.ResponseWriter, msg string) (*content.ParsedPost, []byte, bool) {
	writeError(w, http.StatusBadRequest, msg)
	return nil, nil, false
}

// writeAdminWriteError maps internal/admin service errors to HTTP statuses.
func (s *Server) writeAdminWriteError(w http.ResponseWriter, op, slug string, err error) {
	var stale *admin.StaleError
	switch {
	case errors.As(err, &stale):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "stale", "currentFileSha": stale.CurrentFileSHA})
	case errors.Is(err, admin.ErrPostExists):
		writeError(w, http.StatusConflict, "post already exists")
	case errors.Is(err, admin.ErrGitHubWrite):
		s.log.Error(op, "slug", slug, "error", err.Error())
		writeError(w, http.StatusBadGateway, "github write failed")
	case errors.Is(err, admin.ErrCacheAfterGitHub):
		writeError(w, http.StatusInternalServerError, admin.ErrCacheAfterGitHub.Error())
	default:
		s.log.Error(op, "slug", slug, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// sniffImageType reports the file extension for a supported raster image type,
// or ok=false otherwise. SVG is intentionally unsupported (inline-served SVG is
// a stored-XSS vector).
func sniffImageType(data []byte) (ext string, ok bool) {
	switch http.DetectContentType(data) {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	}
	// Fallback WEBP sniff (RIFF....WEBP) for toolchains whose DetectContentType
	// does not classify webp.
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return ".webp", true
	}
	return "", false
}

// imageName builds a safe, deterministic, collision-free filename:
// <slug(stem)>-<first8(sha256)><ext>. The charset is [a-z0-9._-]; no traversal.
func imageName(filename, ext string, data []byte) string {
	stem := strings.TrimSuffix(path.Base(filename), path.Ext(filename))
	base := content.Slugify(stem)
	if base == "" {
		base = "image"
	}
	sum := sha256.Sum256(data)
	return base + "-" + hex.EncodeToString(sum[:])[:8] + ext
}

// isTooLarge reports whether err is the MaxBytesReader "request body too large"
// error.
func isTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return true
	}
	return strings.Contains(err.Error(), "request body too large")
}
