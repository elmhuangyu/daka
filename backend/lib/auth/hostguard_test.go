package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestHostGuard(t *testing.T) {
	h := HostGuard(NewHostAllowlist("127.0.0.1:8080", []string{"daka.example.com", "[::1]:9"}))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	tests := []struct {
		host, origin, method string
		want                 int
	}{
		{"localhost:8080", "", "GET", 200},
		{"LOCALHOST.:8080", "", "GET", 200},
		{"127.0.0.1:8080", "", "POST", 200},
		{"127.0.0.1", "", "GET", 200},
		{"[::1]:8080", "", "GET", 200},
		{"127.5.5.5:1", "", "GET", 200},
		{"daka.example.com", "", "GET", 200}, // allowed_hosts
		{"daka.example.com:443", "", "GET", 200},
		{"evil.test", "", "GET", 403},
		{"evil.test:8080", "http://evil.test:8080", "GET", 403}, // matching Origin does not help
		{"evil.test:8080", "http://evil.test:8080", "POST", 403},
		{"daka.example.com.evil.test", "", "GET", 403},
		{"", "", "GET", 403},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, "/api/x", nil)
		req.Host = tt.host
		if tt.origin != "" {
			req.Header.Set("Origin", tt.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, tt.want, rec.Code, "%s %s", tt.method, tt.host)
		if tt.want == 403 {
			require.JSONEq(t, `{"error":"forbidden host"}`, rec.Body.String())
		}
	}
	// a wildcard listen address adds nothing beyond loopback
	require.False(t, NewHostAllowlist("0.0.0.0:8080", nil).Allowed("0.0.0.0:8080"))
	require.True(t, NewHostAllowlist("daka.lan:8080", nil).Allowed("daka.lan"))
}

func TestMiddlewareTypedNilPanics(t *testing.T) {
	var m *Manager
	require.Panics(t, func() { Middleware(m) })
}

func TestTruncRuneSafe(t *testing.T) {
	require.Equal(t, "ab", trunc("ab", 5))
	require.Equal(t, "a", trunc("aé", 2)) // é is 2 bytes; cutting at 2 would split it
	require.True(t, utf8.ValidString(trunc("日本語", 4)))
}

func TestSecretEndpointsNotFoundWithoutSharedSecret(t *testing.T) {
	h := &handler{HandlerOptions{Limiter: NopLimiter{}}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"secret":""}`))
	require.False(t, h.readSecret(rr, req))
	require.Equal(t, 404, rr.Code)
}

func TestLimiterLockoutReturns429(t *testing.T) {
	e := newEnv(t, false, "pw-pw-pw-pw-pw-pw")
	e.lim.deny = true
	rec := call(e.h, "POST", "/api/auth/login", `{"secret":"pw-pw-pw-pw-pw-pw"}`, nil)
	require.Equal(t, 429, rec.Code)
	require.Equal(t, "61", rec.Header().Get("Retry-After"))
	_ = time.Second
}

func TestPendingStoreEvictsOldestWhenFull(t *testing.T) {
	clk := newClock()
	s := newPendingStore(clk.Now)
	first, err := s.put(pendingLogin{state: "first"})
	require.NoError(t, err)
	for i := 1; i < maxPending; i++ {
		clk.Advance(time.Millisecond)
		_, err = s.put(pendingLogin{state: "x"})
		require.NoError(t, err)
	}
	clk.Advance(time.Millisecond)
	last, err := s.put(pendingLogin{state: "last"})
	require.NoError(t, err, "a full store must evict, not refuse")
	_, ok := s.take(first)
	require.False(t, ok, "oldest entry was evicted")
	_, ok = s.take(last)
	require.True(t, ok)
}
