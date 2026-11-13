package e2e

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/config"
)

const sharedSecret = "correct horse battery"

func withSecret(cfg *config.Config) { cfg.Auth.SharedSecret = sharedSecret }

// refreshCSRF reads the session-bound CSRF token from GET /api/auth/me.
func (c *client) refreshCSRF() {
	c.t.Helper()
	r := c.ok(200, "GET", "/api/auth/me", nil)
	c.csrf, _ = jsonOf[map[string]string](c.t, r)["csrf_token"]
	require.NotEmpty(c.t, c.csrf)
}

// login performs the shared-secret login and picks up the CSRF token.
func (c *client) login(secret string) {
	c.t.Helper()
	c.ok(200, "POST", "/api/auth/login", obj{"secret": secret})
	c.refreshCSRF()
}

func setCookieHeader(r resp, name string) string {
	for _, v := range r.Header.Values("Set-Cookie") {
		if strings.HasPrefix(v, name+"=") {
			return v
		}
	}
	return ""
}

// F5 / B7: single-user mode without login: no cookies, so state-changing
// requests with browser provenance must be same-origin, and the Host header is
// checked against DNS rebinding.
func TestSingleUserModeGuards(t *testing.T) {
	s := startStack(t, "2025-09-10", stackOpts{})
	c := s.newClient()
	st := jsonOf[map[string]bool](t, c.ok(200, "GET", "/api/auth/status", nil))
	require.False(t, st["login_required"])
	require.True(t, st["authenticated"])
	body := obj{"name": "x", "tracker_type": "boolean", "rule": fixedRule("FREQ=DAILY", "none")}

	// Plain clients (curl, scripts) work without CSRF headers.
	c.ok(201, "POST", "/api/items", body)
	// A foreign page in a browser is refused.
	for _, h := range []map[string]string{
		{"Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site"},
	} {
		r := c.doHdr("POST", "/api/items", body, h)
		require.Equal(t, 403, r.Status, "%v: %s", h, r.Body)
		require.Contains(t, r.Body, "cross_origin")
	}
	c.ok(201, "POST", "/api/items", body)
	require.Equal(t, 201, c.doHdr("POST", "/api/items", body, map[string]string{"Origin": s.srv.URL, "Sec-Fetch-Site": "same-origin"}).Status)

	// DNS rebinding: a name that resolves to loopback but is not ours is refused
	// for every method, even when the browser considers it same-origin.
	c.host = "rebind.attacker.example:" + strings.Split(s.srv.Listener.Addr().String(), ":")[1]
	for _, m := range []string{"GET", "POST", "DELETE"} {
		r := c.doHdr(m, "/api/items", nil, map[string]string{"Origin": "http://" + c.host})
		require.Equal(t, 403, r.Status, m)
		require.Contains(t, r.Body, "forbidden host")
	}
	require.Equal(t, 403, c.do("GET", "/api/auth/status", nil).Status)
	require.Equal(t, 403, c.do("GET", "/api/calendar/feed.ics?token=dk_x", nil).Status)
	c.host = "localhost"
	c.ok(200, "GET", "/api/items", nil)
}

// F5 / B7: shared-secret login, cookie flags, CSRF, logout.
func TestSharedSecretSessionFlow(t *testing.T) {
	s := startStack(t, "2025-09-10", stackOpts{mut: withSecret})
	c := s.newClient()
	body := obj{"name": "x", "tracker_type": "boolean", "rule": fixedRule("FREQ=DAILY", "none")}

	// Unauthenticated.
	require.Equal(t, 401, c.do("GET", "/api/items", nil).Status)
	require.Equal(t, 401, c.do("POST", "/api/items", body).Status)
	require.Equal(t, 401, c.do("GET", "/api/auth/me", nil).Status)
	st := jsonOf[map[string]bool](t, c.ok(200, "GET", "/api/auth/status", nil))
	require.True(t, st["login_required"])
	require.False(t, st["authenticated"])

	// Login failures and login CSRF.
	r := c.do("POST", "/api/auth/login", obj{"secret": "wrong"})
	require.Equal(t, 401, r.Status)
	require.Empty(t, setCookieHeader(r, "daka_session"))
	r = c.doHdr("POST", "/api/auth/login", obj{"secret": sharedSecret}, map[string]string{"Origin": "https://evil.example"})
	require.Equal(t, 403, r.Status, "cross-site login is refused")
	require.Empty(t, setCookieHeader(r, "daka_session"))

	// Successful login: cookie flags.
	r = c.ok(200, "POST", "/api/auth/login", obj{"secret": sharedSecret})
	ck := setCookieHeader(r, "daka_session")
	require.Contains(t, ck, "HttpOnly")
	require.Contains(t, ck, "SameSite=Lax")
	require.Contains(t, ck, "Path=/")
	require.NotContains(t, ck, "Secure", "secure_cookie=false in this harness")
	c.refreshCSRF()

	// CSRF: safe methods pass without the header; unsafe ones need it.
	c.ok(200, "GET", "/api/items", nil)
	noCSRF := c.csrf
	c.csrf = ""
	for _, m := range []string{"POST", "PATCH", "PUT", "DELETE"} {
		path := "/api/items"
		if m != "POST" {
			path = "/api/items/1"
		}
		r := c.do(m, path, obj{})
		require.Equal(t, 403, r.Status, m)
		require.Contains(t, r.Body, "csrf")
	}
	c.csrf = "not-the-token"
	require.Equal(t, 403, c.do("POST", "/api/items", body).Status)
	c.csrf = noCSRF
	id := c.createItem(body)

	// Dangerous operations need only the session: no re-authentication step.
	s.clk.Advance(6 * time.Minute)
	c.ok(201, "POST", "/api/tokens", obj{"name": "t"})
	c.ok(200, "DELETE", fmt.Sprintf("/api/items/%d", id), nil)
	require.Equal(t, 404, c.do("POST", "/api/auth/reauth", obj{"secret": sharedSecret}).Status, "the reauth endpoint is gone")

	// API tokens are not sessions: a token cannot be used on session routes.
	tok := c.token("read", "")
	anon := s.newClient()
	require.Equal(t, 401, anon.doHdr("POST", "/api/items", body, bearer(tok)).Status)

	// Logout invalidates the session server-side, not just the cookie.
	cookie := c.cookie("daka_session")
	require.NotNil(t, cookie)
	c.csrf = ""
	require.Equal(t, 403, c.do("POST", "/api/auth/logout", nil).Status, "logout needs the CSRF token")
	c.csrf = noCSRF
	r = c.ok(204, "POST", "/api/auth/logout", nil)
	require.Contains(t, setCookieHeader(r, "daka_session"), "Max-Age=0")
	stale := s.newClient()
	rr := stale.doHdr("GET", "/api/items", nil, map[string]string{"Cookie": "daka_session=" + cookie.Value})
	require.Equal(t, 401, rr.Status, "the old cookie is dead")
	rr = stale.doHdr("POST", "/api/items", body, map[string]string{"Cookie": "daka_session=" + cookie.Value, "X-Daka-CSRF": noCSRF})
	require.Equal(t, 401, rr.Status)

	// Sessions expire (14 days).
	c2 := s.newClient()
	c2.login(sharedSecret)
	c2.ok(200, "GET", "/api/items", nil)
	s.clk.Advance(15 * 24 * time.Hour)
	require.Equal(t, 401, c2.do("GET", "/api/items", nil).Status)
}

func TestSecureCookieFlag(t *testing.T) {
	s := startStack(t, "2025-09-10", stackOpts{mut: func(cfg *config.Config) {
		withSecret(cfg)
		cfg.Auth.SecureCookie = true
	}})
	r := s.newClient().ok(200, "POST", "/api/auth/login", obj{"secret": sharedSecret})
	ck := setCookieHeader(r, "daka_session")
	require.Contains(t, ck, "Secure")
	require.Contains(t, ck, "HttpOnly")
	require.Contains(t, ck, "SameSite=Lax")
}

// With login enabled the Host check applies once allowed_hosts is configured.
func TestHostGuardWithLogin(t *testing.T) {
	s := startStack(t, "2025-09-10", stackOpts{mut: func(cfg *config.Config) {
		withSecret(cfg)
		cfg.Server.AllowedHosts = []string{"daka.example.com"}
	}})
	c := s.newClient()
	c.host = "evil.example.com"
	r := c.do("GET", "/api/auth/status", nil)
	require.Equal(t, 403, r.Status)
	require.Contains(t, r.Body, "forbidden host")
	require.Equal(t, 403, c.do("POST", "/api/auth/login", obj{"secret": sharedSecret}).Status)
	c.host = "daka.example.com"
	c.ok(200, "GET", "/api/auth/status", nil)
	c.host = ""
	c.ok(200, "GET", "/api/auth/status", nil) // loopback literal
}

// F5 / B7: end-to-end OIDC login (authorization code + PKCE) against a fake IdP.
func TestOIDCFlow(t *testing.T) {
	clk := newClock(shanghaiNoon("2025-09-10"))
	idp := newFakeIdP(t, clk)
	s := startStack(t, "2025-09-10", stackOpts{
		clk: clk, oidcHTTP: idp.srv.Client(),
		mut: func(cfg *config.Config) {
			cfg.Auth.Issuer = idp.srv.URL
			cfg.Auth.ClientID = idpClientID
			cfg.Auth.ClientSecret = "client-secret"
		},
	})
	redirect := s.cfg.Auth.RedirectURL
	c := s.newClient()

	st := jsonOf[map[string]bool](t, c.ok(200, "GET", "/api/auth/status", nil))
	require.True(t, st["oidc_enabled"])
	require.False(t, st["authenticated"])
	require.Equal(t, 401, c.do("GET", "/api/items", nil).Status)
	// Shared-secret login is disabled in OIDC mode.
	require.Equal(t, 404, c.do("POST", "/api/auth/login", obj{"secret": "x"}).Status)

	// start returns the callback URL the IdP would send the browser to.
	start := func(c *client, target string) (callback string, q url.Values, r resp) {
		r = c.do("GET", target, nil)
		require.Equal(t, http.StatusFound, r.Status, r.Body)
		callback, q = idp.authorize(t, r.Header.Get("Location"), redirect)
		return callback, q, r
	}

	// Login: pending-login cookie is HttpOnly, Lax and scoped to /api/auth.
	cb, _, r := start(c, "/api/auth/oidc/login")
	pend := setCookieHeader(r, "daka_oidc")
	require.Contains(t, pend, "HttpOnly")
	require.Contains(t, pend, "SameSite=Lax")
	require.Contains(t, pend, "Path=/api/auth")
	require.Equal(t, "no-store", r.Header.Get("Cache-Control"))

	// State is checked: a callback with a different state is refused, and
	// consumes the pending login (single use).
	bad := c.do("GET", strings.Replace(cb, "state=", "state=x", 1), nil)
	require.Equal(t, 400, bad.Status)
	require.Contains(t, bad.Body, "login_failed")
	require.Equal(t, 400, c.do("GET", cb, nil).Status, "the pending login is gone")
	require.Equal(t, 401, c.do("GET", "/api/items", nil).Status)

	// Happy path.
	cb, _, _ = start(c, "/api/auth/oidc/login")
	r = c.do("GET", cb, nil)
	require.Equal(t, http.StatusFound, r.Status, r.Body)
	require.Equal(t, "/", r.Header.Get("Location"))
	ck := setCookieHeader(r, "daka_session")
	require.Contains(t, ck, "HttpOnly")
	require.Contains(t, ck, "SameSite=Lax", "Strict would drop the cookie on the IdP redirect")
	require.NotNil(t, c.cookie("daka_session"))
	require.Nil(t, c.cookie("daka_oidc"), "the pending cookie is cleared")
	// Replaying the same callback URL does nothing.
	require.Equal(t, 400, s.newClient().do("GET", cb, nil).Status)

	me := jsonOf[map[string]string](t, c.ok(200, "GET", "/api/auth/me", nil))
	require.True(t, strings.HasPrefix(me["id"], "oidc_"))
	require.Equal(t, "Alice", me["name"])
	c.csrf = me["csrf_token"]
	require.NotEmpty(t, c.csrf)
	body := obj{"name": "oidc item", "tracker_type": "boolean", "rule": fixedRule("FREQ=DAILY", "none")}
	c.csrf = ""
	require.Equal(t, 403, c.do("POST", "/api/items", body).Status, "no CSRF header, no write")
	c.csrf = me["csrf_token"]
	id := c.createItem(body)

	// The OIDC user is a separate account from default_user.
	require.Contains(t, c.ok(200, "GET", "/api/items", nil).Body, "oidc item")

	// Dangerous operations need only the session, even long after login.
	clk.Advance(10 * time.Minute)
	c.ok(200, "DELETE", fmt.Sprintf("/api/items/%d", id), nil)

	// A callback without a pending login is refused (and logged).
	require.Equal(t, 400, s.newClient().do("GET", "/api/auth/oidc/callback?code=x&state=y", nil).Status)

	// Logout.
	cookie := c.cookie("daka_session")
	logs := s.logs.String()
	require.Contains(t, logs, "oidc callback failed", "failed callbacks are logged (and the capture works)")
	for _, secret := range []string{cookie.Value, c.csrf, "client-secret", "at-secret"} {
		require.NotContains(t, logs, secret, "credentials never reach the logs")
	}
	c.ok(204, "POST", "/api/auth/logout", nil)
	require.Equal(t, 401, s.newClient().doHdr("GET", "/api/items", nil, map[string]string{"Cookie": "daka_session=" + cookie.Value}).Status)
}

func TestOIDCSecureCookieFlag(t *testing.T) {
	clk := newClock(shanghaiNoon("2025-09-10"))
	idp := newFakeIdP(t, clk)
	s := startStack(t, "2025-09-10", stackOpts{
		clk: clk, oidcHTTP: idp.srv.Client(),
		mut: func(cfg *config.Config) {
			cfg.Auth.Issuer = idp.srv.URL
			cfg.Auth.ClientID = idpClientID
			cfg.Auth.SecureCookie = true
		},
	})
	r := s.newClient().ok(302, "GET", "/api/auth/oidc/login", nil)
	require.Contains(t, setCookieHeader(r, "daka_oidc"), "Secure")
}
