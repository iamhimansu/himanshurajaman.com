package testutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
)

// GHRequest is one request the GitHubMock received.
type GHRequest struct {
	Method        string
	Path          string // decoded URL path
	RawQuery      string
	Authorization string
	Body          []byte
}

// GHResponse is a programmed response for a (method, path) route. A zero Status
// is served as 200.
type GHResponse struct {
	Status int
	Body   []byte
	Header map[string]string
}

// GitHubMock is a programmable stand-in for the GitHub REST API covering the
// calls this codebase makes: the Contents API (GET/PUT/DELETE), the commits
// endpoint (plain-text sha body) and the tarball endpoint. Responses are queued
// per (method, path); when a route has more than one queued response they are
// served in order and the final one repeats. Every request is recorded.
type GitHubMock struct {
	Server *httptest.Server
	Owner  string
	Repo   string

	mu       sync.Mutex
	queues   map[string][]GHResponse
	requests []GHRequest
}

// NewGitHubMock starts a mock server registered for cleanup. Owner/Repo default
// to "octocat"/"content"; set them (and the admin/source config) to match.
func NewGitHubMock(t testing.TB) *GitHubMock {
	t.Helper()
	m := &GitHubMock{Owner: "octocat", Repo: "content", queues: map[string][]GHResponse{}}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.Server.Close)
	return m
}

// APIBase returns the base URL to pass to github.WithAPIBase.
func (m *GitHubMock) APIBase() string { return m.Server.URL }

func (m *GitHubMock) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	m.mu.Lock()
	m.requests = append(m.requests, GHRequest{
		Method:        r.Method,
		Path:          r.URL.Path,
		RawQuery:      r.URL.RawQuery,
		Authorization: r.Header.Get("Authorization"),
		Body:          body,
	})
	q := m.queues[key]
	var resp GHResponse
	switch {
	case len(q) == 0:
		resp = GHResponse{Status: http.StatusInternalServerError, Body: []byte(`{"message":"no stub for ` + key + `"}`)}
	case len(q) == 1:
		resp = q[0] // keep the last response; it repeats
	default:
		resp = q[0]
		m.queues[key] = q[1:]
	}
	m.mu.Unlock()

	for k, v := range resp.Header {
		w.Header().Set(k, v)
	}
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(resp.Body)
}

func (m *GitHubMock) on(key string, resps ...GHResponse) {
	m.mu.Lock()
	m.queues[key] = append(m.queues[key], resps...)
	m.mu.Unlock()
}

func (m *GitHubMock) contentsKey(method, path string) string {
	return method + " /repos/" + m.Owner + "/" + m.Repo + "/contents/" + path
}

// OnGetContents queues a GET Contents response for a repo-relative path.
func (m *GitHubMock) OnGetContents(path string, resps ...GHResponse) {
	m.on(m.contentsKey("GET", path), resps...)
}

// OnPutContents queues PUT Contents responses for a repo-relative path.
func (m *GitHubMock) OnPutContents(path string, resps ...GHResponse) {
	m.on(m.contentsKey("PUT", path), resps...)
}

// OnDeleteContents queues DELETE Contents responses for a repo-relative path.
func (m *GitHubMock) OnDeleteContents(path string, resps ...GHResponse) {
	m.on(m.contentsKey("DELETE", path), resps...)
}

// OnCommitSHA stubs the commits endpoint for ref with a plain-text sha body.
func (m *GitHubMock) OnCommitSHA(ref, sha string) {
	m.on("GET /repos/"+m.Owner+"/"+m.Repo+"/commits/"+ref, GHResponse{Status: 200, Body: []byte(sha)})
}

// OnTarball stubs the tarball endpoint for ref with gzip bytes.
func (m *GitHubMock) OnTarball(ref string, gz []byte) {
	m.on("GET /repos/"+m.Owner+"/"+m.Repo+"/tarball/"+ref, GHResponse{Status: 200, Body: gz})
}

// Requests returns a copy of the recorded requests.
func (m *GitHubMock) Requests() []GHRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]GHRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

// RequestCount returns how many requests the mock has received.
func (m *GitHubMock) RequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// --- response-body builders ---

// GHContents builds a GET Contents 200 body ({sha, base64(content)}).
func GHContents(data []byte, sha string) GHResponse {
	body, _ := json.Marshal(map[string]string{
		"sha":     sha,
		"content": base64.StdEncoding.EncodeToString(data),
	})
	return GHResponse{Status: 200, Body: body}
}

// GHWrite builds a PUT/DELETE Contents success body ({content:{sha}, commit:{sha}}).
func GHWrite(status int, blobSHA, commitSHA string) GHResponse {
	body, _ := json.Marshal(map[string]any{
		"content": map[string]string{"sha": blobSHA},
		"commit":  map[string]string{"sha": commitSHA},
	})
	return GHResponse{Status: status, Body: body}
}

// GHStatus builds a bare response with just a status and a small JSON message.
func GHStatus(status int) GHResponse {
	return GHResponse{Status: status, Body: []byte(`{"message":"stub"}`)}
}

// MakeTarGz builds a gzipped tarball whose entries live under topDir (the
// "<owner>-<repo>-<sha>/" wrapper GitHub adds). Keys of files are repo-relative
// paths (e.g. "posts/hello.json"). A top-level directory entry is included.
func MakeTarGz(topDir string, files map[string][]byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	_ = tw.WriteHeader(&tar.Header{Name: topDir + "/", Mode: 0o755, Typeflag: tar.TypeDir})

	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		data := files[k]
		_ = tw.WriteHeader(&tar.Header{
			Name:     topDir + "/" + k,
			Mode:     0o644,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		})
		_, _ = tw.Write(data)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// --- failing HTTP client (transport error) ---

type errRoundTripper struct{}

func (errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("testutil: simulated transport failure")
}

// FailingHTTPClient returns an *http.Client whose every request fails at the
// transport layer, for proving GitHub-unavailable (transport error) paths.
func FailingHTTPClient() *http.Client {
	return &http.Client{Transport: errRoundTripper{}}
}
