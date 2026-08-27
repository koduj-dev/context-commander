// Package http maps the internal domain onto a REST API. It contains no
// business logic of its own — every handler resolves a principal (via
// internal/identity.Authenticate) and a project, then calls straight into
// the same domain services adapters/cli and adapters/mcp use.
//
// Artifact logical paths contain "/", so they travel as a query parameter
// (?path=...), never as a chi URL segment — the same lesson (object refs
// with "/" or ":" break path-segment routing) applies here as it did for
// docker-commander's image/container refs.
package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// Deps bundles what every handler needs — constructed once in cmd/ccd and
// passed to NewHandler.
type Deps struct {
	Pool      *db.Pool
	Projects  *projects.Store
	Artifacts *artifacts.Service
}

func NewHandler(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// No middleware.RealIP: it trusts X-Forwarded-For/X-Real-IP unconditionally,
	// which is spoofable unless a reverse proxy is known to set it and strip
	// client-supplied copies first — not something this handler can assume
	// (GHSA-3fxj-6jh8-hvhx). Add it only alongside an explicit trusted-proxy config.
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Route("/v1", func(r chi.Router) {
		r.Use(deps.authenticateMiddleware)

		r.Post("/projects", deps.createProject)
		r.Route("/projects/{namespace}/{slug}", func(r chi.Router) {
			r.Use(deps.resolveProjectMiddleware)

			r.Get("/", deps.getProject)
			r.Post("/members", deps.addMember)

			r.Get("/artifacts", deps.listArtifacts)
			r.Get("/artifacts/stat", deps.statArtifact)
			r.Get("/artifacts/history", deps.historyArtifact)
			r.Get("/artifacts/content", deps.readArtifact)
			r.Put("/artifacts/content", deps.writeArtifact)
			r.Delete("/artifacts/content", deps.deleteArtifact)
		})
	})

	return r
}
