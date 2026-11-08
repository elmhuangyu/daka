package api

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

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
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFileFS(w, r, fsys, name)
	})
}
