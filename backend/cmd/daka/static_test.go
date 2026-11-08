package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/config"
)

func TestStaticDirHeadersAndRouting(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	for f, body := range map[string]string{"index.html": "INDEX", "sw.js": "SW", "assets/app-1.js": "JS"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644))
	}
	ts := startServer(t, func(c *config.Config) { c.Server.StaticDir = dir })
	get := func(path, auth string) (*http.Response, string) {
		req, err := http.NewRequest("GET", ts.base+path, nil)
		require.NoError(t, err)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp, string(b)
	}
	resp, body := get("/", "")
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "INDEX", body)
	require.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	resp, _ = get("/sw.js", "")
	require.Contains(t, resp.Header.Get("Cache-Control"), "no-cache")
	resp, _ = get("/assets/app-1.js", "")
	require.Contains(t, resp.Header.Get("Cache-Control"), "immutable")

	// A read token on a static path is not routed through the API gate.
	resp, body = get("/sw.js", "Bearer dk_notarealtoken")
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "SW", body)

	resp, body = get("/api/unknown", "")
	require.Equal(t, 404, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "json")
	require.NotContains(t, body, "INDEX")
}

func TestServeRejectsBadStaticDir(t *testing.T) {
	cfg := config.Default()
	cfg.DB.DSN = config.Secret(filepath.Join(t.TempDir(), "daka.db"))
	cfg.Holiday.Disabled = true
	cfg.Server.StaticDir = filepath.Join(t.TempDir(), "missing")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	err = serve(context.Background(), &cfg, ln, zerolog.Nop())
	require.ErrorContains(t, err, "static_dir")
}
