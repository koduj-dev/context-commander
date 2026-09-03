package installation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
)

// ErrForbidden is returned when a caller lacks the requested Capability —
// see internal/organizations.ErrForbidden for the same reasoning (distinct
// from ErrNotFound so an adapter can choose whether to leak existence).
var ErrForbidden = errors.New("installation: forbidden")

// Service is installation-admin's authorization chokepoint (docs/adr/0014):
// every method checks admin status itself rather than trusting a caller to
// have checked already. Every method takes both principalID (to set RLS
// session context) and callerUserID (installation_admins is keyed by User,
// never Principal — a ServicePrincipal can never be an installation admin,
// docs/adr/0017) — the same shape internal/organizations.Service uses and
// for the same reason: the two are not derivable from each other without a
// lookup the caller already has (adapters/http/organizations.go's
// resolveCallerUserID, reused as-is by the installation adapters).
type Service struct {
	pool     *db.Pool
	store    *Store
	identity *identity.Store
}

func NewService(pool *db.Pool, store *Store, identityStore *identity.Store) *Service {
	return &Service{pool: pool, store: store, identity: identityStore}
}

func (s *Service) checkCapability(ctx context.Context, principalID, callerUserID uuid.UUID, cap Capability) error {
	var allowed bool
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		isAdmin, err := s.store.IsAdmin(ctx, tx, callerUserID)
		if err != nil {
			return err
		}
		allowed = isAdmin && RoleCan(RoleAdmin, cap)
		return nil
	})
	if err != nil {
		return fmt.Errorf("installation: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// ListAdmins returns every installation admin, after checking callerUserID
// holds CapAdminRead (granted to any admin — this is a read of the admin
// list itself, not an escalation path: no method in this package lets one
// admin grant another over HTTP/MCP, see docs/adr/0017).
func (s *Service) ListAdmins(ctx context.Context, principalID, callerUserID uuid.UUID) ([]Admin, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapAdminRead); err != nil {
		return nil, err
	}
	var admins []Admin
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		admins, err = s.store.List(ctx, tx)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("installation: list admins: %w", err)
	}
	return admins, nil
}

// CreateUser creates a User and its Principal after checking callerUserID
// holds CapUserCreate — the HTTP/MCP-reachable counterpart to `ccd user
// create`, which internal/identity's own doc comment already anticipated
// (docs/adr/0009's "a future adapters/http admin endpoint").
func (s *Service) CreateUser(ctx context.Context, principalID, callerUserID uuid.UUID, email, displayName string) (identity.User, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapUserCreate); err != nil {
		return identity.User{}, err
	}
	var u identity.User
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		u, err = s.identity.CreateUser(ctx, tx, email, displayName)
		if err != nil {
			return err
		}
		_, err = s.identity.CreatePrincipalForUser(ctx, tx, u.ID)
		return err
	})
	if err != nil {
		return identity.User{}, fmt.Errorf("installation: create user: %w", err)
	}
	return u, nil
}

// CreateServicePrincipal registers a ServicePrincipal (an agent) and its
// Principal after checking callerUserID holds CapServicePrincipalCreate —
// internal/identity.Store.CreateServicePrincipal/CreatePrincipalForService
// existed with zero adapter callers before this method (NEXT.md's audit
// note: "agents cannot be registered through any interface today, not even
// CLI").
func (s *Service) CreateServicePrincipal(ctx context.Context, principalID, callerUserID uuid.UUID, name, description string) (identity.ServicePrincipal, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapServicePrincipalCreate); err != nil {
		return identity.ServicePrincipal{}, err
	}
	var sp identity.ServicePrincipal
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		sp, err = s.identity.CreateServicePrincipal(ctx, tx, name, description)
		if err != nil {
			return err
		}
		_, err = s.identity.CreatePrincipalForService(ctx, tx, sp.ID)
		return err
	})
	if err != nil {
		return identity.ServicePrincipal{}, fmt.Errorf("installation: create service principal: %w", err)
	}
	return sp, nil
}

// IssueCredentialFor issues a PAT for targetPrincipalID — any principal,
// not just the caller — after checking callerUserID holds
// CapCredentialManage. Distinct from adapters/http/tokens.go's self-only
// issueToken: that one never accepts a principal id from the request body
// at all (internal/identity has no authz layer of its own, ADR-0009's
// consequences); this is the deliberately separate, capability-gated
// feature that comment calls out as unbuilt.
func (s *Service) IssueCredentialFor(
	ctx context.Context, principalID, callerUserID, targetPrincipalID uuid.UUID, clientType string, expiresAt *time.Time,
) (string, identity.ClientCredential, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapCredentialManage); err != nil {
		return "", identity.ClientCredential{}, err
	}
	var token string
	var cred identity.ClientCredential
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		token, cred, err = s.identity.IssueCredential(ctx, tx, targetPrincipalID, clientType, expiresAt)
		return err
	})
	if err != nil {
		return "", identity.ClientCredential{}, fmt.Errorf("installation: issue credential: %w", err)
	}
	return token, cred, nil
}

// ListCredentialsFor lists every PAT belonging to targetPrincipalID, after
// checking callerUserID holds CapCredentialManage.
func (s *Service) ListCredentialsFor(ctx context.Context, principalID, callerUserID, targetPrincipalID uuid.UUID) ([]identity.ClientCredential, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapCredentialManage); err != nil {
		return nil, err
	}
	var creds []identity.ClientCredential
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		creds, err = s.identity.ListCredentialsForPrincipal(ctx, tx, targetPrincipalID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("installation: list credentials: %w", err)
	}
	return creds, nil
}

// RevokeCredentialFor revokes credentialID regardless of which principal it
// belongs to, after checking callerUserID holds CapCredentialManage — the
// admin counterpart to adapters/http/tokens.go's revokeToken, which only
// ever revokes the caller's own credential (ownership enforced in its
// WHERE clause). Uses identity.Store.RevokeCredentialByID, which has no
// such ownership restriction — that is exactly the point of this method.
func (s *Service) RevokeCredentialFor(ctx context.Context, principalID, callerUserID, credentialID uuid.UUID) error {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapCredentialManage); err != nil {
		return err
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return s.identity.RevokeCredentialByID(ctx, tx, credentialID)
	})
	if err != nil {
		return fmt.Errorf("installation: revoke credential: %w", err)
	}
	return nil
}
