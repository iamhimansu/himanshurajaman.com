package contact

import (
	"sync"
	"time"
)

// RateLimiter is a per-key sliding-window limiter. It is safe for concurrent use
// and bounded in memory: fully-expired keys are evicted lazily, and a hard cap
// on the number of tracked keys triggers a sweep so a flood of distinct IPs
// cannot grow the map without bound. A single process instance is assumed, so
// in-memory state needs no coordination.
type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	maxKeys int
	hits    map[string][]time.Time
}

// NewRateLimiter allows up to limit events per window per key, tracking at most
// ~maxKeys keys before sweeping expired ones.
func NewRateLimiter(limit int, window time.Duration, maxKeys int) *RateLimiter {
	return &RateLimiter{
		limit:   limit,
		window:  window,
		maxKeys: maxKeys,
		hits:    make(map[string][]time.Time),
	}
}

// Allow reports whether an event for key is permitted at time now. When
// permitted it records the event against the window; when denied it leaves the
// window unchanged (so the caller is not penalized for being rate limited).
func (r *RateLimiter) Allow(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := now.Add(-r.window)
	kept := prune(r.hits[key], cutoff)

	if len(kept) >= r.limit {
		r.hits[key] = kept
		return false
	}

	kept = append(kept, now)
	r.hits[key] = kept

	if len(r.hits) > r.maxKeys {
		r.sweep(cutoff)
	}
	return true
}

// sweep drops keys whose events have all expired. Called only when the map
// exceeds maxKeys, so the amortized cost stays low.
func (r *RateLimiter) sweep(cutoff time.Time) {
	for k, v := range r.hits {
		if len(prune(v, cutoff)) == 0 {
			delete(r.hits, k)
		}
	}
}

// prune returns ts with timestamps at or before cutoff removed, reusing the
// backing array to avoid allocation.
func prune(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for _, t := range ts {
		if t.After(cutoff) {
			ts[i] = t
			i++
		}
	}
	return ts[:i]
}
