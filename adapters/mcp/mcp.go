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
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// Deps bundles what every tool handler needs — constructed once in
// cmd/ccd and passed to NewHandler.
type Deps struct {
	Pool      *db.Pool
	Projects  *projects.Store
	Artifacts *artifacts.Service
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
		cred, err := identity.Authenticate(r.Context(), deps.Pool, token)
		if err != nil {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
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
		Instructions: "Context Commander: shared project context for AI agents. Resolve or " +
			"create a project first, then read/write artifacts within it. Every call acts on " +
			"behalf of the authenticated principal for this session.",
	})

	registerProjectTools(server, deps, principalID, clientID)
	registerArtifactTools(server, deps, principalID, clientID)

	return server
}
