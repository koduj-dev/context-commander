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

// DenyRecorder is told about every refusal Can makes. It exists as a hook
// rather than a direct call because internal/audit and internal/security
// both import this package (for Capability/CapAuditRead), so this package
// cannot import them back — the dependency has to be inverted.
//
// Optional: a nil recorder means refusals are decided exactly as before and
// simply not written down, which is what every test and every CLI command
// that constructs an AuthzService gets. Only the server wires one in.
type DenyRecorder interface {
	RecordDeny(ctx context.Context, principalID, projectID uuid.UUID, capability Capability, reason string)
}

type AuthzService struct {
	pool     *db.Pool
	projects *projects.Store
	onDeny   DenyRecorder
}

func NewAuthzService(pool *db.Pool, projectStore *projects.Store) *AuthzService {
	return &AuthzService{pool: pool, projects: projectStore}
}

// SetDenyRecorder attaches a recorder after construction, so the twenty-odd
// existing callers of NewAuthzService keep compiling and only the server
// opts in. Call it once, during wiring, before serving.
func (a *AuthzService) SetDenyRecorder(r DenyRecorder) { a.onDeny = r }

// Can decides whether principalID may exercise cap on projectID. Absence of
// membership is DENY, not an error — see the negative test suite in
// auth_integration_test.go (READER write -> DENY, cross-project -> DENY,
// no membership at all -> DENY).
func (a *AuthzService) Can(ctx context.Context, principalID uuid.UUID, projectID uuid.UUID, cap Capability) (bool, error) {
	var allowed bool

	// reason distinguishes the two ways a caller is refused. They call for
	// different responses from whoever reads the record later: "not a
	// member" is someone reaching for a project that isn't theirs, while
	// "role lacks capability" is a legitimate member hitting the edge of
	// what their role allows — routine, and noise if mistaken for the first.
	var reason string
	err := db.WithPrincipal(ctx, a.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		membership, err := a.projects.GetMembership(ctx, tx, projectID, principalID)
		if errors.Is(err, projects.ErrNotFound) {
			allowed = false
			reason = "principal is not a member of this project"
			return nil
		}
		if err != nil {
			return fmt.Errorf("auth: resolve membership: %w", err)
		}
		allowed = RoleCan(membership.Role, cap)
		if !allowed {
			reason = "role " + string(membership.Role) + " does not hold this capability"
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	// Recorded here because this is the only place that decides ALLOW/DENY
	// for project content (docs/adr/0006) — putting it in each domain's own
	// checkCapability would mean nine places to forget it in, and a tenth
	// domain arriving without it.
	if !allowed && a.onDeny != nil {
		a.onDeny.RecordDeny(ctx, principalID, projectID, cap, reason)
	}
	return allowed, nil
}
