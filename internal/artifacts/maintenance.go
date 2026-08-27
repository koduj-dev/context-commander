package artifacts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/storage"
)

// PurgeArtifact physically removes an artifact and all its versions: the DB
// rows first (inside tx, which the caller commits), then — only after that
// commit has actually happened — the ObjectStore objects they referenced.
// This is the second half of docs/adr/0013's tombstone-then-cleanup
// lifecycle; Delete (postgres.go) only sets deleted_at.
//
// tx must be an RLS-exempt, cc_owner-connected transaction (via
// db.WithoutPrincipal on an owner-privileged pool) — cc_app has no DELETE
// grant on artifact_versions/artifact_version_envelopes at all (immutable
// at the privilege level, see migrations/000003), so a normal
// WithPrincipal/cc_app transaction cannot run this even by accident.
func PurgeArtifact(ctx context.Context, tx pgx.Tx, artifactID uuid.UUID) (storageKeys []string, err error) {
	rows, err := tx.Query(ctx, `SELECT storage_key FROM artifact_versions WHERE artifact_id = $1`, artifactID)
	if err != nil {
		return nil, fmt.Errorf("artifacts: purge: list versions: %w", err)
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, fmt.Errorf("artifacts: purge: scan storage_key: %w", err)
		}
		storageKeys = append(storageKeys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("artifacts: purge: iterate versions: %w", err)
	}
	rows.Close()

	if _, err := tx.Exec(ctx,
		`DELETE FROM artifact_version_envelopes WHERE artifact_version_id IN (SELECT id FROM artifact_versions WHERE artifact_id = $1)`,
		artifactID); err != nil {
		return nil, fmt.Errorf("artifacts: purge: delete envelopes: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM artifact_versions WHERE artifact_id = $1`, artifactID); err != nil {
		return nil, fmt.Errorf("artifacts: purge: delete versions: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM artifacts WHERE id = $1`, artifactID); err != nil {
		return nil, fmt.Errorf("artifacts: purge: delete artifact: %w", err)
	}

	return storageKeys, nil
}

// DeleteStorageKeys physically removes previously-collected storage keys —
// call only after the transaction that returned them (PurgeArtifact) has
// committed, per docs/adr/0013's write ordering.
func DeleteStorageKeys(ctx context.Context, store storage.ObjectStore, keys []string) error {
	for _, key := range keys {
		if err := store.Delete(ctx, key); err != nil {
			return fmt.Errorf("artifacts: delete storage key %q: %w", key, err)
		}
	}
	return nil
}

// SweepOrphanObjects finds and deletes objects under
// projects/<projectID>/artifacts/ that no artifact_versions row references
// — the async cleanup half of docs/adr/0013's failure-safe write lifecycle
// (a Write whose object upload succeeded but whose deciding transaction
// then failed/rolled back leaves exactly this kind of orphan). olderThan
// gives an in-flight Write (uploaded, DB transaction not committed yet) a
// grace period so this never races a legitimate in-progress write.
func SweepOrphanObjects(ctx context.Context, tx pgx.Tx, store storage.ObjectStore, projectID uuid.UUID, olderThan time.Duration) ([]string, error) {
	prefix := fmt.Sprintf("projects/%s/artifacts/", projectID)
	objects, err := store.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("artifacts: sweep: list objects: %w", err)
	}
	if len(objects) == 0 {
		return nil, nil
	}

	rows, err := tx.Query(ctx, `SELECT storage_key FROM artifact_versions WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, fmt.Errorf("artifacts: sweep: list known keys: %w", err)
	}
	known := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, fmt.Errorf("artifacts: sweep: scan storage_key: %w", err)
		}
		known[key] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("artifacts: sweep: iterate known keys: %w", err)
	}
	rows.Close()

	cutoff := time.Now().Add(-olderThan)
	var deleted []string
	for _, obj := range objects {
		if known[obj.Key] {
			continue
		}
		if obj.LastModified.After(cutoff) {
			continue // too recent: could be an in-flight Write, leave it for the next sweep
		}
		if err := store.Delete(ctx, obj.Key); err != nil {
			return deleted, fmt.Errorf("artifacts: sweep: delete %q: %w", obj.Key, err)
		}
		deleted = append(deleted, obj.Key)
	}
	return deleted, nil
}
