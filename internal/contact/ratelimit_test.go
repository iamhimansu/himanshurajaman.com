package contact_test

import (
	"testing"
	"time"

	"github.com/iamhimansu/himanshurajaman.com/internal/contact"
)

func TestRateLimiter_WindowBlocksThenRecovers(t *testing.T) {
	rl := contact.NewRateLimiter(3, time.Minute, 100)
	base := time.Now()

	for i := 0; i < 3; i++ {
		if !rl.Allow("1.2.3.4", base) {
			t.Fatalf("request %d within the limit should be allowed", i+1)
		}
	}
	// The 4th within the window is blocked.
	if rl.Allow("1.2.3.4", base) {
		t.Fatal("4th request in the window should be blocked")
	}
	// A different key is independent.
	if !rl.Allow("5.6.7.8", base) {
		t.Fatal("a different key should have its own budget")
	}
	// After the window advances, the original key is allowed again.
	if !rl.Allow("1.2.3.4", base.Add(time.Minute+time.Second)) {
		t.Fatal("request after the window elapses should be allowed")
	}
}
