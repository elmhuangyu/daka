package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

const (
	testClientID = "daka-client"
	testRedirect = "http://daka.test/api/auth/oidc/callback"
)

// fakeIdP is an in-process OIDC provider: discovery, JWKS, authorize and token
// endpoints, signing ID tokens with a throwaway RSA key.
type fakeIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey
	clk *clock

	mu    sync.Mutex
	codes map[string]issued
	// per-test knobs
	sub       string
	audience  string
	issuer    string // overrides the iss claim
	expiresIn time.Duration
	authTime  func() time.Time
	nonceOver string // overrides the nonce claim
	picture   string // picture in ID token
	name      string // name in ID token
	userInfo  map[string]any
	lastAuth  url.Values
}

type issued struct {
	challenge, nonce string
}

func newFakeIdP(t *testing.T, clk *clock) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeIdP{key: key, clk: clk, codes: map[string]issued{}, sub: "sub-1", audience: testClientID, expiresIn: time.Hour}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize",
			"token_endpoint": f.srv.URL + "/token", "userinfo_endpoint": f.srv.URL + "/userinfo",
			"jwks_uri": f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", f.token)
	mux.HandleFunc("/userinfo", f.handleUserInfo)
	return f
}

func (f *fakeIdP) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.userInfo != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.userInfo)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// authorize plays the user agent at the IdP: given the Location of
// /api/auth/oidc/login it returns the callback path+query the IdP redirects to.
func (f *fakeIdP) authorize(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	require.NoError(t, err)
	require.Equal(t, f.srv.URL+"/authorize", u.Scheme+"://"+u.Host+u.Path)
	q := u.Query()
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.NotEmpty(t, q.Get("code_challenge"))
	require.Equal(t, testClientID, q.Get("client_id"))
	require.Equal(t, testRedirect, q.Get("redirect_uri"))
	require.Equal(t, "code", q.Get("response_type"))
	require.Contains(t, q.Get("scope"), "openid")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastAuth = q
	code := "code-" + q.Get("state")[:6]
	f.codes[code] = issued{challenge: q.Get("code_challenge"), nonce: q.Get("nonce")}
	cb := url.Values{"code": {code}, "state": {q.Get("state")}}
	return "/api/auth/oidc/callback?" + cb.Encode()
}

func (f *fakeIdP) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	rec, ok := f.codes[r.Form.Get("code")]
	delete(f.codes, r.Form.Get("code")) // single use
	f.mu.Unlock()
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != rec.challenge {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	now := f.clk.Now()
	f.mu.Lock()
	tokName := "Alice"
	if f.name != "" {
		tokName = f.name
	}
	claims := map[string]any{
		"iss": f.srv.URL, "sub": f.sub, "aud": f.audience, "iat": now.Unix(), "exp": now.Add(f.expiresIn).Unix(),
		"nonce": rec.nonce, "name": tokName, "email": "alice@example.com",
	}
	if f.issuer != "" {
		claims["iss"] = f.issuer
	}
	if f.nonceOver != "" {
		claims["nonce"] = f.nonceOver
	}
	if f.authTime != nil {
		claims["auth_time"] = f.authTime().Unix()
	}
	if f.picture != "" {
		claims["picture"] = f.picture
	}
	f.mu.Unlock()
	payload, _ := json.Marshal(claims)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: f.key, KeyID: "k1"}}, nil)
	if err != nil {
		panic(err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		panic(err)
	}
	idt, _ := jws.CompactSerialize()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-secret", "refresh_token": "rt-secret", "token_type": "Bearer", "expires_in": 3600, "id_token": idt})
}

type oidcEnv struct {
	*env
	idp *fakeIdP
}

func newOIDCEnv(t *testing.T, secure bool) *oidcEnv {
	t.Helper()
	g := openDB(t)
	clk := newClock()
	idp := newFakeIdP(t, clk)
	o, err := NewOIDC(OIDCConfig{
		Issuer: idp.srv.URL, ClientID: testClientID, ClientSecret: "cs", RedirectURL: testRedirect,
		HTTPClient: idp.srv.Client(), Now: clk.Now,
	})
	require.NoError(t, err)
	m := NewManager(g, ManagerOptions{SecureCookie: secure, Now: clk.Now})
	e := &env{clk: clk, m: m, lim: &recLimiter{}}
	e.h = NewHandler(HandlerOptions{DB: g, Manager: m, OIDC: o, SecureCookie: secure, Now: clk.Now, Timezone: "Asia/Shanghai"})
	e.api = Middleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		_, _ = w.Write([]byte(u.ID))
	}))
	return &oidcEnv{env: e, idp: idp}
}

// start performs GET /api/auth/oidc/login and returns the daka_oidc cookie and
// the callback URL produced by the IdP.
func (e *oidcEnv) start(t *testing.T, target string, cookies ...*http.Cookie) (*http.Cookie, string) {
	t.Helper()
	rec := call(e.h, "GET", target, "", nil, cookies...)
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	c := cookieNamed(rec, OIDCCookieName)
	require.NotNil(t, c)
	return c, e.idp.authorize(t, rec.Header().Get("Location"))
}

func (e *oidcEnv) login(t *testing.T) *http.Cookie {
	t.Helper()
	oc, cb := e.start(t, "/api/auth/oidc/login")
	rec := call(e.h, "GET", cb, "", nil, oc)
	require.Equal(t, http.StatusFound, rec.Code, "callback response: code %d body %s", rec.Code, rec.Body.String())
	sc := cookieNamed(rec, SessionCookieName)
	require.NotNil(t, sc)
	return sc
}

func TestOIDCLoginSuccess(t *testing.T) {
	for _, secure := range []bool{true, false} {
		e := newOIDCEnv(t, secure)
		rec := call(e.h, "GET", "/api/auth/oidc/login", "", nil)
		require.Equal(t, http.StatusFound, rec.Code)
		oc := cookieNamed(rec, OIDCCookieName)
		require.NotNil(t, oc)
		require.True(t, oc.HttpOnly)
		require.Equal(t, "/api/auth", oc.Path)
		require.Equal(t, http.SameSiteLaxMode, oc.SameSite, "Strict would be dropped on the IdP redirect back")
		require.Equal(t, secure, oc.Secure)
		require.Equal(t, 600, oc.MaxAge)
		cb := e.idp.authorize(t, rec.Header().Get("Location"))
		require.Empty(t, e.idp.lastAuth.Get("prompt"))
		require.Empty(t, e.idp.lastAuth.Get("max_age"))

		rec = call(e.h, "GET", cb, "", nil, oc)
		require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
		require.Equal(t, "/", rec.Header().Get("Location"))
		sc := cookieNamed(rec, SessionCookieName)
		require.NotNil(t, sc)
		require.Equal(t, http.SameSiteLaxMode, sc.SameSite)
		require.Equal(t, secure, sc.Secure)
		require.Negative(t, cookieNamed(rec, OIDCCookieName).MaxAge, "pending cookie is cleared")

		wantID := OIDCUserID(e.idp.srv.URL, "sub-1")
		rec = call(e.api, "GET", "/x", "", nil, sc)
		require.Equal(t, 200, rec.Code)
		require.Equal(t, wantID, rec.Body.String())
		require.NotEqual(t, DefaultUserID, wantID)

		var u dbmodels.User
		require.NoError(t, e.m.db.First(&u, "id = ?", wantID).Error)
		require.Equal(t, "Alice", u.Name)
		require.Equal(t, "alice@example.com", u.Email)
		var st dbmodels.UserSettings
		require.NoError(t, e.m.db.First(&st, "user_id = ?", wantID).Error)
		require.Equal(t, "Asia/Shanghai", st.Timezone)

		rec = call(e.h, "GET", "/api/auth/me", "", nil, sc)
		require.Equal(t, 200, rec.Code)
		require.Contains(t, rec.Body.String(), `"csrf_token"`)
		require.Contains(t, rec.Body.String(), `"email":"alice@example.com"`)
		rec = call(e.h, "GET", "/api/auth/status", "", nil, sc)
		require.JSONEq(t, `{"oidc_enabled":true,"login_required":true,"authenticated":true}`, rec.Body.String())

		// a second login by the same identity reuses the user
		e.login(t)
		var n int64
		e.m.db.Model(&dbmodels.User{}).Where("id = ?", wantID).Count(&n)
		require.EqualValues(t, 1, n)
		// no OIDC token is persisted anywhere in the session table
		var ss []dbmodels.Session
		require.NoError(t, e.m.db.Find(&ss).Error)
		b, _ := json.Marshal(ss)
		require.NotContains(t, string(b), "at-secret")
		require.NotContains(t, string(b), "rt-secret")
	}
}

func TestOIDCShapedLoginDisablesSharedSecret(t *testing.T) {
	e := newOIDCEnv(t, false)
	require.Equal(t, 404, call(e.h, "POST", "/api/auth/login", `{"secret":"x"}`, nil).Code)
}

func TestOIDCCallbackRejections(t *testing.T) {
	type tc struct {
		name  string
		setup func(e *oidcEnv)
		// mutate rewrites the callback URL / cookie before it is delivered
		mutate func(cb string, c *http.Cookie) (string, *http.Cookie)
	}
	setQ := func(k, v string) func(string, *http.Cookie) (string, *http.Cookie) {
		return func(cb string, c *http.Cookie) (string, *http.Cookie) {
			u, _ := url.Parse(cb)
			q := u.Query()
			q.Set(k, v)
			u.RawQuery = q.Encode()
			return u.String(), c
		}
	}
	tests := []tc{
		{name: "wrong state", mutate: setQ("state", "attacker-state")},
		{name: "missing state", mutate: setQ("state", "")},
		{name: "unknown code", mutate: setQ("code", "bogus")},
		{name: "missing code", mutate: setQ("code", "")},
		{name: "provider error", mutate: setQ("error", "access_denied")},
		{name: "no pending cookie", mutate: func(cb string, _ *http.Cookie) (string, *http.Cookie) { return cb, nil }},
		{name: "wrong pending cookie", mutate: func(cb string, c *http.Cookie) (string, *http.Cookie) {
			return cb, &http.Cookie{Name: c.Name, Value: "AAAA"}
		}},
		{name: "wrong nonce", setup: func(e *oidcEnv) { e.idp.nonceOver = "other-nonce" }},
		{name: "expired id token", setup: func(e *oidcEnv) { e.idp.expiresIn = -time.Hour }},
		{name: "wrong audience", setup: func(e *oidcEnv) { e.idp.audience = "someone-else" }},
		{name: "wrong issuer", setup: func(e *oidcEnv) { e.idp.issuer = "https://evil.example" }},
		{name: "pending expired after 10 minutes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOIDCEnv(t, false)
			if tt.setup != nil {
				tt.setup(e)
			}
			oc, cb := e.start(t, "/api/auth/oidc/login")
			if tt.name == "pending expired after 10 minutes" {
				e.clk.Advance(11 * time.Minute)
			}
			c := oc
			if tt.mutate != nil {
				cb, c = tt.mutate(cb, oc)
			}
			var cookies []*http.Cookie
			if c != nil {
				cookies = append(cookies, c)
			}
			rec := call(e.h, "GET", cb, "", nil, cookies...)
			require.GreaterOrEqual(t, rec.Code, 400, rec.Body.String())
			require.Nil(t, cookieNamed(rec, SessionCookieName), "no session on failure")
			require.NotContains(t, rec.Body.String(), "nonce", "reason is not leaked")
			var n int64
			e.m.db.Model(&dbmodels.Session{}).Count(&n)
			require.Zero(t, n)
		})
	}
}

func TestOIDCReplayedCallback(t *testing.T) {
	e := newOIDCEnv(t, false)
	oc, cb := e.start(t, "/api/auth/oidc/login")
	require.Equal(t, 302, call(e.h, "GET", cb, "", nil, oc).Code)
	rec := call(e.h, "GET", cb, "", nil, oc)
	require.Equal(t, 400, rec.Code)
	require.Nil(t, cookieNamed(rec, SessionCookieName))
	var n int64
	e.m.db.Model(&dbmodels.Session{}).Count(&n)
	require.EqualValues(t, 1, n)
}

func TestOIDCPKCEVerifierMismatch(t *testing.T) {
	e := newOIDCEnv(t, false)
	oc, cb := e.start(t, "/api/auth/oidc/login")
	// An attacker who intercepted the code registered a different challenge.
	e.idp.mu.Lock()
	for code, rec := range e.idp.codes {
		rec.challenge = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		e.idp.codes[code] = rec
	}
	e.idp.mu.Unlock()
	rec := call(e.h, "GET", cb, "", nil, oc)
	require.Equal(t, 400, rec.Code)
	require.Nil(t, cookieNamed(rec, SessionCookieName))
}

func TestOIDCCallbackWithoutLoginIsRejected(t *testing.T) {
	e := newOIDCEnv(t, false)
	rec := call(e.h, "GET", "/api/auth/oidc/callback?code=x&state=y", "", nil)
	require.Equal(t, 400, rec.Code)
}

func TestOIDCStateAndNonceAreUnique(t *testing.T) {
	e := newOIDCEnv(t, false)
	e.start(t, "/api/auth/oidc/login")
	s1, n1 := e.idp.lastAuth.Get("state"), e.idp.lastAuth.Get("nonce")
	e.start(t, "/api/auth/oidc/login")
	require.NotEqual(t, s1, e.idp.lastAuth.Get("state"))
	require.NotEqual(t, n1, e.idp.lastAuth.Get("nonce"))
	require.GreaterOrEqual(t, len(s1), 43)
}

func TestOIDCReauth(t *testing.T) {
	e := newOIDCEnv(t, false)
	sc := e.login(t)
	rec := call(e.h, "GET", "/api/auth/me", "", nil, sc)
	var me map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &me))
	hdr := map[string]string{CSRFHeader: me["csrf_token"]}

	// reauth endpoint hands out the redirect
	rec = call(e.h, "POST", "/api/auth/reauth", "", hdr, sc)
	require.Equal(t, 200, rec.Code)
	require.JSONEq(t, `{"redirect":"/api/auth/oidc/login?reauth=1"}`, rec.Body.String())
	require.Equal(t, 403, call(e.h, "POST", "/api/auth/reauth", "", nil, sc).Code)

	// reauth login requires a session
	require.Equal(t, 401, call(e.h, "GET", "/api/auth/oidc/login?reauth=1", "", nil).Code)

	// 1. stale auth_time is rejected (IdP ignored max_age=0)
	e.idp.authTime = func() time.Time { return e.clk.Now().Add(-10 * time.Minute) }
	oc, cb := e.start(t, "/api/auth/oidc/login?reauth=1", sc)
	require.Equal(t, "login", e.idp.lastAuth.Get("prompt"))
	require.Equal(t, "0", e.idp.lastAuth.Get("max_age"))
	require.Equal(t, 400, call(e.h, "GET", cb, "", nil, oc, sc).Code)
	s, _ := e.m.Session(requestWith(sc))
	require.True(t, s.ReauthUntil.IsZero())

	// 2. missing auth_time is rejected
	e.idp.authTime = nil
	oc, cb = e.start(t, "/api/auth/oidc/login?reauth=1", sc)
	require.Equal(t, 400, call(e.h, "GET", cb, "", nil, oc, sc).Code)

	// 3. a different identity cannot reauth the session
	e.idp.authTime = func() time.Time { return e.clk.Now() }
	e.idp.sub = "someone-else"
	oc, cb = e.start(t, "/api/auth/oidc/login?reauth=1", sc)
	require.Equal(t, 403, call(e.h, "GET", cb, "", nil, oc, sc).Code)
	e.idp.sub = "sub-1"

	// 4. fresh auth_time marks the existing session, no new session is made
	oc, cb = e.start(t, "/api/auth/oidc/login?reauth=1", sc)
	rec = call(e.h, "GET", cb, "", nil, oc, sc)
	require.Equal(t, 302, rec.Code, rec.Body.String())
	require.Equal(t, "/", rec.Header().Get("Location"))
	require.Nil(t, cookieNamed(rec, SessionCookieName))
	s, ok := e.m.Session(requestWith(sc))
	require.True(t, ok)
	require.WithinDuration(t, e.clk.Now().Add(5*time.Minute), s.ReauthUntil, time.Second)
}

func requestWith(c *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	return r
}

func TestOIDCDiscoveryFailureIsRetried(t *testing.T) {
	e := newOIDCEnv(t, false)
	// A provider that is down at first: point a fresh client at a dead URL.
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	o, err := NewOIDC(OIDCConfig{Issuer: dead.URL, ClientID: "c", RedirectURL: testRedirect, Now: e.clk.Now})
	require.NoError(t, err, "construction must not need the network")
	h := NewHandler(HandlerOptions{DB: e.m.db, Manager: e.m, OIDC: o})
	require.Equal(t, 502, call(h, "GET", "/api/auth/oidc/login", "", nil).Code)
}

func TestNewOIDCValidates(t *testing.T) {
	_, err := NewOIDC(OIDCConfig{Issuer: "https://x"})
	require.Error(t, err)
}

func TestOIDCAvatarURLFromIDTokenAndUserInfo(t *testing.T) {
	e := newOIDCEnv(t, false)

	// 1. Picture in ID token is extracted and stored
	e.idp.picture = "https://example.com/avatar1.png"
	sc := e.login(t)
	wantID := OIDCUserID(e.idp.srv.URL, "sub-1")

	var u dbmodels.User
	require.NoError(t, e.m.db.First(&u, "id = ?", wantID).Error)
	require.Equal(t, "https://example.com/avatar1.png", u.AvatarURL)

	rec := call(e.h, "GET", "/api/auth/me", "", nil, sc)
	require.Equal(t, 200, rec.Code)
	var me map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &me))
	require.Equal(t, "https://example.com/avatar1.png", me["avatar_url"])

	// 2. Picture missing from ID token falls back to UserInfo endpoint
	e.idp.mu.Lock()
	e.idp.sub = "sub-2"
	e.idp.name = "Bob"
	e.idp.picture = ""
	e.idp.userInfo = map[string]any{
		"sub":     "sub-2",
		"name":    "Bob",
		"email":   "bob@example.com",
		"picture": "https://example.com/avatar2.png",
	}
	e.idp.mu.Unlock()

	sc2 := e.login(t)
	wantID2 := OIDCUserID(e.idp.srv.URL, "sub-2")
	var u2 dbmodels.User
	require.NoError(t, e.m.db.First(&u2, "id = ?", wantID2).Error)
	require.Equal(t, "https://example.com/avatar2.png", u2.AvatarURL)
	require.Equal(t, "Bob", u2.Name)

	rec2 := call(e.h, "GET", "/api/auth/me", "", nil, sc2)
	require.Equal(t, 200, rec2.Code)
	var me2 map[string]string
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &me2))
	require.Equal(t, "https://example.com/avatar2.png", me2["avatar_url"])

	// 3. Dangerous scheme in picture (e.g. javascript:) is discarded by sanitizeAvatarURL
	e.idp.mu.Lock()
	e.idp.sub = "sub-3"
	e.idp.picture = "javascript:alert(1)"
	e.idp.userInfo = nil
	e.idp.mu.Unlock()

	sc3 := e.login(t)
	wantID3 := OIDCUserID(e.idp.srv.URL, "sub-3")
	var u3 dbmodels.User
	require.NoError(t, e.m.db.First(&u3, "id = ?", wantID3).Error)
	require.Empty(t, u3.AvatarURL)

	rec3 := call(e.h, "GET", "/api/auth/me", "", nil, sc3)
	require.Equal(t, 200, rec3.Code)
	var me3 map[string]string
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &me3))
	require.NotContains(t, me3, "avatar_url")

	// 4. UserInfo sub mismatch: discarded per OIDC Core §5.3.2
	e.idp.mu.Lock()
	e.idp.sub = "sub-4"
	e.idp.picture = ""
	e.idp.userInfo = map[string]any{
		"sub":     "impostor-sub",
		"name":    "Impostor",
		"email":   "impostor@example.com",
		"picture": "https://example.com/impostor.png",
	}
	e.idp.mu.Unlock()

	sc4 := e.login(t)
	wantID4 := OIDCUserID(e.idp.srv.URL, "sub-4")
	var u4 dbmodels.User
	require.NoError(t, e.m.db.First(&u4, "id = ?", wantID4).Error)
	require.Empty(t, u4.AvatarURL, "claims from mismatched userinfo sub must not be adopted")
	require.NotEqual(t, "Impostor", u4.Name)

	rec4 := call(e.h, "GET", "/api/auth/me", "", nil, sc4)
	require.Equal(t, 200, rec4.Code)
	var me4 map[string]string
	require.NoError(t, json.Unmarshal(rec4.Body.Bytes(), &me4))
	require.NotContains(t, me4, "avatar_url")
}
