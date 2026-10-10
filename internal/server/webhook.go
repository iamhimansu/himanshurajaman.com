package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// maxWebhookBody caps the raw request body read for signature verification. A
// GitHub push payload is well under this; the cap bounds memory on abusive
// requests.
const maxWebhookBody = 5 << 20 // 5 MiB

// handleGitHubWebhook verifies a GitHub webhook delivery and, on a valid push,
// triggers an asynchronous single-flight resync. The endpoint is disabled
// (404) when no secret is configured. The webhook secret and signature are
// never logged.
func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhookSecret == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if isTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		writeError(w, http.StatusBadRequest, "could not read body")
		return
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" || !validSignature(s.webhookSecret, body, sig) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	switch r.Header.Get("X-GitHub-Event") {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "push":
		go s.asyncResync()
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "ignored"})
	}
}

// validSignature constant-time compares the GitHub X-Hub-Signature-256 header
// (HMAC-SHA256 of the raw body keyed by the secret) with the expected value.
// Neither the secret nor the signature is logged.
func validSignature(secret string, body []byte, header string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(header))
}

// asyncResync runs a resync in the background under a single-flight guard: a
// delivery arriving while one is in progress is skipped. It uses a detached
// context with a timeout so it survives the request that triggered it.
func (s *Server) asyncResync() {
	if !atomic.CompareAndSwapInt32(&s.resyncing, 0, 1) {
		s.log.Info("webhook resync skipped; already in progress")
		return
	}
	defer atomic.StoreInt32(&s.resyncing, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := s.runResync(ctx); err != nil {
		s.log.Error("webhook resync failed", "error", err.Error())
		return
	}
	s.log.Info("webhook resync complete")
}

// runResync reuses the admin service's resync when admin is enabled (it runs
// ingest under the write lock and flips readiness), otherwise it runs the
// ingester directly and marks readiness.
func (s *Server) runResync(ctx context.Context) error {
	if s.admin != nil {
		_, err := s.admin.Resync(ctx)
		return err
	}
	if s.ingester == nil {
		return errors.New("resync unavailable: no ingester")
	}
	res, err := s.ingester.Run(ctx)
	if err != nil {
		return err
	}
	s.MarkReady(res.CommitSHA)
	return nil
}
