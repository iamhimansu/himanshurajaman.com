package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/iamhimansu/himanshurajaman.com/internal/store"
)

func (s *Server) handleListPosts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, next, err := s.store.ListPosts(r.Context(), store.ListParams{
		Limit:  s.resolveLimit(r.Context(), q.Get("limit")),
		Cursor: q.Get("cursor"),
		Tag:    q.Get("tag"),
	})
	if err != nil {
		if errors.Is(err, store.ErrBadCursor) {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		s.log.Error("list posts", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nullable(next)})
}

func (s *Server) handleGetPost(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	post, err := s.store.GetPost(r.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "post not found")
			return
		}
		s.log.Error("get post", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, post)
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.TagsWithCounts(r.Context())
	if err != nil {
		s.log.Error("tags", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tags})
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	sections, err := s.store.Resume(r.Context())
	if err != nil {
		s.log.Error("resume", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sections": sections})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		s.log.Error("settings", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// contactEnabled is server-authoritative (derived from config, never from
	// site.json): the SPA uses it to show the form or a mailto fallback. Set it
	// after loading so any value in the ingested settings cannot override it.
	if settings == nil {
		settings = map[string]json.RawMessage{}
	}
	if s.contact != nil {
		settings["contactEnabled"] = json.RawMessage("true")
	} else {
		settings["contactEnabled"] = json.RawMessage("false")
	}

	// nav is server-computed: the fixed core items followed by published,
	// nav-enabled pages. It is set after loading so it overwrites any nav value
	// present in the ingested site.json. Nav never 500s: a store or marshal
	// error degrades to the core items only.
	navPages, err := s.store.ListNavPages(r.Context())
	if err != nil {
		s.log.Error("settings nav", "error", err.Error())
		navPages = nil
	}
	raw, merr := json.Marshal(buildNavJSON(navPages))
	if merr != nil {
		s.log.Error("settings nav marshal", "error", merr.Error())
		raw = []byte("[]")
	}
	settings["nav"] = json.RawMessage(raw)

	writeJSON(w, http.StatusOK, settings)
}

// navLink is one computed navigation item returned in settings.nav.
type navLink struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// buildNavJSON returns the computed nav: the five fixed core items in order,
// followed by one item per published nav-enabled page (in ListNavPages order)
// pointing at "/"+slug.
func buildNavJSON(navPages []store.NavPage) []navLink {
	out := []navLink{
		{Label: "Home", Path: "/"},
		{Label: "Works", Path: "/works"},
		{Label: "Blog", Path: "/blog"},
		{Label: "Resume", Path: "/resume"},
		{Label: "Contact", Path: "/contact"},
	}
	for _, p := range navPages {
		out = append(out, navLink{Label: p.Label, Path: "/" + p.Slug})
	}
	return out
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, next, err := s.store.Search(r.Context(), q.Get("q"), q.Get("cursor"),
		s.resolveLimit(r.Context(), q.Get("limit")))
	if err != nil {
		if errors.Is(err, store.ErrBadCursor) {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		s.log.Error("search", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nullable(next)})
}

// handleImage serves a file from the active image set with a long immutable
// cache lifetime. It cleans the request path and verifies the resolved file
// stays within the image directory (no traversal, no directory listing).
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	dir := s.images.Current()
	if dir == "" {
		http.NotFound(w, r)
		return
	}

	rel := strings.TrimPrefix(path.Clean("/"+chi.URLParam(r, "*")), "/")
	if rel == "" || rel == "." {
		http.NotFound(w, r)
		return
	}

	full := filepath.Join(dir, filepath.FromSlash(rel))
	rootAbs, err1 := filepath.Abs(dir)
	fullAbs, err2 := filepath.Abs(full)
	if err1 != nil || err2 != nil ||
		(fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+string(os.PathSeparator))) {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	if ct := mime.TypeByExtension(filepath.Ext(full)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// Content is immutable per ingest; images change only via a new ingest,
	// which the frontend will cache-bust by filename/commit as needed.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// handleAsset serves a hashed build asset from the embedded dist/assets tree.
// Filenames are content-hashed by Vite, so a long immutable cache is safe. A
// missing asset is a 404 (not the SPA shell) so broken asset URLs are obvious.
func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(path.Clean("/"+chi.URLParam(r, "*")), "/")
	if rel == "" || rel == "." {
		http.NotFound(w, r)
		return
	}

	name := "assets/" + rel
	f, err := s.dist.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

	// Embedded files implement io.Seeker, so ServeContent gives us range
	// support and correct conditional handling; fall back to a plain copy.
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, info.Name(), info.ModTime(), rs)
		return
	}
	_, _ = io.Copy(w, f)
}

// handleIndex serves the embedded index.html shell for GET /, injecting
// server-side SEO/OG/Twitter meta resolved from the cache (settings + GetPost).
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	meta, status := s.resolveDocMeta(r.Context(), r)
	s.writeDocument(w, status, meta)
}

// handleSPAFallback serves the index.html shell for unmatched routes so client
// deep links and refreshes render (e.g. /resume, /blog/:slug, /admin), with the
// same server-side meta injection as handleIndex. Unknown /api/* paths are the
// exception: they get a JSON 404 instead of the HTML shell, so API clients
// never receive a page where they expect data.
func (s *Server) handleSPAFallback(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	meta, status := s.resolveDocMeta(r.Context(), r)
	s.writeDocument(w, status, meta)
}

// resolveLimit picks the page size: an explicit positive ?limit wins, else
// settings.postsPerPage, else 10. The store clamps to its max.
func (s *Server) resolveLimit(ctx context.Context, raw string) int {
	if raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if n := s.store.PostsPerPage(ctx); n > 0 {
		return n
	}
	return 10
}
