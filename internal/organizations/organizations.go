// Package organizations defines Organization and OrganizationMembership —
// see docs/adr/0006-organization-vs-project-authorization.md.
//
// Organization roles govern organization administration (namespace
// administration, org-scope skills/policies, project creation inside the
// org's namespace) — they never participate in project-content
// authorization decisions. See internal/auth for the actual authorization
// boundary.
package organizations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Role string

const (
	RoleOwner  Role = "OWNER"
	RoleAdmin  Role = "ADMIN"
	RoleMember Role = "MEMBER"
)

type Organization struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

type Membership struct {
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	Role           Role
	CreatedAt      time.Time
}

var ErrNotFound = errors.New("organizations: not found")

// ListMembers returns every member of organizationID, oldest first — the
// counterpart of projects.Store.ListMembers, which existed while this did
// not: organizations could be added to and never enumerated, so the only
// way to answer "who is in this organization" was SQL.
func (s *Store) ListMembers(ctx context.Context, tx pgx.Tx, organizationID uuid.UUID) ([]Membership, error) {
	rows, err := tx.Query(ctx,
		`SELECT organization_id, user_id, role, created_at FROM organization_members
		 WHERE organization_id = $1 ORDER BY created_at ASC`,
		organizationID,
	)
	if err != nil {
		return nil, fmt.Errorf("organizations: list members: %w", err)
	}
	defer rows.Close()

	var out []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.OrganizationID, &m.UserID, &m.Role, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("organizations: list members: scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organizations: list members: %w", err)
	}
	return out, nil
}

type Store struct{}

func NewStore() *Store { return &Store{} }

// Create deliberately does not use INSERT ... RETURNING — see
// projects.Store.Create for the same choice and why: with
// organizations_select requiring cc_has_org_access
// (migrations/000005_organizations_rls.up.sql) and a brand-new
// organization having zero members yet, RETURNING would need to
// read the just-inserted row back through that same SELECT policy and
// fail with "new row violates row-level security policy," even though
// the INSERT itself (governed only by organizations_insert's
// unconditional WITH CHECK (true)) succeeds. Building the struct
// client-side and using a plain Exec sidesteps the read-back entirely.
func (s *Store) Create(ctx context.Context, tx pgx.Tx, name string) (Organization, error) {
	o := Organization{ID: uuid.New(), Name: name, CreatedAt: time.Now().UTC()}
	_, err := tx.Exec(ctx,
		`INSERT INTO organizations (id, name, created_at) VALUES ($1, $2, $3)`,
		o.ID, o.Name, o.CreatedAt,
	)
	if err != nil {
		return Organization{}, fmt.Errorf("organizations: create: %w", err)
	}
	return o, nil
}

func (s *Store) Get(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Organization, error) {
	var o Organization
	err := tx.QueryRow(ctx,
		`SELECT id, name, created_at FROM organizations WHERE id = $1`, id,
	).Scan(&o.ID, &o.Name, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("organizations: get: %w", err)
	}
	return o, nil
}

// AddMember avoids RETURNING for the same reason Create does: the very
// first member of a brand-new organization would otherwise need to pass
// organization_members_select's cc_has_org_access check before that
// membership row exists to grant it.
func (s *Store) AddMember(ctx context.Context, tx pgx.Tx, organizationID, userID uuid.UUID, role Role) (Membership, error) {
	m := Membership{OrganizationID: organizationID, UserID: userID, Role: role, CreatedAt: time.Now().UTC()}
	_, err := tx.Exec(ctx,
		`INSERT INTO organization_members (organization_id, user_id, role, created_at) VALUES ($1, $2, $3, $4)`,
		m.OrganizationID, m.UserID, m.Role, m.CreatedAt,
	)
	if err != nil {
		return Membership{}, fmt.Errorf("organizations: add member: %w", err)
	}
	return m, nil
}

func (s *Store) GetMembership(ctx context.Context, tx pgx.Tx, organizationID, userID uuid.UUID) (Membership, error) {
	var m Membership
	err := tx.QueryRow(ctx,
		`SELECT organization_id, user_id, role, created_at FROM organization_members
		 WHERE organization_id = $1 AND user_id = $2`,
		organizationID, userID,
	).Scan(&m.OrganizationID, &m.UserID, &m.Role, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	if err != nil {
		return Membership{}, fmt.Errorf("organizations: get membership: %w", err)
	}
	return m, nil
}

// OrganizationMembership is one row of ListForUser — an organization userID
// belongs to, plus their role on it.
type OrganizationMembership struct {
	Organization Organization
	Role         Role
}

// ListForUser lists every organization userID is a member of, driven by
// organization_members_user_id_idx (migrations/000016) — the web UI's
// dashboard org picker is the only caller today.
func (s *Store) ListForUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID) ([]OrganizationMembership, error) {
	rows, err := tx.Query(ctx,
		`SELECT o.id, o.name, o.created_at, om.role
		 FROM organization_members om
		 JOIN organizations o ON o.id = om.organization_id
		 WHERE om.user_id = $1
		 ORDER BY o.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("organizations: list for user: %w", err)
	}
	defer rows.Close()

	var out []OrganizationMembership
	for rows.Next() {
		var m OrganizationMembership
		if err := rows.Scan(&m.Organization.ID, &m.Organization.Name, &m.Organization.CreatedAt, &m.Role); err != nil {
			return nil, fmt.Errorf("organizations: list for user: scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organizations: list for user: %w", err)
	}
	return out, nil
}
