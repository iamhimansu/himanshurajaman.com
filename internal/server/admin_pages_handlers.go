package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
)

// --- request/response shapes ---

type adminPageRequest struct {
	Meta    adminPageMeta   `json:"meta"`
	Content json.RawMessage `json:"content"`
	FileSha string          `json:"fileSha"`
}

type adminPageMeta struct {
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	Nav       bool   `json:"nav"`
	NavLabel  string `json:"navLabel"`
	NavOrder  *int   `json:"navOrder"`
	Published bool   `json:"published"`
	Summary   string `json:"summary"`
	Updated   string `json:"updated"`
}

// --- page handlers ---

func (s *Server) handleAdminListPages(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListPages(r.Context())
	if err != nil {
		s.log.Error("admin list pages", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAdminGetPage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	ap, err := s.store.GetAdminPage(r.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "page not found")
			return
		}
		s.log.Error("admin get page", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	navOrder := ap.NavOrder
	meta := adminPageMeta{
		Title:     ap.Title,
		Slug:      ap.Slug,
		Nav:       ap.Nav,
		NavLabel:  ap.NavLabel,
		NavOrder:  &navOrder,
		Published: ap.Published,
		Summary:   ap.Summary,
		Updated:   ap.Updated,
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

func (s *Server) handleAdminCreatePage(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodePageRequest(w, r)
	if !ok {
		return
	}
	parsed, data, ok := s.validatePage(w, req)
	if !ok {
		return
	}

	fileSha, err := s.admin.CreatePage(r.Context(), parsed, data)
	if err != nil {
		s.writeAdminWriteError(w, "create page", parsed.Slug, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"slug": parsed.Slug, "fileSha": fileSha})
}

func (s *Server) handleAdminUpdatePage(w http.ResponseWriter, r *http.Request) {
	pathSlug := chi.URLParam(r, "slug")
	req, ok := s.decodePageRequest(w, r)
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
	// date populates, overriding any client value. Mirrors the post handler.
	req.Meta.Updated = time.Now().UTC().Format(time.RFC3339)
	parsed, data, ok := s.validatePage(w, req)
	if !ok {
		return
	}

	fileSha, err := s.admin.UpdatePage(r.Context(), parsed, data, req.FileSha)
	if err != nil {
		s.writeAdminWriteError(w, "update page", parsed.Slug, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slug": parsed.Slug, "fileSha": fileSha})
}

func (s *Server) handleAdminDeletePage(w http.ResponseWriter, r *http.Request) {
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

	if err := s.admin.DeletePage(r.Context(), slug, sha); err != nil {
		s.writeAdminWriteError(w, "delete page", slug, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// --- helpers ---

// decodePageRequest reads and JSON-decodes a bounded page request body.
func (s *Server) decodePageRequest(w http.ResponseWriter, r *http.Request) (adminPageRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPostJSON)
	var req adminPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return adminPageRequest{}, false
	}
	return req, true
}

// validatePage enforces the §10.3 field limits (reusing the post caps), then
// assembles the file bytes and re-validates them through content.ParsePage (the
// ingest validator), so admin page rules are byte-for-byte identical to ingest
// rules — including the reserved-slug and slug-pattern checks (surfaced as 400).
func (s *Server) validatePage(w http.ResponseWriter, req adminPageRequest) (*content.ParsedPage, []byte, bool) {
	m := req.Meta
	if len(m.Title) > maxTitleLen {
		return s.badPage(w, "title too long (max 200)")
	}
	if len(m.Slug) > maxSlugLen {
		return s.badPage(w, "slug too long (max 100)")
	}
	if len(m.Summary) > maxSummaryLen {
		return s.badPage(w, "summary too long (max 500)")
	}
	if len(req.Content) > maxBodyBytes {
		return s.badPage(w, "content too large (max 256KiB)")
	}

	// Decode just enough to bound the block count before rendering.
	var doc struct {
		Blocks []json.RawMessage `json:"blocks"`
	}
	if len(req.Content) > 0 {
		if err := json.Unmarshal(req.Content, &doc); err != nil {
			return s.badPage(w, "content must be an Editor.js document")
		}
	}
	if len(doc.Blocks) > maxBlocks {
		return s.badPage(w, "too many blocks (max 1000)")
	}

	meta := content.PageMeta{
		Title:     m.Title,
		Slug:      m.Slug,
		Nav:       m.Nav,
		NavLabel:  m.NavLabel,
		NavOrder:  m.NavOrder,
		Published: m.Published,
		Summary:   m.Summary,
		Updated:   m.Updated,
	}
	data, err := content.AssemblePageJSON(meta, req.Content)
	if err != nil {
		return s.badPage(w, "invalid content json")
	}
	parsed, err := content.ParsePage("pages/"+m.Slug+".json", data)
	if err != nil {
		return s.badPage(w, err.Error())
	}
	return parsed, data, true
}

func (s *Server) badPage(w http.ResponseWriter, msg string) (*content.ParsedPage, []byte, bool) {
	writeError(w, http.StatusBadRequest, msg)
	return nil, nil, false
}
