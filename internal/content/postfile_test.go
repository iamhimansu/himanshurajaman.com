package content_test

import (
	"strings"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
)

const validPostJSON = `{
  "meta": {
    "title": "Hello World",
    "slug": "hello-world",
    "date": "2024-03-02T09:00:00+05:30",
    "updated": "2024-03-05T12:30:00Z",
    "summary": "A summary.",
    "tags": ["Go", "go", "Benchmarks"],
    "published": true
  },
  "content": {"blocks": [{"type":"paragraph","data":{"text":"hi"}}]}
}`

func TestParsePost_Validation(t *testing.T) {
	t.Run("valid post normalizes tags and dates", func(t *testing.T) {
		p, err := content.ParsePost("posts/hello-world.json", []byte(validPostJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !p.Published {
			t.Error("expected published true")
		}
		// "Go" and "go" slugify to the same slug and dedupe; "Benchmarks" stays.
		if len(p.Tags) != 2 {
			t.Fatalf("expected 2 deduped tags, got %d: %+v", len(p.Tags), p.Tags)
		}
		if p.Tags[0].Name != "Go" || p.Tags[0].Slug != "go" {
			t.Errorf("unexpected first tag: %+v", p.Tags[0])
		}
		// Date normalized to UTC (the +05:30 offset becomes 03:30Z).
		if p.Date != "2024-03-02T03:30:00Z" {
			t.Errorf("date not normalized to UTC: %q", p.Date)
		}
		if p.Updated != "2024-03-05T12:30:00Z" {
			t.Errorf("updated not normalized: %q", p.Updated)
		}
	})

	t.Run("published defaults to false when absent", func(t *testing.T) {
		src := `{"meta":{"title":"T","slug":"a-post","date":"2024-01-01T00:00:00Z","summary":"s"},"content":{"blocks":[]}}`
		p, err := content.ParsePost("posts/a-post.json", []byte(src))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Published {
			t.Error("expected published to default to false")
		}
	})

	invalid := []struct {
		name string
		path string
		src  string
	}{
		{"missing title", "posts/x.json", `{"meta":{"slug":"x","date":"2024-01-01T00:00:00Z","summary":"s"},"content":{}}`},
		{"missing slug", "posts/x.json", `{"meta":{"title":"T","date":"2024-01-01T00:00:00Z","summary":"s"},"content":{}}`},
		{"bad slug shape", "posts/Bad_Slug.json", `{"meta":{"title":"T","slug":"Bad_Slug","date":"2024-01-01T00:00:00Z","summary":"s"},"content":{}}`},
		{"slug not equal to stem", "posts/other.json", `{"meta":{"title":"T","slug":"hello","date":"2024-01-01T00:00:00Z","summary":"s"},"content":{}}`},
		{"missing summary", "posts/x.json", `{"meta":{"title":"T","slug":"x","date":"2024-01-01T00:00:00Z"},"content":{}}`},
		{"missing date", "posts/x.json", `{"meta":{"title":"T","slug":"x","summary":"s"},"content":{}}`},
		{"bad date", "posts/x.json", `{"meta":{"title":"T","slug":"x","date":"03/02/2024","summary":"s"},"content":{}}`},
		{"bad updated", "posts/x.json", `{"meta":{"title":"T","slug":"x","date":"2024-01-01T00:00:00Z","updated":"nope","summary":"s"},"content":{}}`},
		{"malformed json", "posts/x.json", `{not json`},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := content.ParsePost(tt.path, []byte(tt.src)); err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestParsePost_EmptyContentDefaults(t *testing.T) {
	src := `{"meta":{"title":"T","slug":"empty-post","date":"2024-01-01T00:00:00Z","summary":"s"},"content":null}`
	p, err := content.ParsePost("posts/empty-post.json", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Document.Blocks == nil || len(p.Document.Blocks) != 0 {
		t.Errorf("expected empty block slice, got %+v", p.Document.Blocks)
	}
	if string(p.DocumentJSON) != `{"blocks":[]}` {
		t.Errorf("empty content should canonicalize to {\"blocks\":[]}, got %q", p.DocumentJSON)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Hello World", "hello-world"},
		{"  Multiple   Spaces  ", "multiple-spaces"},
		{"Already-Slug", "already-slug"},
		{"Trailing-", "trailing"},
		{"C++ & Go!", "c-go"},
		{"snake_case_name", "snake-case-name"},
		{"___", ""},
		{"MiXeD123", "mixed123"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := content.Slugify(tt.in); got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGitBlobSHA(t *testing.T) {
	// Values verified against `git hash-object --stdin`.
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"},
		{"hello", "hello", "b6fc4c620b67d95f953a5c1c1230aaab5db5a1b0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := content.GitBlobSHA([]byte(tt.in)); got != tt.want {
				t.Errorf("GitBlobSHA(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	// Spot-check it tracks the documented sha1("blob <len>\0"+data) construction.
	if !strings.HasPrefix(content.GitBlobSHA([]byte("hello")), "b6fc4c6") {
		t.Error("unexpected sha prefix")
	}
}
