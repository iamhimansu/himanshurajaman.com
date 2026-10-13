package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iamhimansu/himanshurajaman.com/internal/ai"
)

// AI drafting limits. The input cap matches the client's (internal/ai) and
// bounds prompt cost; the per-model timeout bounds a single Groq call so one
// slow model cannot stall the whole fan-out.
const (
	maxAIInput        = 4000
	aiPerModelTimeout = 45 * time.Second
	maxAIRequestBytes = 64 * 1024
)

// aiAssistRequest is the POST /api/admin/assist body.
type aiAssistRequest struct {
	Input    string   `json:"input"`
	Language string   `json:"language"`
	Models   []string `json:"models"`
}

// handleAdminModels returns the selectable chat-model allowlist, the default
// (first allowlist entry), and the per-request cap so the UI can both pre-check
// the default and stop the author selecting more than GROQ_MAX_MODELS. Runs
// behind requireAdminEnabled + requireSession + requireAIEnabled.
func (s *Server) handleAdminModels(w http.ResponseWriter, _ *http.Request) {
	def := ""
	if len(s.aiModels) > 0 {
		def = s.aiModels[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"models":  s.aiModels,
		"default": def,
		"max":     s.aiMaxModels,
	})
}

// handleAdminAssist fans a brief out to the selected models CONCURRENTLY (one
// Groq call per model, each with its own timeout) and returns one sanitized
// candidate draft per model that succeeded plus a per-model error for each that
// failed. Partial success is 200; all-failed is 502. Runs behind
// requireAdminEnabled + requireSession + requireCSRF + requireAIEnabled.
func (s *Server) handleAdminAssist(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAIRequestBytes)
	var req aiAssistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input := strings.TrimSpace(req.Input)
	if input == "" {
		writeError(w, http.StatusBadRequest, "input is required")
		return
	}
	if len(input) > maxAIInput {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("input too long (max %d)", maxAIInput))
		return
	}

	language, ok := normalizeLanguage(req.Language)
	if !ok {
		writeError(w, http.StatusBadRequest, "language must be auto, en, or hi")
		return
	}

	models, msg := s.validateModels(req.Models)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	// Fan out concurrently; each goroutine writes only its own result slot, so no
	// lock is needed. The per-model context is derived from the request context so
	// a client disconnect cancels the in-flight calls.
	type result struct {
		model  string
		cand   *ai.Candidate
		errMsg string
	}
	results := make([]result, len(models))
	var wg sync.WaitGroup
	for i, model := range models {
		wg.Add(1)
		go func(i int, model string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), aiPerModelTimeout)
			defer cancel()

			draft, err := s.ai.Generate(ctx, ai.GenerateRequest{Input: input, Language: language, Model: model})
			if err != nil {
				results[i] = result{model: model, errMsg: aiErrorMessage(err)}
				return
			}
			cand, serr := ai.SanitizeDraft(model, draft)
			if serr != nil {
				results[i] = result{model: model, errMsg: serr.Error()}
				return
			}
			results[i] = result{model: model, cand: &cand}
		}(i, model)
	}
	wg.Wait()

	candidates := make([]ai.Candidate, 0, len(models))
	errs := make([]map[string]string, 0)
	for _, res := range results {
		if res.cand != nil {
			candidates = append(candidates, *res.cand)
			continue
		}
		errs = append(errs, map[string]string{"model": res.model, "message": res.errMsg})
	}

	body := map[string]any{"candidates": candidates, "errors": errs}
	if len(candidates) == 0 {
		// Every model failed: surface the per-model reasons with 502.
		writeJSON(w, http.StatusBadGateway, body)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// validateModels checks the requested models are a non-empty, de-duplicated
// subset of the allowlist within the per-request cap. It returns the cleaned
// list, or a 400 message describing the first problem.
func (s *Server) validateModels(models []string) ([]string, string) {
	if len(models) == 0 {
		return nil, "select at least one model"
	}
	if len(models) > s.aiMaxModels {
		return nil, fmt.Sprintf("too many models (max %d)", s.aiMaxModels)
	}
	allow := make(map[string]bool, len(s.aiModels))
	for _, m := range s.aiModels {
		allow[m] = true
	}
	seen := make(map[string]bool, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		if !allow[m] {
			return nil, "unknown model: " + m
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out, ""
}

// normalizeLanguage validates the optional language selector. Empty maps to
// "auto"; "auto"/"en"/"hi" pass through; anything else is rejected.
func normalizeLanguage(lang string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "auto":
		return "auto", true
	case "en":
		return "en", true
	case "hi":
		return "hi", true
	default:
		return "", false
	}
}

// aiErrorMessage renders a per-model failure for the UI without leaking the API
// key (the ai client never puts the key in an error) and bounds its length.
func aiErrorMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "request timed out"
	}
	msg := strings.TrimSpace(err.Error())
	const max = 200
	if len(msg) > max {
		msg = msg[:max]
	}
	if msg == "" {
		msg = "request failed"
	}
	return msg
}
