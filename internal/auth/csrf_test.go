package auth_test

import (
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/auth"
)

func TestCSRFValid(t *testing.T) {
	tests := []struct {
		name           string
		cookie, header string
		want           bool
	}{
		{"both empty", "", "", false},
		{"empty cookie", "", "token", false},
		{"empty header", "token", "", false},
		{"mismatch", "token-a", "token-b", false},
		{"equal non-empty", "same-token", "same-token", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := auth.CSRFValid(tt.cookie, tt.header); got != tt.want {
				t.Errorf("CSRFValid(%q, %q) = %v, want %v", tt.cookie, tt.header, got, tt.want)
			}
		})
	}
}
