package db

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5"
)

// LockClass namespaces an advisory lock. Postgres advisory locks live in one
// flat, cluster-wide space, so two unrelated call sites that hash to the same
// key would serialize against each other for no reason — or, worse, deadlock
// each other if they ever nest in opposite orders. Declaring every class here
// keeps that space reviewable in one place: this list is the registry.
//
// Locks are transaction-scoped (pg_advisory_xact_lock), so they are always
// released on commit or rollback — there is no unlock call to forget.
type LockClass int32

const (
	// LockClassSecretValue serializes the read-modify-write in
	// internal/secrets.LocalEncryptedBackend.Store, keyed by
	// (project_id, name).
	LockClassSecretValue LockClass = 1
	// LockClassProjectKey serializes creating a project's key version in
	// internal/crypto, keyed by project_id. Taken only on the path that
	// would insert — normal encryption of an existing key never blocks.
	LockClassProjectKey LockClass = 2
)

// AdvisoryXactLock takes a transaction-scoped exclusive advisory lock on
// (class, the hash of parts), blocking until it is available. Both the
// nesting order and the classes involved must be considered together: this
// package's only nesting today is LockClassSecretValue then
// LockClassProjectKey, never the reverse, which is what keeps it
// deadlock-free.
//
// parts is hashed rather than passed through because the lock key is a pair
// of int4s, not text. A hash collision between two different keys costs some
// needless serialization and nothing else — it can never produce a wrong
// result, because the lock only orders writers, it does not identify them.
func AdvisoryXactLock(ctx context.Context, tx pgx.Tx, class LockClass, parts ...string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::int4, $2::int4)`, int32(class), advisoryKey(parts)); err != nil {
		return fmt.Errorf("db: advisory lock (class %d): %w", class, err)
	}
	return nil
}

// advisoryKey hashes parts into the lock's second int4. Parts are
// NUL-separated so ("ab", "c") and ("a", "bc") cannot produce the same
// digest.
func advisoryKey(parts []string) int32 {
	h := fnv.New32a()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return int32(h.Sum32()) //nolint:gosec // a wrapped hash is still a valid, stable lock key
}
