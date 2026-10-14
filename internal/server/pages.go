package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iamhimansu/himanshurajaman.com/internal/store"
)

// handleGetPage serves GET /api/pages/{slug}: a published page plus its
// published posts in one round-trip. It reads only the cache. An unknown or
// draft slug is a JSON 404; the posts list is always a non-nil slice.
func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	page, err := s.store.GetPage(r.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "page not found")
			return
		}
		s.log.Error("get page", "slug", slug, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	posts, err := s.store.PostsByPage(r.Context(), slug, true)
	if err != nil {
		s.log.Error("get page posts", "slug", slug, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"page": page, "posts": posts})
}
