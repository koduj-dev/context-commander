package secrets_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/secrets"
)

// secretRow reads the stored row's id/version directly, as cc_owner —
// neither is exposed through the Service, and the point of these tests is
// what ends up on disk.
func (f *fixture) secretRow(t *testing.T, projectID uuid.UUID, name string) (id uuid.UUID, version int) {
	t.Helper()
	err := db.WithoutPrincipal(context.Background(), f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT id, version FROM secret_values WHERE project_id = $1 AND name = $2`,
			projectID, name,
		).Scan(&id, &version)
	})
	if err != nil {
		t.Fatalf("read secret row: %v", err)
	}
	return id, version
}

// storeConcurrently fires n Create calls for the same name at once, released
// from a common barrier so they genuinely overlap, and returns their errors.
func storeConcurrently(t *testing.T, svc *secrets.Service, principalID, projectID uuid.UUID, name string, n int) []error {
	t.Helper()
	ctx := context.Background()

	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, errs[i] = svc.Create(ctx, principalID, projectID, name, fmt.Appendf(nil, "value-%d", i))
		}()
	}
	start.Done()
	done.Wait()
	return errs
}

// TestConcurrentRotationDoesNotLoseAWrite is the regression test for the
// read-modify-write race in LocalEncryptedBackend.Store: it read the current
// version, then upserted version+1, with nothing serializing the two steps.
// N concurrent rotations all read version 1 and all wrote version 2, so
// N-1 values were silently dropped. Serialized, the stored version must
// have advanced once per call.
func TestConcurrentRotationDoesNotLoseAWrite(t *testing.T) {
	f := newFixture(t)
	principalID, projectID := f.seedProject(t, "OWNER")
	ctx := context.Background()

	if _, err := f.svc.Create(ctx, principalID, projectID, "api-token", []byte("initial")); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	_, version := f.secretRow(t, projectID, "api-token")
	if version != 1 {
		t.Fatalf("seeded version = %d, want 1", version)
	}

	const rotations = 8
	for i, err := range storeConcurrently(t, f.svc, principalID, projectID, "api-token", rotations) {
		if err != nil {
			t.Fatalf("concurrent rotation %d: %v", i, err)
		}
	}

	if _, version = f.secretRow(t, projectID, "api-token"); version != 1+rotations {
		t.Errorf("version after %d concurrent rotations = %d, want %d — a write was lost",
			rotations, version, 1+rotations)
	}
}

// TestConcurrentFirstWriteStaysDecryptable covers the other half of the same
// race, the one that only exists before the row does. Every writer sees
// pgx.ErrNoRows, so every writer mints its OWN uuid and binds it into the
// AEAD associated data (crypto.EncryptionContext.ResourceID), while the
// upsert's DO UPDATE never touches `id` — so a ciphertext could end up on a
// row whose `id` belongs to the first inserter and stop decrypting entirely.
// Unlike the lost update above, this one did not reproduce on demand: it
// needs the LAST writer to commit to be one that saw no row, and in practice
// a writer that read the committed row overwrote it consistently first. The
// test is kept as a guard on the invariant that actually matters — the stored
// secret stays readable — rather than as a reproduction.
func TestConcurrentFirstWriteStaysDecryptable(t *testing.T) {
	f := newFixture(t)
	principalID, projectID := f.seedProject(t, "OWNER")
	ctx := context.Background()

	const writers = 8
	for i, err := range storeConcurrently(t, f.svc, principalID, projectID, "fresh-token", writers) {
		if err != nil {
			t.Fatalf("concurrent first write %d: %v", i, err)
		}
	}

	ref := secrets.NewProjectReference(projectID, "fresh-token")
	if _, err := f.svc.Resolve(ctx, principalID, projectID, ref); err != nil {
		t.Fatalf("secret is undecryptable after concurrent first writes: %v", err)
	}
}
