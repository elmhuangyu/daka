package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }
func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestLimiterBurstAndRefill(t *testing.T) {
	c := newClock()
	l := NewLimiter(LimiterConfig{RPS: 2, Burst: 3, Now: c.Now})
	for i := 0; i < 3; i++ {
		ok, _ := l.Allow("a")
		require.True(t, ok)
	}
	ok, wait := l.Allow("a")
	require.False(t, ok)
	require.Equal(t, 500*time.Millisecond, wait)

	ok, _ = l.Allow("b") // other key unaffected
	require.True(t, ok)

	c.Advance(500 * time.Millisecond)
	ok, _ = l.Allow("a")
	require.True(t, ok)
	ok, _ = l.Allow("a")
	require.False(t, ok)

	c.Advance(time.Hour) // refill is capped at burst
	for i := 0; i < 3; i++ {
		ok, _ = l.Allow("a")
		require.True(t, ok)
	}
	ok, _ = l.Allow("a")
	require.False(t, ok)
}

func TestLimiterBoundedMemory(t *testing.T) {
	c := newClock()
	l := NewLimiter(LimiterConfig{RPS: 1, Burst: 2, MaxKeys: 50, Now: c.Now})
	for i := 0; i < 1000; i++ {
		l.Allow("k" + strconv.Itoa(i))
		require.LessOrEqual(t, l.Len(), 50)
		c.Advance(time.Millisecond)
	}
	// after everything refilled, a new key clears the full buckets
	c.Advance(time.Hour)
	l.Allow("fresh")
	require.Equal(t, 1, l.Len())
}

func TestMiddleware429(t *testing.T) {
	c := newClock()
	l := NewLimiter(LimiterConfig{RPS: 1, Burst: 1, Now: c.Now})
	h := Middleware(l, ClientIPResolver{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	do := func(remote string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/x", nil)
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	require.Equal(t, 204, do("203.0.113.1:1000").Code)
	w := do("203.0.113.1:1001") // same IP, different port
	require.Equal(t, 429, w.Code)
	require.JSONEq(t, `{"error":"rate limited"}`, w.Body.String())
	require.Equal(t, "1", w.Header().Get("Retry-After"))
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.Equal(t, 204, do("203.0.113.2:1000").Code)

	require.NotNil(t, Middleware(nil, ClientIPResolver{})(http.NotFoundHandler()))
}

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	tests := []struct {
		name    string
		trusted []netip.Prefix
		remote  string
		xff     []string
		want    string
	}{
		{"no trust ignores xff", nil, "203.0.113.9:5", []string{"1.2.3.4"}, "203.0.113.9"},
		{"untrusted peer ignores xff", trusted, "203.0.113.9:5", []string{"1.2.3.4"}, "203.0.113.9"},
		{"trusted peer uses xff", trusted, "10.0.0.1:5", []string{"1.2.3.4"}, "1.2.3.4"},
		{"spoofed left entries ignored", trusted, "10.0.0.1:5", []string{"9.9.9.9, 1.2.3.4"}, "1.2.3.4"},
		{"chain through trusted hops", trusted, "10.0.0.1:5", []string{"1.2.3.4, 10.0.0.7"}, "1.2.3.4"},
		{"multiple header lines", trusted, "10.0.0.1:5", []string{"9.9.9.9", "1.2.3.4"}, "1.2.3.4"},
		{"no xff", trusted, "10.0.0.1:5", nil, "10.0.0.1"},
		{"malformed xff", trusted, "10.0.0.1:5", []string{"garbage"}, "10.0.0.1"},
		{"all trusted", trusted, "10.0.0.1:5", []string{"10.0.0.2"}, "10.0.0.1"},
		{"ipv6 peer", trusted, "[::1]:5", []string{"2001:db8::1"}, "2001:db8::1"},
		{"mapped v4 peer", trusted, "[::ffff:10.0.0.1]:5", []string{"1.2.3.4"}, "1.2.3.4"},
		{"xff with v4 port", trusted, "10.0.0.1:5", []string{"1.2.3.4:5555"}, "1.2.3.4"},
		{"xff with v6 port", trusted, "10.0.0.1:5", []string{"[2001:db8::1]:443"}, "2001:db8::1"},
		{"xff bracketless v6", trusted, "10.0.0.1:5", []string{"2001:db8::1"}, "2001:db8::1"},
		{"no port", nil, "203.0.113.9", nil, "203.0.113.9"},
		{"unparseable", nil, "@", nil, "@"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			require.Equal(t, tt.want, ClientIPResolver{Trusted: tt.trusted}.ClientIP(r))
		})
	}
}

func newAuth(c *clock, max int) *AuthFailureLimiter {
	return NewAuthFailureLimiter(AuthFailureConfig{Now: c.Now, MaxEntries: max})
}

func fail(l *AuthFailureLimiter, ip, p string, n int) {
	for i := 0; i < n; i++ {
		l.Failure(ip, p)
	}
}

func TestAuthFailureLockoutAndBackoff(t *testing.T) {
	c := newClock()
	l := newAuth(c, 0)
	fail(l, "1.1.1.1", "abcd1234", 4)
	ok, _ := l.Check("1.1.1.1", "abcd1234")
	require.True(t, ok, "below threshold")

	fail(l, "1.1.1.1", "abcd1234", 1) // 5th failure -> 1 min
	ok, wait := l.Check("1.1.1.1", "abcd1234")
	require.False(t, ok)
	require.Equal(t, time.Minute, wait)

	// failures reported during lockout do not extend it
	fail(l, "1.1.1.1", "abcd1234", 3)
	_, wait = l.Check("1.1.1.1", "abcd1234")
	require.Equal(t, time.Minute, wait)

	// doubling: each failure after the lockout ends doubles the next lockout
	want := []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	prev := time.Minute
	for _, w := range want {
		c.Advance(prev)
		ok, _ = l.Check("1.1.1.1", "abcd1234")
		require.True(t, ok, "lockout over")
		l.Failure("1.1.1.1", "abcd1234")
		ok, got := l.Check("1.1.1.1", "abcd1234")
		require.False(t, ok)
		require.Equal(t, w, got)
		prev = w
	}
}

func TestAuthFailureLockoutBlocksCorrectToken(t *testing.T) {
	c := newClock()
	l := newAuth(c, 0)
	const ip, p = "1.1.1.1", "goodtokn"
	authenticate := func(token string) (status int) {
		if ok, _ := l.Check(ip, TokenPrefix(token)); !ok {
			return 429
		}
		if token == "goodtokn-correct" {
			l.Success(ip, TokenPrefix(token))
			return 200
		}
		l.Failure(ip, TokenPrefix(token))
		return 401
	}
	for i := 0; i < 5; i++ {
		require.Equal(t, 401, authenticate("goodtokn-wrong"))
	}
	require.Equal(t, 429, authenticate("goodtokn-correct"), "correct token refused during lockout")
	c.Advance(time.Minute)
	require.Equal(t, 200, authenticate("goodtokn-correct"))
	require.Equal(t, 0, l.Len(), "success resets")
	require.Equal(t, p, TokenPrefix("goodtokn-correct"))
}

func TestAuthFailureIsolation(t *testing.T) {
	c := newClock()
	l := newAuth(c, 0)
	fail(l, "1.1.1.1", "aaaaaaaa", 5)
	ok, _ := l.Check("1.1.1.1", "aaaaaaaa")
	require.False(t, ok)
	ok, _ = l.Check("2.2.2.2", "aaaaaaaa")
	require.True(t, ok, "other IP")
	ok, _ = l.Check("1.1.1.1", "bbbbbbbb")
	require.True(t, ok, "other prefix")
}

func TestAuthFailureSuccessResetsHistory(t *testing.T) {
	c := newClock()
	l := newAuth(c, 0)
	fail(l, "1.1.1.1", "aaaaaaaa", 4)
	l.Success("1.1.1.1", "aaaaaaaa")
	fail(l, "1.1.1.1", "aaaaaaaa", 4)
	ok, _ := l.Check("1.1.1.1", "aaaaaaaa")
	require.True(t, ok)
}

func TestAuthFailureStaleCleanupAndCap(t *testing.T) {
	c := newClock()
	l := newAuth(c, 20)
	for i := 0; i < 500; i++ {
		l.Failure("10.0.0."+strconv.Itoa(i%250), "p"+strconv.Itoa(i))
		require.LessOrEqual(t, l.Len(), 20)
	}
	// stale entries (idle > ResetAfter) are dropped on the next insert at cap
	c.Advance(2 * time.Hour)
	l.Failure("9.9.9.9", "x")
	require.Equal(t, 1, l.Len())

	// a stale entry also forgets its old failure count
	l2 := newAuth(c, 0)
	fail(l2, "1.1.1.1", "aaaaaaaa", 4)
	c.Advance(2 * time.Hour)
	l2.Failure("1.1.1.1", "aaaaaaaa")
	ok, _ := l2.Check("1.1.1.1", "aaaaaaaa")
	require.True(t, ok)
}

func TestAuthFailureCapPrefersEvictingUnlocked(t *testing.T) {
	c := newClock()
	l := newAuth(c, 3)
	fail(l, "1.1.1.1", "locked00", 5) // locked
	l.Failure("2.2.2.2", "a")
	l.Failure("3.3.3.3", "b")
	l.Failure("4.4.4.4", "c") // forces eviction of an unlocked entry
	require.Equal(t, 3, l.Len())
	ok, _ := l.Check("1.1.1.1", "locked00")
	require.False(t, ok, "locked entry survives eviction")
}

func TestAuthFailureNeverEvictsLocked(t *testing.T) {
	c := newClock()
	l := newAuth(c, 3)
	for i := 0; i < 3; i++ { // one IP varying the prefix fills the table with locked keys
		fail(l, "1.1.1.1", "p"+strconv.Itoa(i), 5)
	}
	l.Failure("2.2.2.2", "new") // fail open for the new key
	require.Equal(t, 3, l.Len())
	for i := 0; i < 3; i++ {
		ok, _ := l.Check("1.1.1.1", "p"+strconv.Itoa(i))
		require.False(t, ok)
	}
}

func TestEvictionIsBatched(t *testing.T) {
	c := newClock()
	l := newAuth(c, 1000)
	for i := 0; i < 1000; i++ {
		l.Failure("ip"+strconv.Itoa(i), "p")
		c.Advance(time.Millisecond)
	}
	require.Equal(t, 1000, l.Len())
	l.Failure("extra", "p") // evicts ~1% (10) oldest, then inserts
	require.Equal(t, 991, l.Len())
	ok, _ := l.Check("ip0", "p")
	require.True(t, ok)

	lim := NewLimiter(LimiterConfig{RPS: 0.001, Burst: 5, MaxKeys: 1000, Now: c.Now})
	for i := 0; i < 1000; i++ {
		lim.Allow("k" + strconv.Itoa(i))
		c.Advance(time.Millisecond)
	}
	lim.Allow("extra")
	require.Equal(t, 991, lim.Len())
}

func TestAuthFailureConcurrent(t *testing.T) {
	l := NewAuthFailureLimiter(AuthFailureConfig{MaxEntries: 50})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				ip := "10.0.0." + strconv.Itoa(i%100)
				l.Check(ip, "p")
				l.Failure(ip, "p")
				if i%7 == 0 {
					l.Success(ip, "p")
				}
			}
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, l.Len(), 50)
}
