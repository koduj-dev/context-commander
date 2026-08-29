package secrets

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
)

// Service is the authorization chokepoint for secrets (docs/adr/0014):
// every method checks CapSecretRead/CapSecretWrite itself before calling
// into backend. Audit writes happen in their own transaction, immediately
// after backend succeeds — NOT the single-transaction
// mutation+audit+outbox pattern internal/artifacts uses, because Backend
// is deliberately storage-agnostic (a Vault-backed implementation has no
// Postgres transaction for audit.Write to share). This is an accepted,
// documented gap versus that stricter atomicity: a crash between backend
// success and the audit write would lose that one audit row, never the
// secret value itself.
type Service struct {
	pool    *db.Pool
	authz   *auth.AuthzService
	backend Backend
}

func NewService(pool *db.Pool, authz *auth.AuthzService, backend Backend) *Service {
	return &Service{pool: pool, authz: authz, backend: backend}
}

func (s *Service) checkCapability(ctx context.Context, principalID, projectID uuid.UUID, cap auth.Capability) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, cap)
	if err != nil {
		return fmt.Errorf("secrets: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (s *Service) writeAudit(ctx context.Context, principalID, projectID uuid.UUID, action string) error {
	return db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID,
			Action: action, ResourceType: "secret", Result: audit.ResultAllow,
		})
	})
}

// Create stores plaintext under name for projectID (creating it, or
// rotating it if it already exists — LocalEncryptedBackend.Store upserts)
// and returns its Reference. The audit entry never carries the secret's
// name or value — audit.Entry has no free-form field for either, only the
// fixed allowlisted columns (docs/adr/0011).
func (s *Service) Create(ctx context.Context, principalID, projectID uuid.UUID, name string, plaintext []byte) (Reference, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSecretWrite); err != nil {
		return Reference{}, err
	}

	ref := NewProjectReference(projectID, name)
	if err := s.backend.Store(ctx, principalID, ref, plaintext); err != nil {
		return Reference{}, fmt.Errorf("secrets: create: %w", err)
	}
	if err := s.writeAudit(ctx, principalID, projectID, "secret.write"); err != nil {
		return Reference{}, err
	}
	return ref, nil
}

// Resolve returns the plaintext ref points to — the only method in this
// package that does. Every call that reaches backend.Fetch is audited,
// because this is the single highest-sensitivity operation in the system:
// it is the one path that ever hands back a live credential.
func (s *Service) Resolve(ctx context.Context, principalID, projectID uuid.UUID, ref Reference) ([]byte, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSecretRead); err != nil {
		return nil, err
	}
	if ref.Scope != "project" || ref.ScopeRef != projectID {
		return nil, ErrForbidden
	}

	plaintext, err := s.backend.Fetch(ctx, principalID, ref)
	if err != nil {
		return nil, fmt.Errorf("secrets: resolve: %w", err)
	}
	if err := s.writeAudit(ctx, principalID, projectID, "secret.resolve"); err != nil {
		return nil, err
	}
	return plaintext, nil
}

// List returns projectID's secret references — names only, never values.
func (s *Service) List(ctx context.Context, principalID, projectID uuid.UUID) ([]Reference, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSecretRead); err != nil {
		return nil, err
	}

	names, err := s.backend.List(ctx, principalID, "project", projectID)
	if err != nil {
		return nil, fmt.Errorf("secrets: list: %w", err)
	}
	out := make([]Reference, len(names))
	for i, name := range names {
		out[i] = NewProjectReference(projectID, name)
	}
	return out, nil
}

// Delete removes a secret value. Uses CapSecretWrite, same as Create —
// there is no separate CapSecretManage: a contributor trusted to rotate a
// secret's value is equally trusted to remove it.
func (s *Service) Delete(ctx context.Context, principalID, projectID uuid.UUID, name string) error {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSecretWrite); err != nil {
		return err
	}

	ref := NewProjectReference(projectID, name)
	if err := s.backend.Delete(ctx, principalID, ref); err != nil {
		return fmt.Errorf("secrets: delete: %w", err)
	}
	return s.writeAudit(ctx, principalID, projectID, "secret.delete")
}
