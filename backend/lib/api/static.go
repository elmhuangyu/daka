package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ResolveStaticDir makes dir absolute and verifies that it is an existing
// directory, so a mistyped static_dir fails at startup instead of serving
// 404s. hasIndex reports whether index.html exists in it (a build that was
// never run); callers should warn about that.
func ResolveStaticDir(dir string) (abs string, hasIndex bool, err error) {
	abs, err = filepath.Abs(dir)
	if err != nil {
		return "", false, fmt.Errorf("resolve static_dir: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", false, fmt.Errorf("static_dir %q: %w", abs, err)
	}
	if !st.IsDir() {
		return "", false, fmt.Errorf("static_dir %q is not a directory", abs)
	}
	ist, err := os.Stat(filepath.Join(abs, "index.html"))
	return abs, err == nil && ist.Mode().IsRegular(), nil
}

// StaticHandler serves the built frontend from dir. Unknown paths without a
// file extension fall back to index.html (SPA routing); unknown paths with an
// extension (missing assets) return 404. The path is cleaned and resolved
// through os.DirFS, so it cannot escape dir.
func StaticHandler(dir string) http.Handler {
	fsys := os.DirFS(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if st, err := fs.Stat(fsys, name); err != nil || st.IsDir() {
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
		}
		setStaticHeaders(w, name)
		http.ServeFileFS(w, r, fsys, name)
	})
}

// setStaticHeaders sets cache headers. Hashed build assets are immutable; the
// entry points that must stay fresh (index.html, the service worker and the
// manifest) are always revalidated so a new deploy is picked up at once.
func setStaticHeaders(w http.ResponseWriter, name string) {
	h := w.Header()
	switch {
	case strings.HasPrefix(name, "assets/"):
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	case name == "sw.js":
		h.Set("Cache-Control", "no-cache, max-age=0")
		h.Set("Content-Type", "text/javascript; charset=utf-8")
		h.Set("Service-Worker-Allowed", "/")
	case name == "manifest.webmanifest":
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Type", "application/manifest+json")
	default:
		h.Set("Cache-Control", "no-cache")
	}
}
