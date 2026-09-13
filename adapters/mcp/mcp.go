// Package mcp maps the internal domain onto MCP tools. It contains no
// business logic of its own — every tool handler resolves a principal (via
// internal/identity.Authenticate, the same chokepoint adapters/cli and
// adapters/http use) and calls straight into the same domain services those
// adapters call. See docs/adr/0016-mcp-sdk-choice.md for the SDK choice.
package mcp

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/contextpack"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/members"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
	"github.com/koduj-dev/context-commander/internal/secrets"
	"github.com/koduj-dev/context-commander/internal/security"
	"github.com/koduj-dev/context-commander/internal/skills"
)

// Deps bundles what every tool handler needs — constructed once in
// cmd/ccd and passed to NewHandler.
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
	// Both may be nil — a nil Recorder records nothing, a nil Limiter
	// allows everything (see adapters/http.Deps).
	Security *security.Recorder
	Limiter  *ratelimit.Limiter
}

type credKey struct{}

// NewHandler returns an http.Handler serving MCP over the Streamable HTTP
// transport at whatever path the caller mounts it on. Authentication
// happens once per HTTP request, outside the SDK: identity.Authenticate
// resolves the bearer token into a ClientCredential, which is then used to
// build a *sdkmcp.Server whose tools are closed over that one principal —
// every tool call within the resulting session acts as that principal,
// with no per-call re-declaration of identity to trust (docs/adr/0009).
func NewHandler(deps Deps) http.Handler {
	getServer := func(r *http.Request) *sdkmcp.Server {
		cred, ok := r.Context().Value(credKey{}).(identity.ClientCredential)
		if !ok {
			return nil // NewStreamableHTTPHandler serves 400 for a nil server; auth failures are already turned into 401 below, before this is ever reached.
		}
		return newServer(deps, cred.PrincipalID, cred.ID)
	}

	mcpHandler := sdkmcp.NewStreamableHTTPHandler(getServer, nil)
	return authenticateMiddleware(deps, mcpHandler)
}

func authenticateMiddleware(deps Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			http.Error(w, "missing or malformed Authorization: Bearer <token> header", http.StatusUnauthorized)
			return
		}

		// Same two bounds as the REST adapter (adapters/http/middleware.go):
		// the credential lookup is work an unauthenticated caller can
		// trigger, and an authenticated one is limited per credential. Both
		// transports share one Limiter, so an agent cannot double its
		// allowance by switching protocol.
		release, ok := deps.Limiter.AcquireAuth()
		if !ok {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many authentication attempts in flight; retry shortly", http.StatusServiceUnavailable)
			return
		}
		cred, err := identity.Authenticate(r.Context(), deps.Pool, token)
		release()
		if err != nil {
			var credentialID *uuid.UUID
			if id, _, parseErr := identity.ParseToken(token); parseErr == nil {
				credentialID = &id
			}
			deps.Security.Record(r.Context(), security.Event{
				Kind:         security.KindAuthenticationFailed,
				CredentialID: credentialID,
				Reason:       "presented token did not resolve to an active credential (mcp)",
			})
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		if !deps.Limiter.AllowCredential(cred.ID) {
			deps.Security.Record(r.Context(), security.Event{
				Kind:         security.KindRateLimited,
				PrincipalID:  &cred.PrincipalID,
				CredentialID: &cred.ID,
				Reason:       "credential exceeded its request rate limit (mcp)",
			})
			w.Header().Set("Retry-After", "1")
			http.Error(w, "this credential is making requests too quickly", http.StatusTooManyRequests)
			return
		}

		ctx := context.WithValue(r.Context(), credKey{}, cred)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newServer(deps Deps, principalID, clientID uuid.UUID) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "context-commander",
		Version: "0.1.0",
	}, &sdkmcp.ServerOptions{
		Instructions: "Context Commander: shared project context for AI agents. Start with " +
			"context.bootstrap on the project you were given — it returns your role, the handoff to " +
			"continue from, the effective skills' content, the artifacts to read, and an event cursor, " +
			"so you do not need to learn the domain model to take a first useful step. Then claim the " +
			"handoff (handoff.claim) before working, and close it or create a new one when you stop. " +
			"The per-domain tools (artifact.*, skill.*, handoff.*, event.list) cover everything past " +
			"that. Every call acts on behalf of the authenticated principal for this session.",
	})

	registerContextTools(server, deps, principalID, clientID)
	registerProjectTools(server, deps, principalID, clientID)
	registerArtifactTools(server, deps, principalID, clientID)
	registerEventTools(server, deps, principalID, clientID)
	registerOrganizationTools(server, deps, principalID, clientID)
	registerAuditTools(server, deps, principalID, clientID)
	registerHandoffTools(server, deps, principalID, clientID)
	registerHistoryTools(server, deps, principalID, clientID)
	registerSkillTools(server, deps, principalID, clientID)
	registerGateTools(server, deps, principalID, clientID)
	registerSecretTools(server, deps, principalID, clientID)
	registerMeTools(server, deps, principalID)
	registerInstallationTools(server, deps, principalID, clientID)

	return server
}
