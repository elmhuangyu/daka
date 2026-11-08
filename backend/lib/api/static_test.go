package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStaticHandler(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dist")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("JS"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.txt"), []byte("SECRET"), 0o644))

	h := StaticHandler(dir)
	tests := []struct {
		name, method, path string
		code               int
		body               string
	}{
		{"root", "GET", "/", 200, "INDEX"},
		{"asset", "GET", "/assets/app.js", 200, "JS"},
		{"spa route", "GET", "/history/2026", 200, "INDEX"},
		{"dir falls back", "GET", "/assets", 200, "INDEX"},
		{"missing asset", "GET", "/assets/nope.js", 404, ""},
		{"traversal", "GET", "/../secret.txt", 404, ""},
		{"deep traversal", "GET", "/assets/../../secret.txt", 404, ""},
		{"post", "POST", "/", 405, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://x"+tc.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, tc.code, w.Code)
			if tc.body != "" {
				require.Equal(t, tc.body, w.Body.String())
			}
			require.NotContains(t, w.Body.String(), "SECRET")
		})
	}
}
