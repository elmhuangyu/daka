package auth

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"reflect"
	"time"
)

// SessionCookieName is the name of the session cookie.
const SessionCookieName = "daka_session"

// CSRFHeader carries the session-bound CSRF token on state-changing requests.
const CSRFHeader = "X-Daka-CSRF"

// SessionVerifier decides whether a request carries a valid session and who it
// belongs to. Manager is the real (database-backed) implementation.
type SessionVerifier interface {
	Verify(r *http.Request) (User, bool)
}

// SessionSource is implemented by verifiers that expose the full session
// (Manager). Middleware then also enforces CSRF and exposes the session to
// handlers (see SessionFrom, RequireReauth).
type SessionSource interface {
	Session(r *http.Request) (Session, bool)
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// Middleware authenticates requests.
//
// With a nil verifier (single-user mode without shared_secret) every request
// gets DefaultUser. There is no cookie, hence nothing for classic CSRF to ride
// on, but a malicious web page could still make the user's browser POST to the
// loopback server, so state-changing requests that carry browser provenance
// headers must be same-origin (see SameOrigin). Plain clients (curl) send
// neither header and keep working.
//
// With a verifier, requests without a valid session get 401; if the verifier is
// a SessionSource, state-changing requests must also carry a matching
// X-Daka-CSRF header (403 otherwise).
func Middleware(v SessionVerifier) func(http.Handler) http.Handler {
	if v != nil && isNilVerifier(v) {
		// A typed nil (e.g. a nil *Manager) is a wiring bug; failing open to
		// default_user would silently disable authentication.
		panic("auth: Middleware called with a typed-nil SessionVerifier")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			switch src := v.(type) {
			case nil:
				if !isSafeMethod(r.Method) && !SameOrigin(r) {
					writeError(w, http.StatusForbidden, "cross_origin")
					return
				}
				ctx = WithUser(ctx, DefaultUser)
			case SessionSource:
				s, ok := src.Session(r)
				if !ok {
					writeError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				if !isSafeMethod(r.Method) && !CheckCSRF(r, s) {
					writeError(w, http.StatusForbidden, "csrf")
					return
				}
				ctx = WithSession(WithUser(ctx, User{ID: s.UserID}), s)
			default:
				u, ok := v.Verify(r)
				if !ok {
					writeError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				ctx = WithUser(ctx, u)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isNilVerifier reports a typed nil pointer wrapped in the interface.
func isNilVerifier(v SessionVerifier) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// CheckCSRF reports whether r carries the CSRF token bound to s (constant time).
func CheckCSRF(r *http.Request, s Session) bool {
	got := r.Header.Get(CSRFHeader)
	return got != "" && s.CSRFToken != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.CSRFToken)) == 1
}

// SameOrigin is the browser CSRF defence for cookie-less requests: if an Origin
// header is present its host must equal the request Host; otherwise, if
// Sec-Fetch-Site is present it must be same-origin or none. Requests with
// neither header (non-browser clients) pass. Behind a reverse proxy that
// rewrites Host, the proxy must preserve the original Host header.
func SameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && u.Host != "" && u.Host == r.Host
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	return false
}

// TODO(P1-9, P1-12): RequireReauth must be applied by the routes that create
// API tokens, delete items and delete the account; it is not wired anywhere yet.
//
// RequireReauth rejects requests whose session has no re-authentication within
// the last 5 minutes with 403 {"error":"reauth_required"}. Apply it (after
// Middleware) to dangerous routes: DELETE /api/account, DELETE /api/items/{id},
// API token creation. Without a cookie session (single-user mode without
// shared_secret) there is nothing to re-authenticate against and requests pass.
func RequireReauth(now func() time.Time) func(http.Handler) http.Handler {
	if now == nil {
		now = time.Now
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s, ok := SessionFrom(r.Context()); ok && !s.ReauthUntil.After(now()) {
				writeError(w, http.StatusForbidden, "reauth_required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
