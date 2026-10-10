package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Sentinels surfaced to the admin service so it can map GitHub write outcomes
// to HTTP statuses. ErrNotFound: the path does not exist (404). ErrConflict:
// an optimistic-concurrency failure — a stale sha on update/delete (409) or a
// create collision where the file already exists (422).
var (
	ErrNotFound = errors.New("github: contents not found")
	ErrConflict = errors.New("github: contents conflict")
)

// getContentsResponse mirrors the GET Contents JSON (file metadata at the top
// level).
type getContentsResponse struct {
	SHA     string `json:"sha"`
	Content string `json:"content"` // base64, newline-wrapped
}

// writeContentsResponse mirrors the PUT/DELETE Contents JSON.
type writeContentsResponse struct {
	Content struct {
		SHA string `json:"sha"`
	} `json:"content"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// GetContents fetches a single file. It returns the raw decoded bytes and the
// file's blob sha. A 404 maps to ErrNotFound. (Targets are small .json
// files, well under the Contents-API 1 MiB GET cap.)
func (c *Client) GetContents(ctx context.Context, owner, repo, path, ref string) (data []byte, sha string, err error) {
	u := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.apiBase, owner, repo, escapePath(path))
	if ref != "" {
		u += "?ref=" + url.QueryEscape(ref)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	c.setCommonHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))

	switch resp.StatusCode {
	case http.StatusOK:
		var cr getContentsResponse
		if err := json.Unmarshal(body, &cr); err != nil {
			return nil, "", fmt.Errorf("get contents %q: decode: %w", path, err)
		}
		decoded, err := decodeBase64Content(cr.Content)
		if err != nil {
			return nil, "", fmt.Errorf("get contents %q: %w", path, err)
		}
		return decoded, cr.SHA, nil
	case http.StatusNotFound:
		return nil, "", ErrNotFound
	default:
		return nil, "", fmt.Errorf("get contents %q: github returned %s: %s", path, resp.Status, snippet(body))
	}
}

// putRequest is the JSON body of a Contents PUT.
type putRequest struct {
	Message string `json:"message"`
	Content string `json:"content"`
	Branch  string `json:"branch,omitempty"`
	SHA     string `json:"sha,omitempty"`
}

// deleteRequest is the JSON body of a Contents DELETE.
type deleteRequest struct {
	Message string `json:"message"`
	SHA     string `json:"sha"`
	Branch  string `json:"branch,omitempty"`
}

// PutContents creates (sha == "") or updates (sha set) a file on the given
// branch. It returns the new blob sha and the commit sha. 409/422 map to
// ErrConflict (stale sha, or a create collision when the file already exists);
// 404 maps to ErrNotFound.
func (c *Client) PutContents(ctx context.Context, owner, repo, path, branch, message string, data []byte, sha string) (newBlobSHA, commitSHA string, err error) {
	payload, err := json.Marshal(putRequest{
		Message: message,
		Content: base64.StdEncoding.EncodeToString(data),
		Branch:  branch,
		SHA:     sha,
	})
	if err != nil {
		return "", "", err
	}
	u := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.apiBase, owner, repo, escapePath(path))
	resp, body, err := c.doWrite(ctx, http.MethodPut, u, payload)
	if err != nil {
		return "", "", err
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var wr writeContentsResponse
		if err := json.Unmarshal(body, &wr); err != nil {
			return "", "", fmt.Errorf("put contents %q: decode: %w", path, err)
		}
		return wr.Content.SHA, wr.Commit.SHA, nil
	case http.StatusConflict, http.StatusUnprocessableEntity:
		return "", "", ErrConflict
	case http.StatusNotFound:
		return "", "", ErrNotFound
	default:
		return "", "", fmt.Errorf("put contents %q: github returned %s: %s", path, resp.Status, snippet(body))
	}
}

// DeleteContents removes a file at the given sha on the given branch. 409/422
// → ErrConflict; 404 → ErrNotFound.
func (c *Client) DeleteContents(ctx context.Context, owner, repo, path, branch, message, sha string) error {
	payload, err := json.Marshal(deleteRequest{Message: message, SHA: sha, Branch: branch})
	if err != nil {
		return err
	}
	u := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.apiBase, owner, repo, escapePath(path))
	resp, body, err := c.doWrite(ctx, http.MethodDelete, u, payload)
	if err != nil {
		return err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusConflict, http.StatusUnprocessableEntity:
		return ErrConflict
	case http.StatusNotFound:
		return ErrNotFound
	default:
		return fmt.Errorf("delete contents %q: github returned %s: %s", path, resp.Status, snippet(body))
	}
}

// doWrite issues a JSON write request with the common headers and returns the
// response plus its (bounded) body. The caller inspects the status code. The
// token is never logged.
func (c *Client) doWrite(ctx context.Context, method, u string, payload []byte) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	c.setCommonHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))
	return resp, body, nil
}

// escapePath url.PathEscapes each path segment, preserving the slashes.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}

// decodeBase64Content decodes the Contents API's base64 field, which GitHub
// wraps with newlines.
func decodeBase64Content(s string) ([]byte, error) {
	clean := strings.NewReplacer("\n", "", "\r", "").Replace(s)
	return base64.StdEncoding.DecodeString(clean)
}

func snippet(b []byte) string {
	return strings.TrimSpace(string(bytes.TrimSpace(b)))
}
