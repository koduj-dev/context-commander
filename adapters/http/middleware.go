package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// authenticateMiddleware resolves the caller's Principal from a bearer PAT,
// via the same identity.Authenticate every adapter uses (docs/adr/0009) —
// this package never parses or verifies a token itself, and nothing about
// identity is trusted from the client beyond the token.
func (d Deps) authenticateMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or malformed Authorization: Bearer <token> header")
			return
		}

		cred, err := identity.Authenticate(r.Context(), d.Pool, token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyPrincipalID, cred.PrincipalID)
		ctx = context.WithValue(ctx, ctxKeyClientID, cred.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// resolveProjectMiddleware resolves {namespace}/{slug} into a Project and
// puts it in the request context. Resolve itself is RLS-backed
// (internal/projects) — a non-member gets ErrNotFound here, not a 403, so
// project existence isn't leaked to outsiders.
func (d Deps) resolveProjectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := chi.URLParam(r, "namespace") + "/" + chi.URLParam(r, "slug")
		principalID := principalIDFrom(r.Context())

		var project projects.Project
		err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			var err error
			project, err = d.Projects.Resolve(r.Context(), tx, path)
			return err
		})
		if err != nil {
			writeDomainError(w, err)
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyProject, project)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
