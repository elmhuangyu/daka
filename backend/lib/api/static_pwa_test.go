package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStaticHandlerPWAHeaders(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	for _, f := range []string{"index.html", "sw.js", "manifest.webmanifest", "assets/app-1.js"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644))
	}
	h := StaticHandler(dir)
	get := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://x"+p, nil))
		return w
	}
	sw := get("/sw.js")
	require.Equal(t, 200, sw.Code)
	require.Equal(t, "/", sw.Header().Get("Service-Worker-Allowed"))
	require.Contains(t, sw.Header().Get("Cache-Control"), "no-cache")
	require.Contains(t, sw.Header().Get("Content-Type"), "javascript")
	m := get("/manifest.webmanifest")
	require.Equal(t, "application/manifest+json", m.Header().Get("Content-Type"))
	require.Equal(t, "no-cache", m.Header().Get("Cache-Control"))
	require.Contains(t, get("/assets/app-1.js").Header().Get("Cache-Control"), "immutable")
	require.Equal(t, "no-cache", get("/").Header().Get("Cache-Control"))
}
