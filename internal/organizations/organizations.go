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

type Store struct{}

func NewStore() *Store { return &Store{} }

func (s *Store) Create(ctx context.Context, tx pgx.Tx, name string) (Organization, error) {
	var o Organization
	err := tx.QueryRow(ctx,
		`INSERT INTO organizations (name) VALUES ($1) RETURNING id, name, created_at`,
		name,
	).Scan(&o.ID, &o.Name, &o.CreatedAt)
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

func (s *Store) AddMember(ctx context.Context, tx pgx.Tx, organizationID, userID uuid.UUID, role Role) (Membership, error) {
	var m Membership
	err := tx.QueryRow(ctx,
		`INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)
		 RETURNING organization_id, user_id, role, created_at`,
		organizationID, userID, role,
	).Scan(&m.OrganizationID, &m.UserID, &m.Role, &m.CreatedAt)
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
