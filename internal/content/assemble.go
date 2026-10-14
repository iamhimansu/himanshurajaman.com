package content

import (
	"bytes"
	"encoding/json"
	"strings"
)

// AssemblePostJSON emits the canonical bytes of a posts/<slug>.json file:
// pretty-printed `{meta, content}` with a trailing newline, such that
// ParsePost(path, AssemblePostJSON(meta, content)) round-trips stably (tag
// names preserved, dates requoted RFC3339, published:false emitted, slug stays
// the filename stem). HTML escaping is disabled so inline markup inside code or
// text stays readable in the stored file. An empty content is normalized to a
// valid empty Editor.js document.
func AssemblePostJSON(meta PostMeta, content json.RawMessage) ([]byte, error) {
	if trimmed := strings.TrimSpace(string(content)); trimmed == "" || trimmed == "null" {
		content = json.RawMessage(emptyDocumentJSON)
	}

	doc := struct {
		Meta    PostMeta        `json:"meta"`
		Content json.RawMessage `json:"content"`
	}{Meta: meta, Content: content}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	// enc.Encode already appends a trailing newline.
	return buf.Bytes(), nil
}

// AssemblePageJSON emits the canonical bytes of a pages/<slug>.json file,
// byte-parallel to AssemblePostJSON: pretty-printed `{meta, content}` with a
// trailing newline, HTML escaping disabled, and an empty content normalized to
// a valid empty Editor.js document. Because PageMeta uses omitempty on
// navLabel/navOrder/summary/updated those keys drop out when empty, while nav
// and published are always emitted so false survives, such that
// ParsePage(path, AssemblePageJSON(meta, content)) round-trips stably.
func AssemblePageJSON(meta PageMeta, content json.RawMessage) ([]byte, error) {
	if trimmed := strings.TrimSpace(string(content)); trimmed == "" || trimmed == "null" {
		content = json.RawMessage(emptyDocumentJSON)
	}

	doc := struct {
		Meta    PageMeta        `json:"meta"`
		Content json.RawMessage `json:"content"`
	}{Meta: meta, Content: content}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
