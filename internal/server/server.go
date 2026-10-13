// Package server wires the public HTTP API, health endpoints, image serving,
// and the readiness gate together behind a chi router.
package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"

	"github.com/iamhimansu/himanshurajaman.com/internal/admin"
	"github.com/iamhimansu/himanshurajaman.com/internal/ai"
	"github.com/iamhimansu/himanshurajaman.com/internal/auth"
	"github.com/iamhimansu/himanshurajaman.com/internal/contact"
	"github.com/iamhimansu/himanshurajaman.com/internal/ingest"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
)

// ImageSource is the minimal view the image handler needs onto the ingest-owned
// image set. Keeping it an interface avoids a server -> ingest dependency.
type ImageSource interface {
	Current() string
}

// Deps are the collaborators server.New needs. The admin-related fields are
// nil/false when the admin surface is disabled, in which case requireAdminEnabled
// turns every /auth/* and /api/admin/* request into 503 "admin disabled".
type Deps struct {
	Store  *store.Store
	Images ImageSource
	Index  []byte
	Dist   fs.FS
	Log    *slog.Logger

	// ConfigBaseURL is cfg.BaseURL (BASE_URL env), used as the fallback canonical
	// origin for server-side meta when settings.baseUrl is absent. May be "".
	ConfigBaseURL string

	// Admin wiring (all nil/false when admin is disabled).
	Sealer        *auth.Sealer        // cookie sealing (session/oauth/csrf)
	Auth          *auth.Authenticator // Google OIDC authenticator
	Admin         *admin.Service      // write-through CMS orchestration
	AdminEnabled  bool                // admin surface configured
	WritesEnabled bool                // AdminEnabled && content source is GitHub

	// Ingester runs the content pipeline; the webhook falls back to it when the
	// admin service is nil. WebhookSecret enables POST /webhook/github when set
	// (empty = endpoint disabled). WebhookSecret is sensitive: never log it.
	Ingester      *ingest.Ingester
	WebhookSecret string

	// Contact is the public contact-form email service, nil when contact is not
	// configured (SMTP env unset). When nil, POST /api/contact returns 503 and
	// GET /api/settings reports contactEnabled=false.
	Contact *contact.Service

	// AI drafting (all nil/false/zero when GROQ_API_KEY is unset). AI is the Groq
	// chat client; AIModels is the curated allowlist offered to the UI; AIMaxModels
	// caps models per /assist request. When AIEnabled is false the models/assist
	// endpoints return 503 and GET /api/admin/me reports aiEnabled=false.
	AI          *ai.Client
	AIEnabled   bool
	AIModels    []string
	AIMaxModels int
}

// Server holds shared state and the composed HTTP handler.
type Server struct {
	store  *store.Store
	images ImageSource
	index  []byte
	dist   fs.FS
	log    *slog.Logger

	// configBaseURL is the BASE_URL fallback origin. shellHead/shellTail are the
	// embedded index.html split once at boot around </head> (with the static
	// <title>/description stripped) so writeDocument can inject per-request meta.
	// shellHead is nil when the split failed, in which case writeDocument falls
	// back to serving the raw shell.
	configBaseURL string
	shellHead     []byte
	shellTail     []byte

	sealer        *auth.Sealer
	auth          *auth.Authenticator
	admin         *admin.Service
	adminEnabled  bool
	writesEnabled bool

	// ingester backs the webhook resync when admin is off; webhookSecret gates
	// the webhook endpoint (empty = 404); resyncing is the single-flight guard
	// (atomic 0/1) so overlapping webhook resyncs are skipped.
	ingester      *ingest.Ingester
	webhookSecret string
	resyncing     int32

	// contact is the contact-form email service (nil = contact disabled).
	contact *contact.Service

	// ai drafting (nil/false/zero when disabled). aiModels is the selectable chat
	// allowlist; aiMaxModels caps models per /assist request.
	ai          *ai.Client
	aiEnabled   bool
	aiModels    []string
	aiMaxModels int

	mu     sync.RWMutex
	ready  bool
	commit string

	handler http.Handler
}

// New builds the server. Deps.Index is the embedded index.html shell and
// Deps.Dist is the embedded built frontend tree (its /assets are served as
// static files; every other non-API path falls back to index.html for
// client-side routing).
func New(d Deps) *Server {
	s := &Server{
		store:         d.Store,
		images:        d.Images,
		index:         d.Index,
		dist:          d.Dist,
		log:           d.Log,
		configBaseURL: d.ConfigBaseURL,
		sealer:        d.Sealer,
		auth:          d.Auth,
		admin:         d.Admin,
		adminEnabled:  d.AdminEnabled,
		writesEnabled: d.WritesEnabled,
		ingester:      d.Ingester,
		webhookSecret: d.WebhookSecret,
		contact:       d.Contact,
		ai:            d.AI,
		aiEnabled:     d.AIEnabled,
		aiModels:      d.AIModels,
		aiMaxModels:   d.AIMaxModels,
	}
	// Split the shell once so each document response is shellHead + generated
	// meta + shellTail. splitShell returns a nil tail when </head> is absent;
	// that is a failed split, so we leave shellHead nil and writeDocument serves
	// the raw shell instead.
	if head, tail := splitShell(d.Index); tail != nil {
		s.shellHead, s.shellTail = head, tail
	} else if d.Log != nil {
		d.Log.Warn("index.html shell split failed; serving raw shell without server-side meta")
	}
	s.handler = s.routes()
	return s
}

// MarkReady flips readiness true and records the synced commit. Called by the
// bootstrap goroutine after the first successful ingest.
func (s *Server) MarkReady(commit string) {
	s.mu.Lock()
	s.ready = true
	s.commit = commit
	s.mu.Unlock()
}

func (s *Server) readyState() (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready, s.commit
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// nullable returns nil for an empty string so JSON renders null, not "".
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
