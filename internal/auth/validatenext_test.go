package auth_test

import (
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/auth"
)

func TestValidateNext(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty falls back to default", "", "/admin"},
		{"simple local path", "/blog/post", "/blog/post"},
		{"scheme-relative rejected", "//evil.com", "/admin"},
		{"backslash host rejected", `/\evil.com`, "/admin"},
		{"absolute http rejected", "http://evil.com", "/admin"},
		{"absolute https rejected", "https://evil.com", "/admin"},
		{"auth area rejected", "/auth", "/admin"},
		{"auth subpath rejected", "/auth/callback", "/admin"},
		{"bare relative (no slash) rejected", "foo", "/admin"},
		{"single-slash path allowed", "/resume", "/resume"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := auth.ValidateNext(tt.in); got != tt.want {
				t.Errorf("ValidateNext(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
