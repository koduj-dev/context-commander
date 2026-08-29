package secrets

import (
	"context"

	"github.com/google/uuid"
)

// Backend stores and retrieves the plaintext a Reference points to.
// Deliberately storage-agnostic: no pgx.Tx or other Postgres type appears
// here, because a future backend (HashiCorp Vault, a cloud secret manager)
// has no Postgres transaction to participate in — see
// docs/architecture-cookbook.md §15. principalID is still threaded through
// every method because LocalEncryptedBackend (this package's only v1
// implementation) needs it to set RLS session context the same way every
// other repository in this codebase does.
type Backend interface {
	Store(ctx context.Context, principalID uuid.UUID, ref Reference, plaintext []byte) error
	Fetch(ctx context.Context, principalID uuid.UUID, ref Reference) ([]byte, error)
	Delete(ctx context.Context, principalID uuid.UUID, ref Reference) error
	// List returns the names stored under (scope, scopeRef) — never
	// values. Part of Backend, not a Postgres-only query bolted onto
	// Service, so a non-Postgres backend can serve secret.list too.
	List(ctx context.Context, principalID uuid.UUID, scope string, scopeRef uuid.UUID) ([]string, error)
}
