package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContext(t *testing.T) {
	_, ok := UserFrom(context.Background())
	require.False(t, ok)
	u, ok := UserFrom(WithUser(context.Background(), User{ID: "x"}))
	require.True(t, ok)
	require.Equal(t, "x", u.ID)
}

func TestMiddleware(t *testing.T) {
	good := &http.Cookie{Name: SessionCookieName, Value: SessionToken("pw")}
	tests := []struct {
		name     string
		verifier SessionVerifier
		cookie   *http.Cookie
		want     int
	}{
		{"no secret injects default user", nil, nil, http.StatusOK},
		{"secret, no cookie", NewSharedSecretVerifier("pw"), nil, http.StatusUnauthorized},
		{"secret, wrong cookie", NewSharedSecretVerifier("pw"), &http.Cookie{Name: SessionCookieName, Value: "nope"}, http.StatusUnauthorized},
		{"secret, raw secret as cookie rejected", NewSharedSecretVerifier("pw"), &http.Cookie{Name: SessionCookieName, Value: "pw"}, http.StatusUnauthorized},
		{"secret, valid cookie", NewSharedSecretVerifier("pw"), good, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got User
			h := Middleware(tt.verifier)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got, _ = UserFrom(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			require.Equal(t, tt.want, rec.Code)
			if tt.want == http.StatusOK {
				require.Equal(t, DefaultUserID, got.ID)
			} else {
				require.Empty(t, got.ID)
			}
		})
	}
}
