package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// groqCompletion wraps a draft object as a Groq chat-completion response (the
// draft JSON lives in choices[0].message.content as a string).
func groqCompletion(draft any) string {
	content, _ := json.Marshal(draft)
	body, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"content": string(content)}},
		},
	})
	return string(body)
}

// mockGroq returns an httptest server that answers /chat/completions by routing
// on the requested model: "good*" models return a valid draft, everything else
// returns a 500 with a Groq-style error body.
func mockGroq(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		if strings.HasPrefix(body.Model, "good") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(groqCompletion(map[string]any{
				"title":   "Great Post",
				"summary": "A short summary.",
				"slug":    "My Great Slug", // exercises server-side slugification
				"tags":    []string{"Go", "Web"},
				"blocks": []map[string]any{
					{"type": "header", "data": map[string]any{"text": "Intro", "level": 2}},
					{"type": "paragraph", "data": map[string]any{"text": "Body paragraph."}},
					{"type": "image", "data": map[string]any{"file": map[string]any{"url": "x.png"}}}, // dropped
				},
			})))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"model exploded"}}`))
	}))
}

// assistResponse is the parsed /api/admin/assist body.
type assistResponse struct {
	Candidates []struct {
		Model   string `json:"model"`
		Title   string `json:"title"`
		Slug    string `json:"slug"`
		Content struct {
			Blocks  []json.RawMessage `json:"blocks"`
			Version string            `json:"version"`
		} `json:"content"`
	} `json:"candidates"`
	Errors []struct {
		Model   string `json:"model"`
		Message string `json:"message"`
	} `json:"errors"`
}

// authedAssist builds a session+CSRF POST to /api/admin/assist with the given body.
func (h *harness) authedAssist(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/assist", strings.NewReader(body))
	csrfCookie, token := h.csrfPair()
	req.AddCookie(h.sessionCookie("admin@example.com"))
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", token)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestAIModelsEndpoint(t *testing.T) {
	h := newServer(t, options{
		adminEnabled: true,
		aiEnabled:    true,
		aiModels:     []string{"model-a", "model-b"},
		aiMaxModels:  3,
		aiBaseURL:    "http://unused.test",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/models", nil)
	req.AddCookie(h.sessionCookie("admin@example.com"))
	rr := h.serve(req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got struct {
		Models  []string `json:"models"`
		Default string   `json:"default"`
		Max     int      `json:"max"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Models) != 2 || got.Models[0] != "model-a" {
		t.Errorf("models = %v", got.Models)
	}
	if got.Default != "model-a" {
		t.Errorf("default = %q, want model-a", got.Default)
	}
	if got.Max != 3 {
		t.Errorf("max = %d, want 3", got.Max)
	}
}

func TestAIModelsDisabled503(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, aiEnabled: false})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/models", nil)
	req.AddCookie(h.sessionCookie("admin@example.com"))
	if rr := h.serve(req); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("models with AI disabled = %d, want 503", rr.Code)
	}
}

func TestAIAssistPartialFailure(t *testing.T) {
	mock := mockGroq(t)
	defer mock.Close()
	h := newServer(t, options{
		adminEnabled: true,
		aiEnabled:    true,
		aiModels:     []string{"good-model", "bad-model"},
		aiMaxModels:  4,
		aiBaseURL:    mock.URL,
	})

	rr := h.serve(h.authedAssist(`{"input":"write about go","language":"en","models":["good-model","bad-model"]}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (partial success): %s", rr.Code, rr.Body.String())
	}
	var got assistResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1: %s", len(got.Candidates), rr.Body.String())
	}
	c := got.Candidates[0]
	if c.Model != "good-model" {
		t.Errorf("candidate model = %q, want good-model", c.Model)
	}
	if c.Slug != "my-great-slug" {
		t.Errorf("slug = %q, want slugified my-great-slug", c.Slug)
	}
	// The unsupported image block is dropped; header + paragraph remain.
	if len(c.Content.Blocks) != 2 {
		t.Errorf("blocks = %d, want 2 (image dropped)", len(c.Content.Blocks))
	}
	if c.Content.Version == "" {
		t.Error("content envelope missing version")
	}
	if len(got.Errors) != 1 || got.Errors[0].Model != "bad-model" {
		t.Errorf("errors = %+v, want one for bad-model", got.Errors)
	}
	if got.Errors[0].Message == "" {
		t.Error("per-model error should carry a message")
	}
}

func TestAIAssistAllFail502(t *testing.T) {
	mock := mockGroq(t)
	defer mock.Close()
	h := newServer(t, options{
		adminEnabled: true,
		aiEnabled:    true,
		aiModels:     []string{"bad-model"},
		aiMaxModels:  4,
		aiBaseURL:    mock.URL,
	})
	rr := h.serve(h.authedAssist(`{"input":"x","models":["bad-model"]}`))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (all failed)", rr.Code)
	}
	var got assistResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Candidates) != 0 || len(got.Errors) != 1 {
		t.Errorf("want 0 candidates + 1 error, got %d / %d", len(got.Candidates), len(got.Errors))
	}
}

func TestAIAssistValidation(t *testing.T) {
	mock := mockGroq(t)
	defer mock.Close()
	h := newServer(t, options{
		adminEnabled: true,
		aiEnabled:    true,
		aiModels:     []string{"good-model", "good-two"},
		aiMaxModels:  2,
		aiBaseURL:    mock.URL,
	})
	cases := []struct {
		name string
		body string
	}{
		{"empty input", `{"input":"   ","models":["good-model"]}`},
		{"no models", `{"input":"hi","models":[]}`},
		{"unknown model", `{"input":"hi","models":["nope"]}`},
		{"too many models", `{"input":"hi","models":["good-model","good-two","good-model"]}`},
		{"bad language", `{"input":"hi","language":"fr","models":["good-model"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rr := h.serve(h.authedAssist(tc.body)); rr.Code != http.StatusBadRequest {
				t.Fatalf("%s = %d, want 400: %s", tc.name, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestAIAssistMissingCSRF403(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, aiEnabled: true, aiBaseURL: "http://unused.test"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/assist", strings.NewReader(`{"input":"x","models":["openai/gpt-oss-120b"]}`))
	req.AddCookie(h.sessionCookie("admin@example.com"))
	if rr := h.serve(req); rr.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF = %d, want 403", rr.Code)
	}
}

func TestAIAssistDisabled503(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, aiEnabled: false})
	if rr := h.serve(h.authedAssist(`{"input":"x","models":["m"]}`)); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("assist with AI disabled = %d, want 503", rr.Code)
	}
}

func TestAIAssistNoSession401(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, aiEnabled: true, aiBaseURL: "http://unused.test"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/assist", strings.NewReader(`{"input":"x","models":["openai/gpt-oss-120b"]}`))
	if rr := h.serve(req); rr.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d, want 401", rr.Code)
	}
}

func TestAdminMeIncludesAIEnabled(t *testing.T) {
	h := newServer(t, options{adminEnabled: true, aiEnabled: true, aiBaseURL: "http://unused.test"})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	req.AddCookie(h.sessionCookie("admin@example.com"))
	rr := h.serve(req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got struct {
		AIEnabled bool `json:"aiEnabled"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.AIEnabled {
		t.Error("expected aiEnabled=true in /api/admin/me")
	}
}
