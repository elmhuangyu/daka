package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveStaticDir(t *testing.T) {
	root := t.TempDir()
	withIndex := filepath.Join(root, "with")
	require.NoError(t, os.MkdirAll(withIndex, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(withIndex, "index.html"), []byte("x"), 0o644))
	empty := filepath.Join(root, "empty")
	require.NoError(t, os.MkdirAll(empty, 0o755))
	file := filepath.Join(root, "file.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	abs, has, err := ResolveStaticDir(withIndex)
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, withIndex, abs)

	_, has, err = ResolveStaticDir(empty)
	require.NoError(t, err)
	require.False(t, has)

	_, _, err = ResolveStaticDir(filepath.Join(root, "typo"))
	require.Error(t, err)
	_, _, err = ResolveStaticDir(file)
	require.Error(t, err)

	t.Chdir(root)
	abs, has, err = ResolveStaticDir("with")
	require.NoError(t, err)
	require.True(t, has)
	require.True(t, filepath.IsAbs(abs))
}

func TestStaticHead(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0o644))
	w := httptest.NewRecorder()
	StaticHandler(dir).ServeHTTP(w, httptest.NewRequest("HEAD", "http://x/", nil))
	require.Equal(t, 200, w.Code)
	require.Empty(t, w.Body.String())
}
