// Package ai is a tiny client for Groq's OpenAI-compatible chat completions
// API, used by the admin AI-drafting feature to turn a short brief into a
// complete blog-post draft (title/summary/slug/tags + Editor.js blocks). The
// API key is sensitive and SERVER-SIDE ONLY: it is never logged, never returned
// to the browser, and never placed in an error. The baseURL field is unexported
// and overridable in tests (mirroring internal/contact/resend.go and
// internal/github/client.go).
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultBaseURL is Groq's OpenAI-compatible API base. The chat endpoint is
// <base>/chat/completions. Overridable via WithBaseURL (tests / GROQ_BASE_URL).
const defaultBaseURL = "https://api.groq.com/openai/v1"

// maxInputChars caps the brief the client forwards to the model, bounding
// prompt size (and cost). The handler rejects longer input first; this is a
// defensive truncation.
const maxInputChars = 4000

// Client calls Groq's chat completions endpoint with a bearer token.
type Client struct {
	apiKey  string // sensitive: never logged
	baseURL string
	http    *http.Client
}

// Option customizes a Client.
type Option func(*Client)

// WithBaseURL overrides the API base URL (default defaultBaseURL). A trailing
// slash is trimmed so <base>/chat/completions is well-formed.
func WithBaseURL(base string) Option {
	return func(c *Client) {
		if base != "" {
			c.baseURL = strings.TrimRight(base, "/")
		}
	}
}

// WithHTTPClient overrides the underlying *http.Client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// NewClient builds a client with a 60s HTTP timeout as a backstop; callers set a
// shorter per-call deadline via the request context (~45s per model).
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// GenerateRequest is one draft request for a single model.
type GenerateRequest struct {
	Input    string
	Language string // "auto" | "en" | "hi" (anything else is treated as auto)
	Model    string
}

// chat request/response shapes: the OpenAI-compatible subset Groq implements.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat map[string]any `json:"response_format"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// groqError is Groq's error envelope ({"error":{"message":...}}). The message is
// non-sensitive (rate-limit / bad-model text) and safe to surface.
type groqError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Generate asks one model for a draft and returns the raw (unsanitized) JSON the
// model produced, parsed into a Draft. The caller sanitizes with SanitizeDraft.
// response_format=json_object forces the model to emit a JSON object; the result
// is still validated here (and sanitized by the caller) because json_object mode
// does not guarantee the schema.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (Draft, error) {
	input := strings.TrimSpace(req.Input)
	if input == "" {
		return Draft{}, errors.New("input is required")
	}
	if len(input) > maxInputChars {
		input = input[:maxInputChars]
	}

	body, err := json.Marshal(chatRequest{
		Model: req.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt(input, req.Language)},
		},
		Temperature:    0.6,
		ResponseFormat: map[string]any{"type": "json_object"},
	})
	if err != nil {
		return Draft{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Draft{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Draft{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Draft{}, errorFromResponse(resp.StatusCode, raw)
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return Draft{}, fmt.Errorf("decode groq response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return Draft{}, errors.New("groq returned no choices")
	}
	msg := strings.TrimSpace(cr.Choices[0].Message.Content)
	if msg == "" {
		return Draft{}, errors.New("groq returned an empty message")
	}

	var d Draft
	if err := json.Unmarshal([]byte(msg), &d); err != nil {
		return Draft{}, fmt.Errorf("model did not return valid JSON: %w", err)
	}
	return d, nil
}

// errorFromResponse turns a non-2xx Groq response into an error carrying Groq's
// (non-sensitive) message when present, else just the status. The API key is
// never part of the response and so is never in the returned error.
func errorFromResponse(status int, body []byte) error {
	var ge groqError
	if json.Unmarshal(body, &ge) == nil && strings.TrimSpace(ge.Error.Message) != "" {
		return fmt.Errorf("groq status %d: %s", status, strings.TrimSpace(ge.Error.Message))
	}
	return fmt.Errorf("groq status %d", status)
}
