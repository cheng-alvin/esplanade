// Package ratelimit provides a small in-memory limiter for abuse-sensitive
// endpoints. It is single-instance by design; a shared store would be
// required once the server runs more than one replica.
package ratelimit

import (
	"sync"
	"time"
)

const (
	defaultMax    = 3
	defaultWindow = time.Hour

	// sweepThreshold bounds the key map: once it grows past this, keys whose
	// attempts have all expired are dropped on the next Allow call.
	sweepThreshold = 4096
)

// Limiter caps attempts per key within a sliding window.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

// New returns a limiter allowing max attempts per window per key. Non-positive
// values fall back to conservative defaults rather than silently disabling
// the limit.
func New(max int, window time.Duration) *Limiter {
	if max <= 0 {
		max = defaultMax
	}
	if window <= 0 {
		window = defaultWindow
	}
	return &Limiter{max: max, window: window, hits: make(map[string][]time.Time)}
}

// Allow records an attempt for key and reports whether it is within the
// window's cap.
func (l *Limiter) Allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.hits) > sweepThreshold {
		l.sweep(now)
	}

	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, at := range l.hits[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}

	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}

	l.hits[key] = append(kept, now)
	return true
}

// sweep drops keys with no live attempts. Callers must hold mu.
func (l *Limiter) sweep(now time.Time) {
	cutoff := now.Add(-l.window)
	for key, times := range l.hits {
		live := false
		for _, at := range times {
			if at.After(cutoff) {
				live = true
				break
			}
		}
		if !live {
			delete(l.hits, key)
		}
	}
}
