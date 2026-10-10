package testutil

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// PostInput describes a post fixture. When Content is empty a single paragraph
// block carrying Body is generated, so most callers only set the metadata and a
// Body string.
type PostInput struct {
	Slug      string
	Title     string
	Summary   string
	Date      string // RFC3339
	Updated   string // RFC3339 or ""
	Cover     string // "images/..." or ""
	Tags      []string
	Published bool
	Body      string          // becomes a single paragraph when Content is empty
	Content   json.RawMessage // raw Editor.js OutputData; overrides Body when set
}

// PostJSON renders a post file ({meta, content}) as bytes that content.ParsePost
// accepts. The exact byte layout is not canonical (ParsePost only decodes it);
// callers that need the canonical form use content.AssemblePostJSON.
func PostJSON(in PostInput) []byte {
	content := in.Content
	if len(content) == 0 {
		text := in.Body
		if text == "" {
			text = "Default body text."
		}
		content = paragraphDoc(text)
	}
	meta := map[string]any{
		"title":     in.Title,
		"slug":      in.Slug,
		"date":      in.Date,
		"summary":   in.Summary,
		"published": in.Published,
	}
	if in.Updated != "" {
		meta["updated"] = in.Updated
	}
	if len(in.Tags) > 0 {
		meta["tags"] = in.Tags
	}
	if in.Cover != "" {
		meta["cover"] = in.Cover
	}
	b, _ := json.Marshal(map[string]any{"meta": meta, "content": content})
	return b
}

// paragraphDoc builds a minimal Editor.js document with one paragraph block.
func paragraphDoc(text string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"blocks": []any{
			map[string]any{"type": "paragraph", "data": map[string]any{"text": text}},
		},
	})
	return b
}

// SamplePostContent is the canonical Editor.js document exercised across the
// suite: a header, an inline-formatted paragraph (bold/italic/link/inline-code),
// a Go code block, and a table with headings.
func SamplePostContent() json.RawMessage {
	blocks := []any{
		map[string]any{"type": "header", "data": map[string]any{"text": "Hello Heading", "level": 2}},
		map[string]any{"type": "paragraph", "data": map[string]any{
			"text": `A paragraph with <b>bold</b>, <i>italic</i>, a <a href="https://example.com">link</a>, ` +
				`and <code class="inline-code">inline code</code>, mentioning goroutine scheduling.`,
		}},
		map[string]any{"type": "code", "data": map[string]any{
			"code":     "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello, world\")\n}",
			"language": "go",
		}},
		map[string]any{"type": "table", "data": map[string]any{
			"withHeadings": true,
			"content":      [][]string{{"Case", "ns/op"}, {"baseline", "120"}, {"optimized", "95"}},
		}},
	}
	b, _ := json.Marshal(map[string]any{"blocks": blocks, "version": "2.31.7"})
	return b
}

// ResumeJSON returns a valid resume document (basics with name+email plus a
// couple of sections) that ingest.BuildResume accepts.
func ResumeJSON() []byte {
	b, _ := json.Marshal(map[string]any{
		"basics": map[string]any{
			"name":     "Test Person",
			"email":    "test@example.com",
			"headline": "Backend engineer",
		},
		"sections": []any{
			map[string]any{"kind": "experience", "title": "Experience", "items": []any{}},
			map[string]any{"kind": "skills", "title": "Skills", "groups": []any{}},
		},
	})
	return b
}

// SettingsJSON returns a valid settings document (includes baseUrl, title and
// postsPerPage) that ingest.BuildSettings accepts.
func SettingsJSON() []byte {
	b, _ := json.Marshal(map[string]any{
		"title":          "Test Site",
		"description":    "A test site description.",
		"author":         "Test Person",
		"baseUrl":        "https://example.test",
		"defaultOgImage": "images/avatar.png",
		"postsPerPage":   10,
	})
	return b
}

// TinyPNG returns the bytes of a 1x1 transparent PNG, used as an image fixture.
func TinyPNG() []byte {
	const b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC"
	data, _ := base64.StdEncoding.DecodeString(b64)
	return data
}

// ContentDir is an in-memory description of a content repository tree. Write
// materializes it under t.TempDir() in the posts/, resume/, settings/, images/
// layout ingest expects. A nil Resume or Settings is omitted so failure cases
// (missing required file) can be constructed by clearing the field.
type ContentDir struct {
	Posts    map[string][]byte // filename (e.g. "hello-world.json") -> file bytes
	Resume   []byte            // resume/resume.json; omitted when nil
	Settings []byte            // settings/site.json; omitted when nil
	Images   map[string][]byte // filename (e.g. "avatar.png") -> file bytes
}

// DefaultContentDir returns a populated tree with six published posts (one of
// them the canonical SamplePostContent) and one draft that uniquely contains
// the word "quokka". Several published posts contain the word "lorem" so bm25
// search ordering can be exercised; one of them ("gamma-post") carries it in
// its title so it ranks best. Each call returns fresh maps so a caller may
// mutate the result (e.g. drop the resume) before Write.
func DefaultContentDir() ContentDir {
	posts := map[string][]byte{
		"hello-world.json": PostJSON(PostInput{
			Slug: "hello-world", Title: "Hello, World", Summary: "First published post.",
			Date: "2024-03-02T09:00:00Z", Updated: "2024-03-05T12:30:00Z",
			Tags: []string{"Go", "Benchmarks", "Notes"}, Published: true,
			Cover: "images/avatar.png", Content: SamplePostContent(),
		}),
		"alpha-post.json": PostJSON(PostInput{
			Slug: "alpha-post", Title: "Alpha", Summary: "Alpha summary.",
			Date: "2024-05-01T00:00:00Z", Tags: []string{"Writing"}, Published: true,
			Body: "lorem ipsum dolor sit amet alpha",
		}),
		"beta-post.json": PostJSON(PostInput{
			Slug: "beta-post", Title: "Beta", Summary: "Beta summary.",
			Date: "2024-05-02T00:00:00Z", Tags: []string{"Writing"}, Published: true,
			Body: "lorem ipsum beta",
		}),
		"gamma-post.json": PostJSON(PostInput{
			Slug: "gamma-post", Title: "Lorem Chronicles", Summary: "Gamma summary.",
			Date: "2024-05-03T00:00:00Z", Tags: []string{"Writing"}, Published: true,
			Body: "gamma body text",
		}),
		"delta-post.json": PostJSON(PostInput{
			Slug: "delta-post", Title: "Delta", Summary: "Delta summary.",
			Date: "2024-05-04T00:00:00Z", Tags: []string{"Writing"}, Published: true,
			Body: "lorem delta",
		}),
		"epsilon-post.json": PostJSON(PostInput{
			Slug: "epsilon-post", Title: "Epsilon", Summary: "Epsilon summary.",
			Date: "2024-05-05T00:00:00Z", Tags: []string{"Misc"}, Published: true,
			Body: "nothing special here epsilon",
		}),
		"draft-note.json": PostJSON(PostInput{
			Slug: "draft-note", Title: "Draft Note", Summary: "Unpublished draft.",
			Date: "2024-04-10T08:00:00Z", Tags: []string{"Drafts"}, Published: false,
			Body: "this draft mentions quokka which is unique",
		}),
	}
	return ContentDir{
		Posts:    posts,
		Resume:   ResumeJSON(),
		Settings: SettingsJSON(),
		Images:   map[string][]byte{"avatar.png": TinyPNG()},
	}
}

// Write materializes the tree under t.TempDir() and returns the directory path.
func (c ContentDir) Write(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel string, data []byte) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("testutil: mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatalf("testutil: write %s: %v", rel, err)
		}
	}
	for name, data := range c.Posts {
		write("posts/"+name, data)
	}
	if c.Resume != nil {
		write("resume/resume.json", c.Resume)
	}
	if c.Settings != nil {
		write("settings/site.json", c.Settings)
	}
	for name, data := range c.Images {
		write("images/"+name, data)
	}
	return dir
}

// WriteContentDir is a convenience wrapper that writes the default tree and
// returns its path. Tests needing a mutated tree build DefaultContentDir(),
// adjust it, and call Write themselves.
func WriteContentDir(t testing.TB) string {
	t.Helper()
	return DefaultContentDir().Write(t)
}
