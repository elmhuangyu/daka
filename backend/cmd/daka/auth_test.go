package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmhuangyu/daka/backend/lib/app"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func TestSharedSecretSessionFlowOverPlainHTTP(t *testing.T) {
	// secure_cookie=false is what makes plain-HTTP LAN deployments work: the
	// cookie jar (like a browser) would refuse to send a Secure cookie over http.
	ts := startServer(t, func(c *config.Config) {
		c.Auth.SharedSecret = "s3cret-s3cret-s3cret"
		c.Auth.SecureCookie = false
	})
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	cl := &http.Client{Jar: jar}
	do := func(method, path, body string, hdr map[string]string) (int, string) {
		req, err := http.NewRequest(method, ts.base+path, strings.NewReader(body))
		require.NoError(t, err)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := cl.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	code, _ := do("GET", "/api/view/today", "", nil)
	require.Equal(t, 401, code)
	code, body := do("GET", "/api/auth/status", "", nil)
	require.Equal(t, 200, code)
	require.JSONEq(t, `{"oidc_enabled":false,"login_required":true,"authenticated":false}`, body)

	code, _ = do("POST", "/api/auth/login", `{"secret":"wrong"}`, nil)
	require.Equal(t, 401, code)
	code, body = do("POST", "/api/auth/login", `{"secret":"s3cret-s3cret-s3cret"}`, nil)
	require.Equal(t, 200, code, body)

	code, _ = do("GET", "/api/view/today", "", nil)
	require.Equal(t, 200, code)

	item := `{"name":"x","tracker_type":"boolean","rule":{"schedule_type":"fixed_rrule","rrule_string":"FREQ=DAILY","holiday_mode":"none"}}`
	code, _ = do("POST", "/api/items", item, nil)
	require.Equal(t, 403, code, "state change without CSRF header")

	_, body = do("GET", "/api/auth/me", "", nil)
	tok := between(t, body, `"csrf_token":"`, `"`)
	code, body = do("POST", "/api/items", item, map[string]string{auth.CSRFHeader: tok})
	require.Equal(t, 201, code, body)

	code, _ = do("POST", "/api/auth/logout", "", map[string]string{auth.CSRFHeader: tok})
	require.Equal(t, 204, code)
	code, _ = do("GET", "/api/view/today", "", nil)
	require.Equal(t, 401, code)
}

func between(t *testing.T, s, a, b string) string {
	t.Helper()
	i := strings.Index(s, a)
	require.GreaterOrEqual(t, i, 0, s)
	s = s[i+len(a):]
	return s[:strings.Index(s, b)]
}

func TestSingleUserModeRejectsCrossOriginWrites(t *testing.T) {
	ts := startServer(t, nil)
	item := `{"name":"x","tracker_type":"boolean","rule":{"schedule_type":"fixed_rrule","rrule_string":"FREQ=DAILY","holiday_mode":"none"}}`
	post := func(origin string) int {
		req, err := http.NewRequest("POST", ts.base+"/api/items", bytes.NewBufferString(item))
		require.NoError(t, err)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	require.Equal(t, 201, post(""))
	require.Equal(t, 201, post(ts.base))
	require.Equal(t, 403, post("https://evil.example"))
}

func TestUserClaimCLI(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "daka.db")
	env := map[string]string{"DAKA_DB_DSN": dsn}
	getenv := func(k string) string { return env[k] }

	d, err := db.Open(config.DBConfig{Driver: config.DriverSQLite, DSN: config.Secret(dsn)})
	require.NoError(t, err)
	require.NoError(t, dbmodels.AutoMigrate(d.Write()))
	require.NoError(t, app.BootstrapDefaultUser(d.Write(), "UTC"))
	target := auth.OIDCUserID("https://idp.example", "sub-9")
	require.NoError(t, d.Write().Create(&dbmodels.User{ID: target, Name: "o"}).Error)
	require.NoError(t, d.Write().Create(&dbmodels.UserSettings{UserID: target, Timezone: "UTC", WeekStart: 1}).Error)
	require.NoError(t, d.Write().Create(&dbmodels.Item{UserID: auth.DefaultUserID, Name: "mine", TrackerType: "boolean"}).Error)
	require.NoError(t, d.Close())

	var out bytes.Buffer
	require.ErrorContains(t, runUserClaim([]string{"--to", "nopipe"}, getenv, &out), "iss")
	require.NoError(t, runUserClaim([]string{"--from", "default_user", "--to", "https://idp.example|sub-9"}, getenv, &out))
	require.Contains(t, out.String(), target)

	d, err = db.Open(config.DBConfig{Driver: config.DriverSQLite, DSN: config.Secret(dsn)})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	var n int64
	require.NoError(t, d.Write().Model(&dbmodels.Item{}).Where("user_id = ?", target).Count(&n).Error)
	require.EqualValues(t, 1, n)
	// reachable through run() as `daka user claim`
	require.Error(t, run(t.Context(), []string{"user", "claim"}, getenv, io.Discard))
}

func TestDNSRebindingHostGuard(t *testing.T) {
	get := func(ts *testServer, host, origin, method string) int {
		req, err := http.NewRequest(method, ts.base+"/api/view/today", nil)
		require.NoError(t, err)
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	ts := startServer(t, nil)
	port := strings.TrimPrefix(ts.base, "http://127.0.0.1")
	require.Equal(t, 200, get(ts, "localhost"+port, "", "GET"))
	require.Equal(t, 200, get(ts, "127.0.0.1"+port, "", "GET"))
	require.Equal(t, 403, get(ts, "evil.test"+port, "http://evil.test"+port, "GET"))
	require.Equal(t, 403, get(ts, "evil.test"+port, "http://evil.test"+port, "POST"))

	ts2 := startServer(t, func(c *config.Config) { c.Server.AllowedHosts = []string{"daka.example.com"} })
	require.Equal(t, 200, get(ts2, "daka.example.com", "", "GET"))
	require.Equal(t, 403, get(ts2, "evil.test", "", "GET"))

	// with login configured the guard is off unless allowed_hosts is set (LAN use)
	ts3 := startServer(t, func(c *config.Config) { c.Auth.SharedSecret = "s3cret-s3cret-s3cret" })
	require.Equal(t, 401, get(ts3, "nas.lan", "", "GET"))
}
