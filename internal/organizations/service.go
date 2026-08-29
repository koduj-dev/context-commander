package organizations

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

// ErrForbidden is returned when a caller lacks the requested Capability on
// an organization — see internal/artifacts.ErrForbidden for the same
// reasoning (distinct from ErrNotFound so an adapter can choose whether to
// leak existence).
var ErrForbidden = errors.New("organizations: forbidden")

// Service is organizations' authorization chokepoint, mirroring every
// other domain's Service (docs/adr/0014): every method checks membership
// itself rather than trusting a caller to have checked already. It takes
// both a principalID (to set RLS session context — organizations still
// live behind cc_has_org_access, migrations/000005) and a userID
// (organization_members is keyed by User, never Principal — docs/adr/0006:
// service principals are never organization members) because the two are
// not derivable from each other without a lookup the caller already has.
type Service struct {
	store *Store
	pool  *db.Pool
}

func NewService(pool *db.Pool, store *Store) *Service {
	return &Service{store: store, pool: pool}
}

func (s *Service) checkCapability(ctx context.Context, principalID, userID, organizationID uuid.UUID, cap Capability) error {
	var allowed bool
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		m, err := s.store.GetMembership(ctx, tx, organizationID, userID)
		if errors.Is(err, ErrNotFound) {
			allowed = false
			return nil
		}
		if err != nil {
			return err
		}
		allowed = RoleCan(m.Role, cap)
		return nil
	})
	if err != nil {
		return fmt.Errorf("organizations: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// Create creates an organization and adds (principalID, userID) as its
// first OWNER, both in one transaction — mirrors
// internal/projects.Store.CreateForOwner: an organization committed
// without an immediate self-membership insert would be permanently
// inaccessible to everyone, itself included, since
// organizations_select/organization_members_select both require
// cc_has_org_access (migrations/000005_organizations_rls.up.sql).
func (s *Service) Create(ctx context.Context, principalID, userID uuid.UUID, name string) (Organization, error) {
	var org Organization
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		org, err = s.store.Create(ctx, tx, name)
		if err != nil {
			return err
		}
		_, err = s.store.AddMember(ctx, tx, org.ID, userID, RoleOwner)
		return err
	})
	if err != nil {
		return Organization{}, fmt.Errorf("organizations: create: %w", err)
	}
	return org, nil
}

// Get returns organizationID after checking (principalID, userID) holds
// CapOrgRead on it.
func (s *Service) Get(ctx context.Context, principalID, userID, organizationID uuid.UUID) (Organization, error) {
	if err := s.checkCapability(ctx, principalID, userID, organizationID, CapOrgRead); err != nil {
		return Organization{}, err
	}
	var org Organization
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		org, err = s.store.Get(ctx, tx, organizationID)
		return err
	})
	if err != nil {
		return Organization{}, fmt.Errorf("organizations: get: %w", err)
	}
	return org, nil
}

// AddMember adds newUserID to organizationID with role, after checking the
// caller (principalID, callerUserID) holds CapOrgManageMembers on it —
// OWNER or ADMIN, per roleCapabilities in capability.go.
func (s *Service) AddMember(ctx context.Context, principalID, callerUserID, organizationID, newUserID uuid.UUID, role Role) (Membership, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, organizationID, CapOrgManageMembers); err != nil {
		return Membership{}, err
	}
	var m Membership
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		m, err = s.store.AddMember(ctx, tx, organizationID, newUserID, role)
		return err
	})
	if err != nil {
		return Membership{}, fmt.Errorf("organizations: add member: %w", err)
	}
	return m, nil
}
