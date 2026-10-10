package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// requireReady returns 503 with a lightweight warming body until the first
// ingest completes, keeping public routes off a half-built cache.
func (s *Server) requireReady(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ready, _ := s.readyState(); !ready {
			w.Header().Set("Retry-After", "5")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "warming"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logRequests logs one structured line per request. It deliberately logs only
// the path (not the raw query) and no headers, so nothing sensitive is logged.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		s.log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"durationMs", time.Since(start).Milliseconds(),
		)
	})
}
