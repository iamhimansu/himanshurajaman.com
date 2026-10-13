package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// completion wraps a model JSON object as a Groq chat-completion response, the
// same envelope the real API returns (the draft JSON lives in message.content).
func completion(contentJSON string) string {
	body, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": contentJSON}},
		},
	})
	return string(body)
}

func TestClientGenerate_Success(t *testing.T) {
	var (
		gotAuth string
		gotPath string
		gotBody map[string]any
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(completion(`{"title":"T","summary":"S","slug":"my-slug","tags":["go"],"blocks":[{"type":"paragraph","data":{"text":"Hi"}}]}`)))
	}))
	defer ts.Close()

	c := NewClient("gsk_secret", WithBaseURL(ts.URL))
	d, err := c.Generate(context.Background(), GenerateRequest{Input: "write about go", Language: "en", Model: "openai/gpt-oss-120b"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer gsk_secret" {
		t.Errorf("Authorization = %q, want Bearer gsk_secret", gotAuth)
	}
	if gotBody["model"] != "openai/gpt-oss-120b" {
		t.Errorf("model = %v, want openai/gpt-oss-120b", gotBody["model"])
	}
	rf, _ := gotBody["response_format"].(map[string]any)
	if rf["type"] != "json_object" {
		t.Errorf("response_format = %v, want type json_object", gotBody["response_format"])
	}
	if d.Title != "T" || d.Slug != "my-slug" || len(d.Blocks) != 1 {
		t.Errorf("decoded draft = %+v", d)
	}
}

func TestClientGenerate_ContentNotJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(completion("this is not JSON at all")))
	}))
	defer ts.Close()

	c := NewClient("gsk_secret", WithBaseURL(ts.URL))
	if _, err := c.Generate(context.Background(), GenerateRequest{Input: "x", Model: "m"}); err == nil {
		t.Fatal("expected an error when message content is not JSON")
	}
}

func TestClientGenerate_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit reached","type":"rate_limit"}}`))
	}))
	defer ts.Close()

	c := NewClient("gsk_secret", WithBaseURL(ts.URL))
	_, err := c.Generate(context.Background(), GenerateRequest{Input: "x", Model: "m"})
	if err == nil {
		t.Fatal("expected an error on a non-2xx response")
	}
	if !strings.Contains(err.Error(), "Rate limit reached") {
		t.Errorf("error should surface Groq's message, got %v", err)
	}
	if strings.Contains(err.Error(), "gsk_secret") {
		t.Error("error must not leak the API key")
	}
}

func TestClientGenerate_EmptyInput(t *testing.T) {
	c := NewClient("gsk_secret")
	if _, err := c.Generate(context.Background(), GenerateRequest{Input: "   ", Model: "m"}); err == nil {
		t.Fatal("expected an error for empty input")
	}
}
