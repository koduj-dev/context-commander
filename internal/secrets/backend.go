package secrets

import (
	"context"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/paging"
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
	// List returns one page of the names stored under (scope, scopeRef) —
	// never values, ordered by name. Part of Backend, not a Postgres-only
	// query bolted onto Service, so a non-Postgres backend can serve
	// secret.list too; paging is in the interface for the same reason,
	// since a backend that cannot page in its own store would otherwise
	// have to fetch everything and slice in memory.
	//
	// Names are ordered rather than timestamped because that is the key a
	// secret store can be relied on to have: a Vault mount lists paths, not
	// creation times.
	List(ctx context.Context, principalID uuid.UUID, scope string, scopeRef uuid.UUID, opts paging.Options) (paging.Page[string], error)
}
