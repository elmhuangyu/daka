package e2e

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
)

const (
	idpClientID = "daka-e2e"
	idpSubject  = "alice-sub"
)

// fakeIdP is an in-process OIDC provider: discovery, JWKS and token endpoint
// (authorization code + PKCE S256), signing ID tokens with a throwaway RSA key.
// The authorization endpoint is played by the test acting as the user agent.
type fakeIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey
	clk *clock

	mu       sync.Mutex
	codes    map[string]issuedCode
	lastAuth url.Values
	// authAge makes the auth_time claim older than now (stale reauth tests).
	authAge time.Duration
}

type issuedCode struct{ challenge, nonce string }

func newFakeIdP(t *testing.T, clk *clock) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeIdP{key: key, clk: clk, codes: map[string]issuedCode{}}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize",
			"token_endpoint": f.srv.URL + "/token", "jwks_uri": f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", f.token)
	return f
}

// authorize plays the user agent at the IdP: it validates the authorization
// request that daka produced and returns the callback path+query the IdP would
// redirect the browser to.
func (f *fakeIdP) authorize(t *testing.T, location, redirectURL string) (callback string, q url.Values) {
	t.Helper()
	u, err := url.Parse(location)
	require.NoError(t, err)
	require.Equal(t, f.srv.URL+"/authorize", u.Scheme+"://"+u.Host+u.Path)
	q = u.Query()
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, idpClientID, q.Get("client_id"))
	require.Equal(t, redirectURL, q.Get("redirect_uri"))
	require.Equal(t, "S256", q.Get("code_challenge_method"), "PKCE is mandatory")
	require.NotEmpty(t, q.Get("code_challenge"))
	require.NotEmpty(t, q.Get("state"))
	require.NotEmpty(t, q.Get("nonce"))
	require.Contains(t, q.Get("scope"), "openid")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastAuth = q
	code := "code-" + q.Get("state")[:8]
	f.codes[code] = issuedCode{challenge: q.Get("code_challenge"), nonce: q.Get("nonce")}
	cb := url.Values{"code": {code}, "state": {q.Get("state")}}
	return "/api/auth/oidc/callback?" + cb.Encode(), q
}

func (f *fakeIdP) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	rec, ok := f.codes[r.Form.Get("code")]
	delete(f.codes, r.Form.Get("code")) // single use
	age := f.authAge
	f.mu.Unlock()
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != rec.challenge {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	now := f.clk.Now()
	claims := map[string]any{
		"iss": f.srv.URL, "sub": idpSubject, "aud": idpClientID, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"nonce": rec.nonce, "name": "Alice", "email": "alice@example.com", "auth_time": now.Add(-age).Unix(),
	}
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
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-secret", "token_type": "Bearer", "expires_in": 3600, "id_token": idt})
}
