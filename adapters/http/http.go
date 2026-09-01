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
	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/secrets"
	"github.com/koduj-dev/context-commander/internal/skills"
)

// Deps bundles what every handler needs — constructed once in cmd/ccd and
// passed to NewHandler.
type Deps struct {
	Pool          *db.Pool
	Projects      *projects.Store
	Authz         *auth.AuthzService
	Artifacts     *artifacts.Service
	Events        *events.Service
	Organizations *organizations.Service
	Audit         *audit.Service
	Handoffs      *handoffs.Service
	History       *history.Service
	Skills        *skills.Service
	Gates         *gates.AdminService
	Secrets       *secrets.Service
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

		r.Post("/organizations", deps.createOrganization)
		r.Post("/organizations/{id}/members", deps.addOrganizationMember)

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

			r.Get("/events", deps.listEvents)
			r.Get("/audit", deps.queryAudit)

			r.Route("/handoffs", func(r chi.Router) {
				r.Post("/", deps.createHandoff)
				r.Get("/", deps.listHandoffs)
				r.Get("/{handoffID}", deps.getHandoff)
			})

			r.Route("/conversations", func(r chi.Router) {
				r.Post("/", deps.createConversation)
				r.Route("/{conversationID}", func(r chi.Router) {
					r.Post("/sessions", deps.createExternalSession)
					r.Post("/objects", deps.appendHistoryObject)
					r.Get("/objects", deps.listHistoryObjects)
					r.Get("/lineage", deps.getLineage)
				})
			})

			r.Post("/skills", deps.publishSkill)
			r.Post("/skills/install", deps.installSkill)
			r.Post("/skills/override", deps.overrideSkill)
			r.Post("/skills/pin", deps.pinSkillBinding)
			r.Post("/skills/unpin", deps.unpinSkillBinding)
			r.Get("/skills/effective", deps.listEffectiveSkills)

			r.Get("/gates/bindings", deps.listGateBindings)
			r.Post("/gates/bindings", deps.bindGate)
			r.Delete("/gates/bindings", deps.unbindGate)

			r.Post("/secrets", deps.createSecret)
			r.Get("/secrets", deps.listSecrets)
			r.Delete("/secrets/{name}", deps.deleteSecret)
		})
	})

	return r
}
