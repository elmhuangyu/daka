package ratelimit

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// TokenPrefixLen is how many leading characters of a presented token identify
// it for failure tracking.
const TokenPrefixLen = 8

// TokenPrefix returns the leading characters of a presented token, used as the
// second half of the failure-tracking key.
func TokenPrefix(token string) string {
	if len(token) > TokenPrefixLen {
		return token[:TokenPrefixLen]
	}
	return token
}

// AuthFailureConfig configures an AuthFailureLimiter.
type AuthFailureConfig struct {
	// Threshold is the number of failures that triggers the first lockout (default 5).
	Threshold int
	// BaseLockout is the first lockout duration (default 1 minute); each
	// further failure after the threshold doubles it.
	BaseLockout time.Duration
	// MaxLockout caps the lockout (default 1 hour).
	MaxLockout time.Duration
	// ResetAfter is how long after the last failure / end of a lockout an
	// entry is forgotten (default MaxLockout).
	ResetAfter time.Duration
	// MaxEntries caps tracked (ip, prefix) pairs (default DefaultMaxKeys).
	MaxEntries int
	// Now is the clock (default time.Now).
	Now func() time.Time
}

type failEntry struct {
	failures    int
	lockedUntil time.Time
	last        time.Time // last failure
}

// AuthFailureLimiter tracks token-auth failures per (client IP, token prefix)
// with exponential backoff and lockout. Usage in an auth path:
//
//	if ok, wait := l.Check(ip, prefix); !ok { reject 429, Retry-After: wait }
//	if verify(token) { l.Success(ip, prefix) } else { l.Failure(ip, prefix) }
//
// Check comes first and a locked-out key is rejected without verifying the
// token, so even a correct token is refused during the lockout. Safe for
// concurrent use. Keys are hashed, so no token material is retained.
type AuthFailureLimiter struct {
	cfg AuthFailureConfig
	now func() time.Time

	mu      sync.Mutex
	entries map[string]*failEntry
}

// NewAuthFailureLimiter creates a limiter, applying defaults for zero fields.
func NewAuthFailureLimiter(c AuthFailureConfig) *AuthFailureLimiter {
	if c.Threshold <= 0 {
		c.Threshold = 5
	}
	if c.BaseLockout <= 0 {
		c.BaseLockout = time.Minute
	}
	if c.MaxLockout <= 0 {
		c.MaxLockout = time.Hour
	}
	if c.MaxLockout < c.BaseLockout {
		c.MaxLockout = c.BaseLockout
	}
	if c.ResetAfter <= 0 {
		c.ResetAfter = c.MaxLockout
	}
	if c.MaxEntries <= 0 {
		c.MaxEntries = DefaultMaxKeys
	}
	now := c.Now
	if now == nil {
		now = time.Now
	}
	return &AuthFailureLimiter{cfg: c, now: now, entries: map[string]*failEntry{}}
}

func failKey(ip, prefix string) string {
	h := sha256.Sum256([]byte(ip + "\x00" + prefix))
	return hex.EncodeToString(h[:16])
}

// Check reports whether an authentication attempt may proceed; when locked out,
// retryAfter is the remaining lockout.
func (l *AuthFailureLimiter) Check(ip, prefix string) (allowed bool, retryAfter time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[failKey(ip, prefix)]
	if e == nil {
		return true, 0
	}
	if now.Before(e.lockedUntil) {
		return false, e.lockedUntil.Sub(now)
	}
	return true, 0
}

// Failure records a failed attempt. Failures reported while the key is locked
// out are ignored (the lockout is not extended by rejected attempts).
func (l *AuthFailureLimiter) Failure(ip, prefix string) {
	now := l.now()
	k := failKey(ip, prefix)
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[k]
	if e != nil && now.Sub(e.expiry()) >= l.cfg.ResetAfter {
		e = nil // stale: start over
	}
	if e == nil {
		if len(l.entries) >= l.cfg.MaxEntries && !l.makeRoom(now) {
			return // table full of locked entries: do not track this key
		}
		e = &failEntry{}
		l.entries[k] = e
	}
	if now.Before(e.lockedUntil) {
		return
	}
	e.failures++
	e.last = now
	if e.failures >= l.cfg.Threshold {
		e.lockedUntil = now.Add(l.lockout(e.failures - l.cfg.Threshold))
	}
}

// Success clears the failure history of the key.
func (l *AuthFailureLimiter) Success(ip, prefix string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, failKey(ip, prefix))
}

// Len returns the number of tracked entries.
func (l *AuthFailureLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// lockout is BaseLockout << level, capped at MaxLockout.
func (l *AuthFailureLimiter) lockout(level int) time.Duration {
	d := l.cfg.BaseLockout
	for i := 0; i < level && d < l.cfg.MaxLockout; i++ {
		d *= 2
	}
	return min(d, l.cfg.MaxLockout)
}

// expiry is the moment the entry's last activity ended (failure or lockout end).
func (e *failEntry) expiry() time.Time {
	if e.lockedUntil.After(e.last) {
		return e.lockedUntil
	}
	return e.last
}

// evictBatch returns how many entries are evicted at once when the table is
// full (about 1% of capacity), which amortizes the O(n) scan over many inserts.
func evictBatch(capacity int) int { return max(1, capacity/100) }

// makeRoom drops stale entries; if the table is still full it evicts a batch
// of the least recently active entries that are NOT locked out. It reports
// whether a slot is free. When only locked entries remain it frees nothing and
// returns false: the caller then does not track the new key (fail open for that
// key) instead of unlocking an attacker that is currently locked out. Note one
// client IP can create many keys by varying the token prefix, so filling the
// table does not require many IPs; locked entries are therefore never evicted.
func (l *AuthFailureLimiter) makeRoom(now time.Time) bool {
	type cand struct {
		key string
		at  time.Time
	}
	var unlocked []cand
	for k, e := range l.entries {
		switch {
		case now.Sub(e.expiry()) >= l.cfg.ResetAfter:
			delete(l.entries, k)
		case !now.Before(e.lockedUntil):
			unlocked = append(unlocked, cand{k, e.expiry()})
		}
	}
	if len(l.entries) < l.cfg.MaxEntries {
		return true
	}
	if len(unlocked) == 0 {
		return false
	}
	sort.Slice(unlocked, func(i, j int) bool { return unlocked[i].at.Before(unlocked[j].at) })
	for _, c := range unlocked[:min(len(unlocked), evictBatch(l.cfg.MaxEntries))] {
		delete(l.entries, c.key)
	}
	return true
}
