// Package web embeds the built frontend (web/dist, produced by `make ui` /
// `npm run build`) into the ccd binary — see adapters/cli/serve.go's `serve`
// command, which mounts DistFS() as the HTTP server's SPA fallback unless
// --dev is set. A placeholder dist/index.html is committed so `go build`
// never depends on Node having run first.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded build rooted at dist/, so callers can serve
// e.g. "index.html" and "assets/..." directly instead of "dist/index.html".
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
