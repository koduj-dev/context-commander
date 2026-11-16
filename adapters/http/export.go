package http

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// lazyDownload sets the download headers on the first byte, so a refusal
// that comes before any content can still be an ordinary JSON error.
type lazyDownload struct {
	w        http.ResponseWriter
	filename string
	started  bool
}

func (l *lazyDownload) Write(p []byte) (int, error) {
	if !l.started {
		l.started = true
		h := l.w.Header()
		h.Set("Content-Type", "application/gzip")
		h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, l.filename))
		h.Set("Cache-Control", "no-store")
		l.w.WriteHeader(http.StatusOK)
	}
	return l.w.Write(p)
}

// exportProject is GET /projects/{ns}/{slug}/export: the project as a
// tar.gz archive (internal/portability, docs/adr/0035), streamed. OWNER and
// ADMIN only. A failure after the first byte cannot change the status any
// more; the archive then has no manifest, and import refuses it.
func (d Deps) exportProject(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	name := strings.NewReplacer("/", "-").Replace(project.Slug) + "-" + time.Now().UTC().Format("20060102-150405") + ".ccproject.tar.gz"
	out := &lazyDownload{w: w, filename: name}
	if _, err := d.Portability.Export(r.Context(), principalIDFrom(r.Context()), project.ID, out); err != nil {
		if !out.started {
			writeDomainError(w, r, err)
			return
		}
		slog.ErrorContext(r.Context(), "project export failed after the download started; the archive is incomplete",
			"project", project.ID, "err", err)
	}
}
