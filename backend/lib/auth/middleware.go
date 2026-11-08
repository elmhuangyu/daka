package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

// SessionCookieName is the name of the session cookie.
const SessionCookieName = "daka_session"

// SessionVerifier decides whether a request carries a valid session and who it
// belongs to. P1-14 plugs the real (login / OIDC) implementation in here.
type SessionVerifier interface {
	Verify(r *http.Request) (User, bool)
}

// Middleware authenticates requests. With a nil verifier (single-user mode
// without shared_secret) every request gets DefaultUser. Otherwise requests
// without a valid session are rejected with 401.
func Middleware(v SessionVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := DefaultUser
			if v != nil {
				var ok bool
				if u, ok = v.Verify(r); !ok {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
		})
	}
}

// SharedSecretVerifier is the minimal single-user gate used until P1-14 adds
// login: the session cookie must hold a token derived from the shared secret
// (never the secret itself). It does not issue cookies.
type SharedSecretVerifier struct {
	token []byte
}

// NewSharedSecretVerifier builds a verifier for the given shared secret.
func NewSharedSecretVerifier(secret string) *SharedSecretVerifier {
	return &SharedSecretVerifier{token: []byte(SessionToken(secret))}
}

// SessionToken derives the expected cookie value from the shared secret.
func SessionToken(secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("daka-session-v1"))
	return hex.EncodeToString(m.Sum(nil))
}

// Verify implements SessionVerifier using a constant-time comparison.
func (s *SharedSecretVerifier) Verify(r *http.Request) (User, bool) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil || subtle.ConstantTimeCompare([]byte(c.Value), s.token) != 1 {
		return User{}, false
	}
	return DefaultUser, true
}
