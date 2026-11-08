// Package ratelimit provides the request-rate protections of DESIGN 8: a keyed
// token-bucket limiter with bounded memory, an exponential-backoff failure
// tracker for token authentication (AuthFailureLimiter), client-IP resolution
// that honors X-Forwarded-For only from configured trusted proxies, and an HTTP
// middleware.
//
// The token bucket is implemented here (about 40 lines) instead of using
// golang.org/x/time/rate: we need per-key state with a hard entry cap,
// lossless eviction of full buckets, an injectable clock and an exact
// Retry-After value, none of which x/time/rate offers, and it keeps the
// dependency set unchanged.
package ratelimit

import (
	"sort"
	"sync"
	"time"
)

// Defaults for NewLimiter when a field is zero.
const (
	DefaultMaxKeys = 10000
)

// LimiterConfig configures a Limiter.
type LimiterConfig struct {
	// RPS is the sustained rate in requests per second per key (> 0).
	RPS float64
	// Burst is the bucket capacity (>= 1).
	Burst int
	// MaxKeys caps the number of tracked keys (default DefaultMaxKeys).
	MaxKeys int
	// Now is the clock (default time.Now).
	Now func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter is a keyed token-bucket rate limiter, safe for concurrent use.
type Limiter struct {
	rps     float64
	burst   float64
	maxKeys int
	now     func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

// NewLimiter creates a Limiter. Invalid RPS / Burst fall back to 1.
func NewLimiter(c LimiterConfig) *Limiter {
	l := &Limiter{rps: c.RPS, burst: float64(c.Burst), maxKeys: c.MaxKeys, now: c.Now, buckets: map[string]*bucket{}}
	if !(l.rps > 0) {
		l.rps = 1
	}
	if l.burst < 1 {
		l.burst = 1
	}
	if l.maxKeys <= 0 {
		l.maxKeys = DefaultMaxKeys
	}
	if l.now == nil {
		l.now = time.Now
	}
	return l
}

// Allow consumes one token for key. When denied, retryAfter is how long until
// a token is available.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= l.maxKeys {
			l.makeRoom(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else {
		if el := now.Sub(b.last); el > 0 {
			b.tokens = min(l.burst, b.tokens+el.Seconds()*l.rps)
		}
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rps * float64(time.Second))
}

// Len returns the number of tracked keys.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// makeRoom frees slots: first it drops buckets that have fully refilled
// (equivalent to a fresh bucket, so dropping is lossless); if the table is still
// full it evicts a batch (about 1% of capacity) of the least recently used
// buckets, amortizing the O(n) scan over many inserts.
func (l *Limiter) makeRoom(now time.Time) {
	type cand struct {
		key  string
		last time.Time
	}
	var rest []cand
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.rps >= l.burst {
			delete(l.buckets, k)
			continue
		}
		rest = append(rest, cand{k, b.last})
	}
	if len(l.buckets) < l.maxKeys {
		return
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].last.Before(rest[j].last) })
	for _, c := range rest[:min(len(rest), evictBatch(l.maxKeys))] {
		delete(l.buckets, c.key)
	}
}
