// Package testutil holds the shared, hermetic test harness used across the
// internal/* test suites: programmable stand-ins for the GitHub REST API, a
// Google-style OIDC issuer (discovery + JWKS + signed ID tokens), a minimal
// fake SMTP server, and the content fixtures (post/resume/settings JSON) tests
// write into a temp content directory.
//
// It depends on the standard library ONLY. It must never import any
// internal/* package, so it is safe to import from white-box (package foo) and
// black-box (package foo_test) tests alike with zero import-cycle risk. All
// fixtures are emitted as raw JSON bytes; callers that need parsed domain types
// run them through the real parsers (content.ParsePost, etc.) themselves.
package testutil
