package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	// RealIP rewrites r.RemoteAddr from the X-Forwarded-For / X-Real-IP headers.
	// TRUSTED-PROXY ASSUMPTION: in production the service runs only behind
	// Render's proxy, which terminates TLS and sets these headers, so the
	// forwarded client address is trustworthy and the per-IP contact rate limiter
	// (see clientIP in contact.go) keys off the real client. Do NOT keep this if
	// the app is ever exposed directly to clients, since the headers are then
	// client-spoofable.
	r.Use(middleware.RealIP)
	r.Use(s.logRequests)
	r.Use(middleware.Recoverer)

	// Health endpoints: always available, never gated, and must never call
	// GitHub. /healthz is what the external pinger hits every 14 minutes.
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	// Public read API, gated by readiness.
	r.Route("/api", func(r chi.Router) {
		r.Use(s.requireReady)
		r.Get("/posts", s.handleListPosts)
		r.Get("/posts/{slug}", s.handleGetPost)
		r.Get("/tags", s.handleTags)
		r.Get("/resume", s.handleResume)
		r.Get("/settings", s.handleSettings)
		r.Get("/search", s.handleSearch)
	})

	// Admin auth + CRUD. [DR6] These live in a SEPARATE group, NOT inside the
	// readiness-gated /api block above, so unauthenticated admin calls return
	// 401 (auth-before-readiness), not 503. requireReady is applied only to the
	// two admin GET reads. requireAdminEnabled short-circuits every route with
	// 503 "admin disabled" when the admin surface is off. Registered before the
	// SPA fallback so these paths are never swallowed by index.html.
	r.Group(func(r chi.Router) {
		// Auth (§10.2).
		r.With(s.requireAdminEnabled).Get("/auth/login", s.handleAuthLogin)
		r.With(s.requireAdminEnabled).Get("/auth/callback", s.handleAuthCallback)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireCSRF).Post("/auth/logout", s.handleAuthLogout)

		// Admin API (§10.2).
		r.With(s.requireAdminEnabled, s.requireSession).Get("/api/admin/me", s.handleAdminMe)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireReady).Get("/api/admin/posts", s.handleAdminListPosts)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireReady).Get("/api/admin/posts/{slug}", s.handleAdminGetPost)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireCSRF, s.requireWrites).Post("/api/admin/posts", s.handleAdminCreatePost)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireCSRF, s.requireWrites).Put("/api/admin/posts/{slug}", s.handleAdminUpdatePost)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireCSRF, s.requireWrites).Delete("/api/admin/posts/{slug}", s.handleAdminDeletePost)
		r.With(s.requireAdminEnabled, s.requireSession).Get("/api/admin/resume", s.handleAdminGetResume)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireCSRF, s.requireWrites).Put("/api/admin/resume", s.handleAdminPutResume)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireCSRF, s.requireWrites).Post("/api/admin/images", s.handleAdminUploadImage)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireCSRF).Post("/api/admin/preview", s.handleAdminPreview)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireCSRF).Post("/api/admin/resync", s.handleAdminResync)

		// AI drafting (admin-only, gated by requireAIEnabled → 503 when
		// GROQ_API_KEY is unset). /assist triggers external paid calls, so it is
		// CSRF-protected like the write routes; /models is a read (no CSRF).
		r.With(s.requireAdminEnabled, s.requireSession, s.requireAIEnabled).Get("/api/admin/models", s.handleAdminModels)
		r.With(s.requireAdminEnabled, s.requireSession, s.requireCSRF, s.requireAIEnabled).Post("/api/admin/assist", s.handleAdminAssist)
	})

	// Public contact form. Deliberately OUTSIDE the readiness-gated /api group
	// (it needs only config + SMTP, not the content cache) and registered before
	// the SPA fallback so index.html never swallows it. When contact is not
	// configured the handler returns 503 "contact unavailable".
	r.Post("/api/contact", s.handleContact)

	// Images are also gated (the set is empty until the first ingest anyway).
	r.With(s.requireReady).Get("/images/*", s.handleImage)

	// Static frontend. Hashed build assets are served from the embedded dist
	// tree with a long immutable cache. Every other non-API path (/, /resume,
	// /blog, /blog/:slug, /search, /admin, and unknown paths) falls back to the
	// embedded index.html so client-side deep links and refreshes work. These
	// are deliberately NOT readiness-gated: the SPA shell must load so it can
	// render its own warming state while /api and /images return 503.
	// Crawler / syndication surfaces. Sitemap and RSS are readiness-gated (503
	// while warming) and read only the cache; robots is ungated so crawlers
	// always get a valid policy. Registered before the SPA fallback so they are
	// never swallowed by index.html.
	r.With(s.requireReady).Get("/sitemap.xml", s.handleSitemap)
	r.With(s.requireReady).Get("/rss.xml", s.handleRSS)
	r.Get("/robots.txt", s.handleRobots)

	// GitHub push-to-resync webhook. Ungated (GitHub cannot wait on readiness)
	// and registered before the SPA fallback. Disabled (404) when no secret is
	// configured; see handleGitHubWebhook.
	r.Post("/webhook/github", s.handleGitHubWebhook)

	r.Get("/assets/*", s.handleAsset)
	r.Get("/", s.handleIndex)
	r.NotFound(s.handleSPAFallback)

	return r
}
