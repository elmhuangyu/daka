package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/config"
)

type testServer struct {
	base   string
	cancel context.CancelFunc
	done   chan error
}

func startServer(t *testing.T, mut func(*config.Config)) *testServer {
	t.Helper()
	cfg := config.Default()
	cfg.DB.DSN = config.Secret(filepath.Join(t.TempDir(), "daka.db"))
	cfg.Timezone = "Asia/Shanghai"
	cfg.Holiday.Disabled = true // no background network refresh in tests
	if mut != nil {
		mut(&cfg)
	}
	require.NoError(t, cfg.Validate())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	ts := &testServer{base: "http://" + ln.Addr().String(), cancel: cancel, done: make(chan error, 1)}
	go func() { ts.done <- serve(ctx, &cfg, ln, zerolog.Nop()) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-ts.done:
			require.NoError(t, err)
		case <-time.After(15 * time.Second):
			t.Error("server did not shut down")
		}
	})
	return ts
}

func (ts *testServer) do(t *testing.T, method, path string, body any, cookie *http.Cookie) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, ts.base+path, rd)
	require.NoError(t, err)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, out
}

func TestEndToEnd(t *testing.T) {
	ts := startServer(t, nil)

	code, b := ts.do(t, "POST", "/api/items", map[string]any{
		"name": "Water plants", "tracker_type": "boolean",
		"rule": map[string]any{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY;INTERVAL=3", "holiday_mode": "none"},
	}, nil)
	require.Equal(t, 201, code, string(b))
	var item struct{ ID uint }
	require.NoError(t, json.Unmarshal(b, &item))

	type today struct {
		Date     string
		Timezone string
		Progress struct{ Done, Total int }
		Items    []struct {
			ID     uint
			Done   bool
			LogIDs []uint `json:"log_ids"`
		}
	}
	get := func() today {
		code, b := ts.do(t, "GET", "/api/view/today", nil, nil)
		require.Equal(t, 200, code, string(b))
		var td today
		require.NoError(t, json.Unmarshal(b, &td))
		return td
	}
	td := get()
	require.Equal(t, "Asia/Shanghai", td.Timezone)
	require.Len(t, td.Items, 1, "dtstart defaults to today, so the item is due today")
	require.False(t, td.Items[0].Done)

	code, b = ts.do(t, "POST", "/api/checkins", map[string]any{"item_id": item.ID, "mode": "set"}, nil)
	require.Equal(t, 200, code, string(b))
	td = get()
	require.True(t, td.Items[0].Done)
	require.Len(t, td.Items[0].LogIDs, 1)
	require.Equal(t, 1, td.Progress.Done)

	code, b = ts.do(t, "POST", "/api/checkins", map[string]any{"item_id": item.ID, "mode": "unset"}, nil)
	require.Equal(t, 200, code, string(b))
	td = get()
	require.False(t, td.Items[0].Done)
	require.Empty(t, td.Items[0].LogIDs)
	require.Equal(t, 0, td.Progress.Done)
	require.Equal(t, 1, td.Progress.Total)
}

func TestStaticDirServed(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644))
	ts := startServer(t, func(c *config.Config) { c.Server.StaticDir = dir })
	code, b := ts.do(t, "GET", "/some/route", nil, nil)
	require.Equal(t, 200, code)
	require.Equal(t, "<html>app</html>", string(b))
	code, _ = ts.do(t, "GET", "/api/nope", nil, nil)
	require.Equal(t, 404, code)
}

func TestBootstrapIdempotent(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "daka.db")
	for i, tz := range []string{"Asia/Shanghai", "UTC"} {
		ts := startServer(t, func(c *config.Config) { c.DB.DSN = config.Secret(dsn); c.Timezone = tz })
		code, b := ts.do(t, "GET", "/api/view/today", nil, nil)
		require.Equal(t, 200, code, string(b))
		var td struct{ Timezone string }
		require.NoError(t, json.Unmarshal(b, &td))
		require.Equal(t, "Asia/Shanghai", td.Timezone, fmt.Sprintf("run %d must not overwrite existing settings", i))
		ts.cancel()
		require.NoError(t, <-ts.done)
		ts.done <- nil
	}
}

func TestLoadConfigAcceptsIssuerRejectsBadTimezone(t *testing.T) {
	env := map[string]string{"DAKA_AUTH_ISSUER": "https://idp", "DAKA_AUTH_CLIENT_ID": "c", "DAKA_AUTH_REDIRECT_URL": "https://d/api/auth/oidc/callback"}
	_, err := loadConfig(nil, func(k string) string { return env[k] }, io.Discard)
	require.NoError(t, err)
	_, err = loadConfig(nil, func(k string) string { return map[string]string{"DAKA_AUTH_ISSUER": "https://idp"}[k] }, io.Discard)
	require.ErrorContains(t, err, "client_id")
	_, err = loadConfig(nil, func(k string) string { return map[string]string{"DAKA_TIMEZONE": "Mars/Base"}[k] }, io.Discard)
	require.ErrorContains(t, err, "invalid timezone")
}

func TestDefaultRateLimitWired(t *testing.T) {
	ts := startServer(t, nil) // default config must enable the limiter
	got429 := false
	for i := 0; i < config.DefaultRateLimitBurst*3 && !got429; i++ {
		code, _ := ts.do(t, "GET", "/api/view/today", nil, nil)
		got429 = code == 429
	}
	require.True(t, got429, "default rate limit not applied")
}
