package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/ratelimit"
)

// The limiter must run before auth so unauthenticated floods are limited.
func TestLimiterPrecedesAuth(t *testing.T) {
	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
	}
	h := NewRouter(Deps{
		Auth:    deny,
		Limiter: ratelimit.NewLimiter(ratelimit.LimiterConfig{RPS: 0.001, Burst: 1}),
	})
	do := func() int {
		r := httptest.NewRequest("GET", "/api/view/today", nil)
		r.RemoteAddr = "203.0.113.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	require.Equal(t, http.StatusUnauthorized, do())
	require.Equal(t, http.StatusTooManyRequests, do())
}
