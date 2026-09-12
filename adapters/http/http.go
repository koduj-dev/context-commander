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
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/contextpack"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/members"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
	"github.com/koduj-dev/context-commander/internal/secrets"
	"github.com/koduj-dev/context-commander/internal/security"
	"github.com/koduj-dev/context-commander/internal/skills"
)

// Deps bundles what every handler needs — constructed once in cmd/ccd and
// passed to NewHandler.
type Deps struct {
	Pool          *db.Pool
	Projects      *projects.Store
	Members       *members.Service
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
	Installation  *installation.Service
	ContextPack   *contextpack.Service
	// Security records refusals and Limiter bounds them. Both are safe to
	// leave nil — a nil *security.Recorder records nothing and a nil
	// *ratelimit.Limiter allows everything, which is what the CLI and the
	// tests that don't exercise either one get.
	Security       *security.Recorder
	Limiter        *ratelimit.Limiter
	SecurityEvents *security.Service
	// WebFS is the embedded web UI build (web.DistFS()), served as the
	// SPA fallback for any request under "/" that isn't "/v1/*" or
	// "/healthz". Left nil in dev mode (`serve --dev`), where the UI is
	// served separately by the Vite dev server instead — see
	// adapters/cli/serve.go.
	WebFS fs.FS
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

		// An unmatched path under /v1 must still get the API's uniform JSON
		// error envelope, not the SPA fallback below — chi sub-routers
		// otherwise inherit the parent's NotFoundHandler.
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
		})

		r.Get("/me", deps.getMe)
		r.Get("/me/projects", deps.listMyProjects)
		r.Get("/me/organizations", deps.listMyOrganizations)
		r.Post("/me/tokens", deps.issueToken)
		r.Get("/me/tokens", deps.listTokens)
		r.Delete("/me/tokens/{id}", deps.revokeToken)

		r.Post("/projects", deps.createProject)

		// The skill registry is a top-level resource, not a project one:
		// packages live at a scope of their own (docs/adr/0018) and an
		// installation-wide one belongs to no project. Only the *bindings*
		// below — which project uses which skill — are project-scoped.
		r.Route("/skills", func(r chi.Router) {
			r.Post("/", deps.publishSkill)
			r.Get("/", deps.searchSkills)
			r.Post("/promote", deps.promoteSkill)
			// Registered before "/{skillID}/...": chi prefers static
			// segments, so "versions" is not read as a skill id.
			r.Get("/versions/{skillVersionID}", deps.getSkillVersion)
			r.Get("/{skillID}/versions", deps.listSkillVersions)
		})

		r.Post("/organizations", deps.createOrganization)
		r.Post("/organizations/{id}/members", deps.addOrganizationMember)

		r.Route("/installation", func(r chi.Router) {
			r.Get("/admins", deps.listInstallationAdmins)
			r.Post("/users", deps.createInstallationUser)
			r.Post("/service-principals", deps.createServicePrincipal)
			r.Post("/principals/{id}/tokens", deps.issueCredentialFor)
			r.Get("/principals/{id}/tokens", deps.listCredentialsFor)
			r.Delete("/tokens/{id}", deps.revokeCredentialFor)
			r.Get("/security-events", deps.listSecurityEvents)
		})

		r.Route("/projects/{namespace}/{slug}", func(r chi.Router) {
			r.Use(deps.resolveProjectMiddleware)

			r.Get("/", deps.getProject)
			r.Post("/members", deps.addMember)
			r.Get("/members", deps.listMembers)

			r.Get("/artifacts", deps.listArtifacts)
			r.Get("/artifacts/stat", deps.statArtifact)
			r.Get("/artifacts/history", deps.historyArtifact)
			r.Get("/artifacts/content", deps.readArtifact)
			r.Put("/artifacts/content", deps.writeArtifact)
			r.Delete("/artifacts/content", deps.deleteArtifact)

			r.Get("/events", deps.listEvents)
			r.Get("/audit", deps.queryAudit)

			// The aggregated bootstrap read model. Mounted on the project
			// itself rather than under a domain, because it is the one
			// operation that deliberately belongs to no single domain.
			r.Get("/context", deps.getContextPack)

			r.Route("/handoffs", func(r chi.Router) {
				r.Post("/", deps.createHandoff)
				r.Get("/", deps.listHandoffs)
				// Registered before "/{handoffID}": chi matches static
				// segments ahead of parameters, so "current" is not
				// swallowed as a (malformed) handoff ID.
				r.Get("/current", deps.getCurrentHandoff)
				r.Get("/{handoffID}", deps.getHandoff)
				r.Post("/{handoffID}/claim", deps.claimHandoff)
				r.Post("/{handoffID}/close", deps.closeHandoff)
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

	if deps.WebFS != nil {
		r.NotFound(spaHandler(deps.WebFS).ServeHTTP)
	}

	return r
}
