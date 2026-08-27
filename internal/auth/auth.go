// Package auth defines AuthzService, the single source of truth for
// project-scoped authorization decisions — see
// docs/adr/0006-organization-vs-project-authorization.md.
//
// AuthzService.Can() must be the only place that decides ALLOW/DENY for
// project content. Row-Level Security (docs/adr/0002) mirrors this same
// decision at the database layer as defense-in-depth, but it is not a
// substitute for calling Can() — see docs/architecture-cookbook.md §4 and
// CONTRIBUTING.md rule 7.
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type AuthzService struct {
	pool     *db.Pool
	projects *projects.Store
}

func NewAuthzService(pool *db.Pool, projectStore *projects.Store) *AuthzService {
	return &AuthzService{pool: pool, projects: projectStore}
}

// Can decides whether principalID may exercise cap on projectID. Absence of
// membership is DENY, not an error — see the negative test suite in
// auth_integration_test.go (READER write -> DENY, cross-project -> DENY,
// no membership at all -> DENY).
func (a *AuthzService) Can(ctx context.Context, principalID uuid.UUID, projectID uuid.UUID, cap Capability) (bool, error) {
	var allowed bool

	err := db.WithPrincipal(ctx, a.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		membership, err := a.projects.GetMembership(ctx, tx, projectID, principalID)
		if errors.Is(err, projects.ErrNotFound) {
			allowed = false
			return nil
		}
		if err != nil {
			return fmt.Errorf("auth: resolve membership: %w", err)
		}
		allowed = RoleCan(membership.Role, cap)
		return nil
	})
	if err != nil {
		return false, err
	}
	return allowed, nil
}
