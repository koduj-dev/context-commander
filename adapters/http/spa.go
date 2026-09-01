package http

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaHandler serves the embedded web UI build, falling back to index.html
// for any path that isn't a real built asset — the standard SPA deep-link
// shape (e.g. /projects/acme/proj resolves to the app shell, then
// react-router-dom takes over client-side), same pattern as
// docker-commander's internal/api/spa.go.
func spaHandler(webFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" {
			if _, err := fs.Stat(webFS, strings.TrimPrefix(path, "/")); err != nil {
				// Not a real built asset — rewrite to "/" (not "/index.html":
				// net/http's file server special-cases a request path ending
				// in "/index.html" and 301-redirects it to "./", which would
				// just bounce right back here) so http.FileServer serves
				// dist/index.html and react-router-dom takes over client-side.
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
