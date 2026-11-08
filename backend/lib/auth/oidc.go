package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
)

const (
	// OIDCCookieName holds the handle of the pending login (state / nonce / PKCE
	// verifier live server-side, keyed by the hash of this handle).
	OIDCCookieName = "daka_oidc"
	oidcPendingTTL = 10 * time.Minute
	// oidcCookiePath scopes the pending-login cookie to the auth endpoints.
	oidcCookiePath = "/api/auth"
	maxPending     = 10000
	// reauthMaxAge is the maximum age of auth_time accepted for a reauth.
	reauthMaxAge = 120 * time.Second
	// authTimeSkew tolerates IdP clock drift for auth_time in the future.
	authTimeSkew = 60 * time.Second
	// userInfoTimeout bounds the fallback call to the IdP UserInfo endpoint.
	userInfoTimeout = 5 * time.Second
)

// OIDCConfig configures the Authorization Code + PKCE client. The BFF keeps no
// OIDC access / refresh token: after the code exchange only the verified ID
// token claims (iss, sub, name, email, auth_time) are used, then everything is
// dropped.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	// HTTPClient is used for discovery, JWKS and the token endpoint (tests).
	HTTPClient *http.Client
	Now        func() time.Time
}

// OIDC is the relying-party side. Provider discovery is lazy and retried, so
// the server starts even while the IdP is unreachable.
type OIDC struct {
	cfg     OIDCConfig
	client  *http.Client
	now     func() time.Time
	pending *pendingStore

	mu       sync.Mutex
	provider *oidc.Provider
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewOIDC validates cfg and builds the client (no network access).
func NewOIDC(cfg OIDCConfig) (*OIDC, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return nil, errors.New("oidc: issuer, client_id and redirect_url are required")
	}
	o := &OIDC{cfg: cfg, client: cfg.HTTPClient, now: cfg.Now}
	if o.client == nil {
		o.client = &http.Client{Timeout: 10 * time.Second}
	}
	if o.now == nil {
		o.now = time.Now
	}
	o.pending = newPendingStore(o.now)
	return o, nil
}

func (o *OIDC) ctx(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, o.client)
}

func (o *OIDC) ensure(ctx context.Context) (*oidc.Provider, *oauth2.Config, *oidc.IDTokenVerifier, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.oauth != nil {
		return o.provider, o.oauth, o.verifier, nil
	}
	// The provider keeps its context for later JWKS fetches, so it must not be
	// the request context; carry only the HTTP client.
	p, err := oidc.NewProvider(o.ctx(context.Background()), o.cfg.Issuer)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("oidc discovery: %w", err)
	}
	scopes := o.cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"profile", "email"}
	}
	scopes = append([]string{oidc.ScopeOpenID}, withoutOpenID(scopes)...)
	o.provider = p
	o.oauth = &oauth2.Config{
		ClientID: o.cfg.ClientID, ClientSecret: o.cfg.ClientSecret,
		Endpoint: p.Endpoint(), RedirectURL: o.cfg.RedirectURL, Scopes: scopes,
	}
	o.verifier = p.VerifierContext(o.ctx(context.Background()), &oidc.Config{ClientID: o.cfg.ClientID, Now: o.now})
	return o.provider, o.oauth, o.verifier, nil
}

func withoutOpenID(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != oidc.ScopeOpenID {
			out = append(out, s)
		}
	}
	return out
}

// Identity is what a successful callback yields.
type Identity struct {
	Issuer, Subject string
	Name, Email     string
	AvatarURL       string
	AuthTime        time.Time // zero when the IdP omitted auth_time
	Reauth          bool      // the login was started as a reauth
}

// UserID is the Daka user ID of the identity.
func (i Identity) UserID() string { return OIDCUserID(i.Issuer, i.Subject) }

// Begin prepares an authorization redirect. It sets the daka_oidc cookie on w
// and returns the IdP URL. With reauth, the IdP is forced to re-prompt
// (prompt=login, max_age=0).
func (o *OIDC) Begin(ctx context.Context, w http.ResponseWriter, secure, reauth bool) (string, error) {
	_, oc, _, err := o.ensure(ctx)
	if err != nil {
		return "", err
	}
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()
	handle, err := o.pending.put(pendingLogin{state: state, nonce: nonce, verifier: verifier, reauth: reauth})
	if err != nil {
		return "", err
	}
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce)}
	if reauth {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("max_age", "0"))
	}
	setCookiePath(w, OIDCCookieName, handle, oidcCookiePath, int(oidcPendingTTL/time.Second), secure)
	return oc.AuthCodeURL(state, opts...), nil
}

// ErrOIDC is returned for every callback validation failure; the specific
// reason is wrapped for logging only and must not be shown to the client.
var ErrOIDC = errors.New("oidc callback rejected")

func reject(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrOIDC, fmt.Sprintf(format, a...))
}

// Complete finishes the flow: it consumes the pending login (single use),
// checks state, exchanges the code with the PKCE verifier, verifies the ID
// token (signature via JWKS, issuer, audience, expiry) and the nonce, and for
// reauth requires a fresh auth_time.
func (o *OIDC) Complete(ctx context.Context, w http.ResponseWriter, r *http.Request, secure bool) (Identity, error) {
	setCookiePath(w, OIDCCookieName, "", oidcCookiePath, -1, secure)
	c, err := r.Cookie(OIDCCookieName)
	if err != nil || c.Value == "" {
		return Identity{}, reject("missing %s cookie", OIDCCookieName)
	}
	p, ok := o.pending.take(c.Value)
	if !ok {
		return Identity{}, reject("unknown or expired login attempt")
	}
	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(p.state)) != 1 {
		return Identity{}, reject("state mismatch")
	}
	if e := q.Get("error"); e != "" {
		return Identity{}, reject("provider returned error %q", e)
	}
	code := q.Get("code")
	if code == "" {
		return Identity{}, reject("missing code")
	}
	provider, oc, verifier, err := o.ensure(ctx)
	if err != nil {
		return Identity{}, err
	}
	tok, err := oc.Exchange(o.ctx(ctx), code, oauth2.VerifierOption(p.verifier))
	if err != nil {
		return Identity{}, reject("code exchange: %v", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return Identity{}, reject("no id_token in token response")
	}
	idt, err := verifier.Verify(o.ctx(ctx), raw)
	if err != nil {
		return Identity{}, reject("id_token: %v", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(p.nonce)) != 1 {
		return Identity{}, reject("nonce mismatch")
	}
	var claims struct {
		Name     string `json:"name"`
		Username string `json:"preferred_username"`
		Email    string `json:"email"`
		Picture  string `json:"picture"`
		AuthTime int64  `json:"auth_time"`
	}
	if err := idt.Claims(&claims); err != nil {
		return Identity{}, reject("claims: %v", err)
	}
	if idt.Subject == "" {
		return Identity{}, reject("empty sub")
	}
	id := Identity{
		Issuer:    idt.Issuer,
		Subject:   idt.Subject,
		Email:     claims.Email,
		AvatarURL: sanitizeAvatarURL(claims.Picture),
		Reauth:    p.reauth,
	}
	hasExplicitName := claims.Name != "" || claims.Username != "" || claims.Email != ""
	id.Name = firstNonEmpty(claims.Name, claims.Username, claims.Email, "user")

	// If Picture was not present in the ID token, query the UserInfo endpoint if available.
	if id.AvatarURL == "" {
		uCtx, cancel := context.WithTimeout(o.ctx(ctx), userInfoTimeout)
		defer cancel()
		uInfo, err := provider.UserInfo(uCtx, oauth2.StaticTokenSource(tok))
		if err != nil {
			log.Warn().Err(err).Msg("oidc userinfo endpoint call failed")
		} else if uInfo.Subject != idt.Subject {
			// OIDC Core §5.3.2: The sub Claim in the UserInfo Response MUST match the
			// sub Claim in the ID Token; if they do not match, the Claims MUST NOT be used.
			log.Warn().Str("id_token_sub", idt.Subject).Str("userinfo_sub", uInfo.Subject).
				Msg("oidc userinfo subject mismatch; discarding userinfo claims")
		} else {
			var uClaims struct {
				Name     string `json:"name"`
				Username string `json:"preferred_username"`
				Email    string `json:"email"`
				Picture  string `json:"picture"`
			}
			if err := uInfo.Claims(&uClaims); err != nil {
				log.Warn().Err(err).Msg("failed to parse oidc userinfo claims")
			} else {
				id.AvatarURL = sanitizeAvatarURL(uClaims.Picture)
				if !hasExplicitName {
					id.Name = firstNonEmpty(uClaims.Name, uClaims.Username, uClaims.Email, "user")
				}
				if id.Email == "" {
					id.Email = uClaims.Email
				}
			}
		}
	}

	now := o.now()
	if claims.AuthTime > 0 {
		id.AuthTime = time.Unix(claims.AuthTime, 0)
	}
	if p.reauth {
		if id.AuthTime.IsZero() || now.Sub(id.AuthTime) > reauthMaxAge || id.AuthTime.Sub(now) > authTimeSkew {
			return Identity{}, reject("auth_time not fresh for reauth")
		}
	}
	if id.AuthTime.IsZero() {
		id.AuthTime = now
	}
	return id, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func sanitizeAvatarURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 1024 {
		return ""
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return ""
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	return raw
}

type pendingLogin struct {
	state, nonce, verifier string
	reauth                 bool
	expires                time.Time
}

// pendingStore keeps in-flight logins in memory, keyed by SHA-256 of the cookie
// handle. Entries are single-use and expire after 10 minutes; a restart drops
// them (the user just retries). The store is size-capped against flooding.
type pendingStore struct {
	now func() time.Time
	mu  sync.Mutex
	m   map[string]pendingLogin
}

func newPendingStore(now func() time.Time) *pendingStore {
	return &pendingStore{now: now, m: map[string]pendingLogin{}}
}

func (s *pendingStore) put(p pendingLogin) (string, error) {
	handle, err := randomToken()
	if err != nil {
		return "", err
	}
	now := s.now()
	p.expires = now.Add(oidcPendingTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.m {
		if !v.expires.After(now) {
			delete(s.m, k)
		}
	}
	if len(s.m) >= maxPending { // evict the oldest so a flood cannot lock real users out
		var oldest string
		var oldestExp time.Time
		for k, v := range s.m {
			if oldest == "" || v.expires.Before(oldestExp) {
				oldest, oldestExp = k, v.expires
			}
		}
		delete(s.m, oldest)
	}
	s.m[hashID(handle)] = p
	return handle, nil
}

func (s *pendingStore) take(handle string) (pendingLogin, bool) {
	k := hashID(handle)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[k]
	delete(s.m, k)
	if !ok || !p.expires.After(s.now()) {
		return pendingLogin{}, false
	}
	return p, true
}
