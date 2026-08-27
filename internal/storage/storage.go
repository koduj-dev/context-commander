// Package storage defines the ObjectStore interface implemented by the
// filesystem and s3 backends — docs/architecture-cookbook.md §8.1.
//
// ObjectStore stores and retrieves opaque bytes by key. It knows nothing
// about encryption — callers (internal/artifacts, Phase 3) encrypt via
// internal/crypto.Provider *before* calling Put and decrypt *after*
// calling Get. Both backends go through the identical caller-side crypto
// path, so a bug in one is a bug in the encryption code, not something a
// backend could accidentally skip (see docs/adr's "filesystem dev backend
// must not bypass the crypto layer" pitfall).
package storage

import (
	"context"
	"errors"
	"io"
)

var (
	ErrNotFound   = errors.New("storage: object not found")
	ErrInvalidKey = errors.New("storage: invalid object key")
)

type ObjectStore interface {
	// Put stores the content of r (exactly size bytes) under key, which
	// must not already exist with different content — see each backend's
	// Put doc for its exact overwrite semantics. Returns an opaque etag
	// useful for logging/debugging, not a content hash to rely on (see
	// docs/adr/0005 — a storage backend's etag is not a substitute for
	// ArtifactVersion.content_hash, which is computed over plaintext before
	// encryption).
	Put(ctx context.Context, key string, r io.Reader, size int64) (etag string, err error)

	// Get returns the stored content for key. The caller must Close it.
	// Returns ErrNotFound if key does not exist.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes key. Deleting a key that doesn't exist is not an
	// error — see docs/adr/0013's tombstone-then-cleanup lifecycle, which
	// calls this only after the owning DB row is already gone, so
	// "already gone" here is a normal, expected outcome, not a bug.
	Delete(ctx context.Context, key string) error
}
