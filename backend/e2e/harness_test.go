// Package e2e drives the real daka HTTP stack (app.NewHandler, the same
// constructor cmd/daka uses) over a loopback httptest server with a temporary
// SQLite database, an injected clock and real sessions and CSRF. Tests
// behave like a client: a cookie jar and the X-Daka-CSRF header, nothing else.
package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/app"
	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/holiday"
	"github.com/elmhuangyu/daka/backend/lib/notify"
)

// clock is the injected, goroutine-safe test clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(t time.Time) *clock { return &clock{t: t} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func (c *clock) Advance(d time.Duration) { c.Set(c.Now().Add(d)) }

// syncBuffer is a log sink that can be read while the server writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// stack is one running daka server.
type stack struct {
	t    *testing.T
	srv  *httptest.Server
	clk  *clock
	db   *db.DB
	cfg  config.Config
	logs *syncBuffer
}

type stackOpts struct {
	mut      func(*config.Config)
	oidcHTTP *http.Client
	// clk, when set, is used instead of a fresh clock (the fake IdP shares it).
	clk *clock
}

// shanghaiNoon returns noon of the given date in Asia/Shanghai as a UTC time.
func shanghaiNoon(date string) time.Time {
	d, err := time.ParseInLocation("2006-01-02", date, time.FixedZone("CST", 8*3600))
	if err != nil {
		panic(err)
	}
	return d.Add(12 * time.Hour).UTC()
}

// startStack boots the production handler chain. The clock starts at noon
// (Asia/Shanghai) of startDate. Server logs (both the global logger and the
// context logger used by the database layer) are captured in stack.logs.
func startStack(t *testing.T, startDate string, o stackOpts) *stack {
	t.Helper()
	logs := &syncBuffer{}
	prev, prevCtx := log.Logger, zerolog.DefaultContextLogger
	lg := zerolog.New(logs).Level(zerolog.DebugLevel)
	log.Logger = lg
	zerolog.DefaultContextLogger = &lg
	t.Cleanup(func() { log.Logger, zerolog.DefaultContextLogger = prev, prevCtx })

	cfg := config.Default()
	cfg.DB.DSN = config.Secret(filepath.Join(t.TempDir(), "daka.db"))
	cfg.Timezone = "Asia/Shanghai"
	cfg.Server.RateLimit.Disabled = true
	cfg.Auth.SecureCookie = false // plain-HTTP httptest; the flag itself is asserted separately
	if o.mut != nil {
		o.mut(&cfg)
	}

	d, err := db.Open(cfg.DB)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.NoError(t, dbmodels.AutoMigrate(d.Write()))
	require.NoError(t, app.BootstrapDefaultUser(d.Write(), cfg.Timezone))
	hp, err := holiday.NewProvider() // embedded snapshot, no network
	require.NoError(t, err)
	clk := o.clk
	if clk == nil {
		clk = newClock(shanghaiNoon(startDate))
	} else {
		clk.Set(shanghaiNoon(startDate))
	}
	tg := notify.New(notify.Options{DB: d.Write(), Calendar: app.HolidayCalendars(hp)})

	// The OIDC redirect URL needs the server address, which exists only after
	// the listener is up: delegate through a late-bound handler.
	var (
		hmu sync.RWMutex
		h   http.Handler
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hmu.RLock()
		defer hmu.RUnlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	cfg.Server.Listen = srv.Listener.Addr().String()
	if cfg.Auth.Issuer != "" && cfg.Auth.RedirectURL == "" {
		cfg.Auth.RedirectURL = srv.URL + "/api/auth/oidc/callback"
	}
	require.NoError(t, cfg.Validate())
	handler, _, err := app.NewHandler(&cfg, d, hp, tg, app.Options{Now: clk.Now, OIDCHTTPClient: o.oidcHTTP})
	require.NoError(t, err)
	hmu.Lock()
	h = handler
	hmu.Unlock()
	return &stack{t: t, srv: srv, clk: clk, db: d, cfg: cfg, logs: logs}
}

// resp is one HTTP response.
type resp struct {
	Status int
	Body   string
	Header http.Header
}

// client is an API client with a cookie jar and optional CSRF token.
type client struct {
	t    *testing.T
	s    *stack
	http *http.Client
	csrf string
	// host overrides the Host header when non-empty.
	host string
}

func (s *stack) newClient() *client {
	jar, err := cookiejar.New(nil)
	require.NoError(s.t, err)
	return &client{t: s.t, s: s, http: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *client) cookie(name string) *http.Cookie {
	u, _ := url.Parse(c.s.srv.URL)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == name {
			return ck
		}
	}
	return nil
}

// do sends a request; a non-nil body is JSON-encoded. The CSRF header is sent
// on unsafe methods when the client holds a token.
func (c *client) do(method, path string, body any) resp {
	c.t.Helper()
	return c.doHdr(method, path, body, nil)
}

// doHdr is do with extra headers; an empty value removes the header.
func (c *client) doHdr(method, path string, body any, hdr map[string]string) resp {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(c.t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.s.srv.URL+path, rd)
	require.NoError(c.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" && method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-Daka-CSRF", c.csrf)
	}
	if c.host != "" {
		req.Host = c.host
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := c.http.Do(req)
	require.NoError(c.t, err)
	defer func() { _ = res.Body.Close() }()
	out, err := io.ReadAll(res.Body)
	require.NoError(c.t, err)
	return resp{Status: res.StatusCode, Body: string(out), Header: res.Header}
}

// ok asserts the status and returns the response.
func (c *client) ok(status int, method, path string, body any) resp {
	c.t.Helper()
	r := c.do(method, path, body)
	require.Equal(c.t, status, r.Status, "%s %s: %s", method, path, r.Body)
	return r
}

type obj = map[string]any

// jsonOf decodes the response body into T.
func jsonOf[T any](t *testing.T, r resp) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal([]byte(r.Body), &v), r.Body)
	return v
}

// createItem POSTs /api/items and returns the new item id.
func (c *client) createItem(body obj) uint {
	c.t.Helper()
	r := c.ok(http.StatusCreated, "POST", "/api/items", body)
	return uint(jsonOf[obj](c.t, r)["id"].(float64))
}

// setCalendar PUTs /api/settings/calendar.
func (c *client) setCalendar(body obj) {
	c.t.Helper()
	c.ok(http.StatusOK, "PUT", "/api/settings/calendar", body)
}
