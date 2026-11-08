package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func TestContext(t *testing.T) {
	_, ok := UserFrom(context.Background())
	require.False(t, ok)
	u, ok := UserFrom(WithUser(context.Background(), User{ID: "x"}))
	require.True(t, ok)
	require.Equal(t, "x", u.ID)
}

func TestOIDCUserID(t *testing.T) {
	a := OIDCUserID("https://idp", "sub1")
	require.NotEqual(t, DefaultUserID, a)
	require.LessOrEqual(t, len(a), 64)
	require.Equal(t, a, OIDCUserID("https://idp", "sub1"))
	require.NotEqual(t, a, OIDCUserID("https://idp", "sub2"))
	require.NotEqual(t, a, OIDCUserID("https://other", "sub1"))
	require.Equal(t, a, OIDCUserIDFromKey("https://idp|sub1"))
}

type env struct {
	clk *clock
	m   *Manager
	h   http.Handler // /api/auth/* handler
	api http.Handler // protected dummy API behind Middleware
	lim *recLimiter
}

type recLimiter struct {
	deny             bool
	fails, successes int
}

func (l *recLimiter) Check(string, string) (bool, time.Duration) { return !l.deny, time.Minute }
func (l *recLimiter) Failure(string, string)                     { l.fails++ }
func (l *recLimiter) Success(string, string)                     { l.successes++ }

func newEnv(t *testing.T, secure bool, secret string) *env {
	t.Helper()
	g := openDB(t)
	e := &env{clk: newClock(), lim: &recLimiter{}}
	e.m = NewManager(g, ManagerOptions{SecureCookie: secure, Now: e.clk.Now})
	e.h = NewHandler(HandlerOptions{DB: g, Manager: e.m, SharedSecret: secret, Limiter: e.lim, SecureCookie: secure, Now: e.clk.Now})
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		_, _ = w.Write([]byte(u.ID))
	})
	mux := http.NewServeMux()
	mux.Handle("/danger", RequireReauth(e.clk.Now)(ok))
	mux.Handle("/", ok)
	e.api = Middleware(e.m)(mux)
	return e
}

func (e *env) login(t *testing.T) (*http.Cookie, string) {
	t.Helper()
	rec := call(e.h, "POST", "/api/auth/login", `{"secret":"pw-pw-pw-pw-pw-pw"}`, nil)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	c := cookieNamed(rec, SessionCookieName)
	require.NotNil(t, c)
	rec = call(e.h, "GET", "/api/auth/me", "", nil, c)
	require.Equal(t, 200, rec.Code)
	var me map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &me))
	require.NotEmpty(t, me["csrf_token"])
	return c, me["csrf_token"]
}

func TestLoginCookieFlags(t *testing.T) {
	for _, secure := range []bool{true, false} {
		e := newEnv(t, secure, "pw-pw-pw-pw-pw-pw")
		rec := call(e.h, "POST", "/api/auth/login", `{"secret":"pw-pw-pw-pw-pw-pw"}`, nil)
		require.Equal(t, 200, rec.Code)
		c := cookieNamed(rec, SessionCookieName)
		require.NotNil(t, c)
		require.True(t, c.HttpOnly)
		require.Equal(t, http.SameSiteLaxMode, c.SameSite)
		require.Equal(t, "/", c.Path)
		require.Equal(t, secure, c.Secure)
		require.Len(t, c.Value, 43) // 256 random bits, base64url
		// only the hash is stored
		var rows []dbmodels.Session
		require.NoError(t, e.m.db.Find(&rows).Error)
		require.Len(t, rows, 1)
		require.Equal(t, hashID(c.Value), rows[0].ID)
		require.NotEqual(t, c.Value, rows[0].ID)
		require.Equal(t, DefaultUserID, rows[0].UserID)
	}
}

func TestLoginFailures(t *testing.T) {
	e := newEnv(t, false, "pw-pw-pw-pw-pw-pw")
	rec := call(e.h, "POST", "/api/auth/login", `{"secret":"nope"}`, nil)
	require.Equal(t, 401, rec.Code)
	require.Nil(t, cookieNamed(rec, SessionCookieName))
	require.Equal(t, 1, e.lim.fails)

	require.Equal(t, 400, call(e.h, "POST", "/api/auth/login", `not json`, nil).Code)

	e.lim.deny = true
	require.Equal(t, 429, call(e.h, "POST", "/api/auth/login", `{"secret":"pw-pw-pw-pw-pw-pw"}`, nil).Code)
	e.lim.deny = false

	// login CSRF: cross-site posts are rejected even with the right secret
	for _, hdr := range []map[string]string{
		{"Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "cross-site"},
	} {
		hdr["Host"] = "daka.test"
		rec = call(e.h, "POST", "/api/auth/login", `{"secret":"pw-pw-pw-pw-pw-pw"}`, hdr)
		require.Equal(t, 403, rec.Code)
	}
	ok := call(e.h, "POST", "/api/auth/login", `{"secret":"pw-pw-pw-pw-pw-pw"}`, map[string]string{"Origin": "http://example.com", "Sec-Fetch-Site": "same-origin"})
	require.Equal(t, 200, ok.Code) // httptest host is example.com
	require.Equal(t, 1, e.lim.successes)

	// not configured => no login endpoint
	e2 := newEnv(t, false, "")
	require.Equal(t, 404, call(e2.h, "POST", "/api/auth/login", `{"secret":""}`, nil).Code)
}

func TestSessionRequiredAndCSRF(t *testing.T) {
	e := newEnv(t, false, "pw-pw-pw-pw-pw-pw")
	require.Equal(t, 401, call(e.api, "GET", "/x", "", nil).Code)
	require.Equal(t, 401, call(e.api, "GET", "/x", "", nil, &http.Cookie{Name: SessionCookieName, Value: "pw-pw-pw-pw-pw-pw"}).Code)

	c, tok := e.login(t)
	rec := call(e.api, "GET", "/x", "", nil, c)
	require.Equal(t, 200, rec.Code)
	require.Equal(t, DefaultUserID, rec.Body.String())

	// safe methods need no token; state changes do
	for _, m := range []string{"POST", "PATCH", "PUT", "DELETE"} {
		require.Equal(t, 403, call(e.api, m, "/x", "", nil, c).Code, m)
		require.Equal(t, 403, call(e.api, m, "/x", "", map[string]string{CSRFHeader: "wrong"}, c).Code, m)
		require.Equal(t, 403, call(e.api, m, "/x", "", map[string]string{CSRFHeader: tok + "x"}, c).Code, m)
		require.Equal(t, 200, call(e.api, m, "/x", "", map[string]string{CSRFHeader: tok}, c).Code, m)
	}
	// the token is bound to the session
	c2, tok2 := e.login(t)
	require.NotEqual(t, tok, tok2)
	require.Equal(t, 403, call(e.api, "POST", "/x", "", map[string]string{CSRFHeader: tok}, c2).Code)
}

func TestMeAndStatus(t *testing.T) {
	e := newEnv(t, false, "pw-pw-pw-pw-pw-pw")
	rec := call(e.h, "GET", "/api/auth/status", "", nil)
	require.JSONEq(t, `{"oidc_enabled":false,"login_required":true,"authenticated":false}`, rec.Body.String())
	require.Equal(t, 401, call(e.h, "GET", "/api/auth/me", "", nil).Code)

	c, _ := e.login(t)
	rec = call(e.h, "GET", "/api/auth/status", "", nil, c)
	require.JSONEq(t, `{"oidc_enabled":false,"login_required":true,"authenticated":true}`, rec.Body.String())
	rec = call(e.h, "GET", "/api/auth/me", "", nil, c)
	var me map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &me))
	require.Equal(t, DefaultUserID, me["id"])
	require.Equal(t, "default_user", me["name"])
	require.NotContains(t, me, "email")
}

func TestSingleUserNoSecretModeHasNoCSRFToken(t *testing.T) {
	e := newEnv(t, false, "")
	rec := call(e.h, "GET", "/api/auth/status", "", nil)
	require.JSONEq(t, `{"oidc_enabled":false,"login_required":false,"authenticated":true}`, rec.Body.String())
	rec = call(e.h, "GET", "/api/auth/me", "", nil)
	require.Equal(t, 200, rec.Code)
	require.NotContains(t, rec.Body.String(), "csrf_token")
}

func TestLogout(t *testing.T) {
	e := newEnv(t, false, "pw-pw-pw-pw-pw-pw")
	c, tok := e.login(t)
	require.Equal(t, 403, call(e.h, "POST", "/api/auth/logout", "", nil, c).Code, "logout needs csrf")
	rec := call(e.h, "POST", "/api/auth/logout", "", map[string]string{CSRFHeader: tok}, c)
	require.Equal(t, 204, rec.Code)
	cleared := cookieNamed(rec, SessionCookieName)
	require.NotNil(t, cleared)
	require.Negative(t, cleared.MaxAge)
	require.Equal(t, 401, call(e.api, "GET", "/x", "", nil, c).Code, "session revoked server-side")
	// logout without a session is a harmless no-op that clears the cookie
	require.Equal(t, 204, call(e.h, "POST", "/api/auth/logout", "", nil).Code)
}

func TestSessionExpiryAndPurge(t *testing.T) {
	e := newEnv(t, false, "pw-pw-pw-pw-pw-pw")
	c, _ := e.login(t)
	e.clk.Advance(DefaultSessionTTL - time.Minute)
	require.Equal(t, 200, call(e.api, "GET", "/x", "", nil, c).Code)
	e.clk.Advance(2 * time.Minute)
	require.Equal(t, 401, call(e.api, "GET", "/x", "", nil, c).Code)
	n, err := e.m.Purge()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

func TestLoginRotatesSession(t *testing.T) {
	e := newEnv(t, false, "pw-pw-pw-pw-pw-pw")
	c1, _ := e.login(t)
	rec := call(e.h, "POST", "/api/auth/login", `{"secret":"pw-pw-pw-pw-pw-pw"}`, nil, c1)
	require.Equal(t, 200, rec.Code)
	require.Equal(t, 401, call(e.api, "GET", "/x", "", nil, c1).Code, "old session is revoked on login")
}

func TestReauthSecretAndRequireReauth(t *testing.T) {
	e := newEnv(t, false, "pw-pw-pw-pw-pw-pw")
	c, tok := e.login(t)
	hdr := map[string]string{CSRFHeader: tok}

	rec := call(e.api, "DELETE", "/danger", "", hdr, c)
	require.Equal(t, 403, rec.Code)
	require.JSONEq(t, `{"error":"reauth_required"}`, rec.Body.String())

	require.Equal(t, 403, call(e.h, "POST", "/api/auth/reauth", `{"secret":"pw-pw-pw-pw-pw-pw"}`, nil, c).Code, "csrf needed")
	require.Equal(t, 401, call(e.h, "POST", "/api/auth/reauth", `{"secret":"bad"}`, hdr, c).Code)
	require.Equal(t, 403, call(e.api, "DELETE", "/danger", "", hdr, c).Code)
	require.Equal(t, 401, call(e.h, "POST", "/api/auth/reauth", `{"secret":"pw-pw-pw-pw-pw-pw"}`, nil).Code, "needs session")
	require.Equal(t, 200, call(e.h, "POST", "/api/auth/reauth", `{"secret":"pw-pw-pw-pw-pw-pw"}`, hdr, c).Code)

	require.Equal(t, 200, call(e.api, "DELETE", "/danger", "", hdr, c).Code)
	e.clk.Advance(5*time.Minute - time.Second)
	require.Equal(t, 200, call(e.api, "DELETE", "/danger", "", hdr, c).Code)
	e.clk.Advance(2 * time.Second)
	rec = call(e.api, "DELETE", "/danger", "", hdr, c)
	require.Equal(t, 403, rec.Code)
	require.JSONEq(t, `{"error":"reauth_required"}`, rec.Body.String())
	// ordinary routes are unaffected
	require.Equal(t, 200, call(e.api, "DELETE", "/x", "", hdr, c).Code)
}

func TestRequireReauthNoSessionPasses(t *testing.T) {
	h := Middleware(nil)(RequireReauth(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})))
	require.Equal(t, 200, call(h, "DELETE", "/danger", "", nil).Code)
}

func TestSingleUserOriginGuard(t *testing.T) {
	h := Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		_, _ = w.Write([]byte(u.ID))
	}))
	// httptest requests have Host example.com
	tests := []struct {
		name string
		m    string
		hdr  map[string]string
		want int
	}{
		{"curl post", "POST", nil, 200},
		{"same origin post", "POST", map[string]string{"Origin": "http://example.com"}, 200},
		{"foreign origin post", "POST", map[string]string{"Origin": "http://evil.example"}, 403},
		{"foreign origin delete", "DELETE", map[string]string{"Origin": "http://evil.example"}, 403},
		{"null origin", "POST", map[string]string{"Origin": "null"}, 403},
		{"cross-site fetch metadata", "PATCH", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"same-site (sibling) fetch metadata", "PUT", map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		{"same-origin fetch metadata", "POST", map[string]string{"Sec-Fetch-Site": "same-origin"}, 200},
		{"foreign origin GET unaffected", "GET", map[string]string{"Origin": "http://evil.example"}, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := call(h, tt.m, "/x", "", tt.hdr)
			require.Equal(t, tt.want, rec.Code)
			if tt.want == 200 {
				require.Equal(t, DefaultUserID, rec.Body.String())
			}
		})
	}
}

func TestPlainVerifierStillSupported(t *testing.T) {
	h := Middleware(denyAll{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	require.Equal(t, 401, call(h, "GET", "/", "", nil).Code)
}

type denyAll struct{}

func (denyAll) Verify(*http.Request) (User, bool) { return User{}, false }
