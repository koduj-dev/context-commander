// Package installation defines the installation-admin authorization tier —
// see docs/adr/0017-installation-admin-tier.md.
//
// Installation-admin rights govern instance-wide administration: creating
// users, registering ServicePrincipals (agents), managing any principal's
// credentials, and writing installation-scope skill/gate bindings. They
// never participate in project-content or organization-content
// authorization decisions — see internal/auth and internal/organizations
// for those boundaries, and docs/adr/0006 for why these types stay
// separate rather than merging into one "super capability" enum.
package installation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Role string

// RoleAdmin is the only role that exists today — CHECK-constrained at the
// schema level (migrations/000017) rather than left unconstrained, so a
// future narrower tier (e.g. a read-only AUDITOR) is an INSERT/UPDATE-time
// decision, not a migration.
const RoleAdmin Role = "ADMIN"

// Admin is one installation_admins row — a User (never a ServicePrincipal,
// same rationale as docs/adr/0004 gives for organization membership, more
// load-bearing here: an installation admin can create arbitrary users and
// register arbitrary service principals, so "an agent could hold this"
// would put the whole instance's identity model behind one compromised
// agent).
type Admin struct {
	UserID    uuid.UUID
	Role      Role
	GrantedBy *uuid.UUID
	GrantedAt time.Time
}

var (
	ErrNotFound     = errors.New("installation: not found")
	ErrAlreadyAdmin = errors.New("installation: user is already an installation admin")
)

// Store is the Postgres-backed installation_admins repository — no RLS
// (migrations/000017), same trust model as internal/identity's tables:
// every caller reaches it through Service's capability check, or through
// `ccd installation grant-admin`'s operator/--database-url-only CLI path.
type Store struct{}

func NewStore() *Store { return &Store{} }

// IsAdmin is the single query every capability check in this package (and
// internal/skills/internal/gates' ScopeInstallation handling) ultimately
// resolves to.
func (s *Store) IsAdmin(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM installation_admins WHERE user_id = $1)`, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("installation: check admin: %w", err)
	}
	return exists, nil
}

// Grant adds userID as an installation admin. grantedBy is nil for a
// bootstrap grant (docs/adr/0017: the first user `ccd bootstrap` creates
// becomes an admin automatically — the only way anyone can ever become one
// without direct database access) and set for every later grant issued via
// `ccd installation grant-admin`.
func (s *Store) Grant(ctx context.Context, tx pgx.Tx, userID uuid.UUID, grantedBy *uuid.UUID) (Admin, error) {
	a := Admin{UserID: userID, Role: RoleAdmin, GrantedBy: grantedBy, GrantedAt: time.Now().UTC()}
	tag, err := tx.Exec(ctx,
		`INSERT INTO installation_admins (user_id, role, granted_by, granted_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (user_id) DO NOTHING`,
		a.UserID, a.Role, a.GrantedBy, a.GrantedAt,
	)
	if err != nil {
		return Admin{}, fmt.Errorf("installation: grant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Admin{}, ErrAlreadyAdmin
	}
	return a, nil
}

// Revoke removes userID's installation-admin rights.
func (s *Store) Revoke(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM installation_admins WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("installation: revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns every installation admin, oldest grant first.
func (s *Store) List(ctx context.Context, tx pgx.Tx) ([]Admin, error) {
	rows, err := tx.Query(ctx, `SELECT user_id, role, granted_by, granted_at FROM installation_admins ORDER BY granted_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("installation: list: %w", err)
	}
	defer rows.Close()

	var out []Admin
	for rows.Next() {
		var a Admin
		if err := rows.Scan(&a.UserID, &a.Role, &a.GrantedBy, &a.GrantedAt); err != nil {
			return nil, fmt.Errorf("installation: list: scan: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("installation: list: %w", err)
	}
	return out, nil
}
