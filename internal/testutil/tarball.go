package testutil

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Synthetic hostnames for the cross-host redirect test. They are never
// resolved by DNS: TarballRedirect.Client's transport maps them to the two
// local test listeners. Distinct hostnames are required so Go's default
// redirect policy treats the hop as cross-host and strips Authorization — two
// 127.0.0.1 httptest servers share a hostname and would NOT trigger stripping.
const (
	apiHost      = "api.github.test"
	codeloadHost = "codeload.github.test"
)

// TarballRedirect emulates api.github.com issuing a 302 to codeload.github.com
// for a tarball download, across two synthetic hosts, so a test can assert that
// Go's standard redirect policy drops the Authorization header on the
// cross-host hop. Use APIBase with github.WithAPIBase and Client with
// github.WithHTTPClient.
type TarballRedirect struct {
	APIBase string       // e.g. http://api.github.test
	Client  *http.Client // transport maps the synthetic hosts to local listeners

	mu           sync.Mutex
	apiAuth      string
	apiSeen      bool
	codeloadAuth string
	codeloadSeen bool
}

// NewTarballRedirect starts the two servers and wires the host-mapping client.
// A GET to the tarball path on APIBase 302-redirects to the codeload host,
// which serves gz. Both servers are closed on test cleanup.
func NewTarballRedirect(t testing.TB, gz []byte) *TarballRedirect {
	t.Helper()
	tr := &TarballRedirect{APIBase: "http://" + apiHost}

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr.mu.Lock()
		tr.apiSeen = true
		tr.apiAuth = r.Header.Get("Authorization")
		tr.mu.Unlock()
		// Redirect to the codeload host; the Location carries its own token.
		http.Redirect(w, r, "http://"+codeloadHost+"/legacy.tar.gz?token=codeload-token", http.StatusFound)
	}))
	t.Cleanup(apiSrv.Close)

	codeloadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr.mu.Lock()
		tr.codeloadSeen = true
		tr.codeloadAuth = r.Header.Get("Authorization")
		tr.mu.Unlock()
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gz)
	}))
	t.Cleanup(codeloadSrv.Close)

	hostToAddr := map[string]string{
		apiHost + ":80":      apiSrv.Listener.Addr().String(),
		codeloadHost + ":80": codeloadSrv.Listener.Addr().String(),
	}
	dialer := &net.Dialer{}
	tr.Client = &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if mapped, ok := hostToAddr[addr]; ok {
					addr = mapped
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}
	return tr
}

// APIAuthorization returns the Authorization header the api host saw.
func (tr *TarballRedirect) APIAuthorization() string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.apiAuth
}

// CodeloadAuthorization returns the Authorization header the codeload host saw
// (want empty — proof the token was stripped on the cross-host redirect).
func (tr *TarballRedirect) CodeloadAuthorization() string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.codeloadAuth
}

// CodeloadReached reports whether the redirect was actually followed.
func (tr *TarballRedirect) CodeloadReached() bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.codeloadSeen
}
