package artifacts

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
)

// upsertArtifactStub inserts the Artifact row if this is the first write
// for (projectID, logicalPath), or fetches the existing one — a standard
// Postgres "insert or get" idiom (ON CONFLICT DO UPDATE with a no-op SET,
// just to make RETURNING give back the existing row).
func upsertArtifactStub(
	ctx context.Context, tx pgx.Tx,
	projectID uuid.UUID, logicalPath, contentType string,
	principalID uuid.UUID, clientID *uuid.UUID,
) (id uuid.UUID, currentVersion int, deleted bool, err error) {
	var deletedAt any
	err = tx.QueryRow(ctx,
		`INSERT INTO artifacts (project_id, logical_path, content_type, created_by_principal, created_by_client)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (project_id, logical_path) DO UPDATE SET logical_path = artifacts.logical_path
		 RETURNING id, current_version, deleted_at`,
		projectID, logicalPath, contentType, principalID, clientID,
	).Scan(&id, &currentVersion, &deletedAt)
	if err != nil {
		return uuid.Nil, 0, false, fmt.Errorf("artifacts: upsert: %w", err)
	}
	return id, currentVersion, deletedAt != nil, nil
}

func lookupArtifactID(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, logicalPath string) (id uuid.UUID, deleted bool, err error) {
	var deletedAt any
	err = tx.QueryRow(ctx,
		`SELECT id, deleted_at FROM artifacts WHERE project_id = $1 AND logical_path = $2`,
		projectID, logicalPath,
	).Scan(&id, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("artifacts: lookup: %w", err)
	}
	return id, deletedAt != nil, nil
}

type versionRow struct {
	Version           Version
	Nonce             []byte
	WrappedDEK        []byte
	ProjectKeyVersion int
}

const versionSelectColumns = `
	v.id, v.project_id, v.artifact_id, v.version, v.content_hash, v.storage_key, v.size_bytes,
	v.parent_version, v.created_by_principal, v.created_by_client, v.created_at,
	e.nonce, e.wrapped_dek, e.project_key_version`

func scanVersionRow(row pgx.Row) (versionRow, error) {
	var vr versionRow
	err := row.Scan(
		&vr.Version.ID, &vr.Version.ProjectID, &vr.Version.ArtifactID, &vr.Version.Version,
		&vr.Version.ContentHash, &vr.Version.StorageKey, &vr.Version.SizeBytes,
		&vr.Version.ParentVersion, &vr.Version.CreatedByPrincipal, &vr.Version.CreatedByClient, &vr.Version.CreatedAt,
		&vr.Nonce, &vr.WrappedDEK, &vr.ProjectKeyVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return versionRow{}, ErrNotFound
	}
	if err != nil {
		return versionRow{}, fmt.Errorf("artifacts: scan version: %w", err)
	}
	return vr, nil
}

// queryVersion fetches a specific version (or the latest, when version is
// nil) joined with its envelope.
func queryVersion(ctx context.Context, tx pgx.Tx, artifactID uuid.UUID, version *int) (versionRow, error) {
	if version != nil {
		row := tx.QueryRow(ctx,
			`SELECT `+versionSelectColumns+`
			 FROM artifact_versions v JOIN artifact_version_envelopes e ON e.artifact_version_id = v.id
			 WHERE v.artifact_id = $1 AND v.version = $2`,
			artifactID, *version)
		return scanVersionRow(row)
	}
	row := tx.QueryRow(ctx,
		`SELECT `+versionSelectColumns+`
		 FROM artifact_versions v JOIN artifact_version_envelopes e ON e.artifact_version_id = v.id
		 WHERE v.artifact_id = $1 ORDER BY v.version DESC LIMIT 1`,
		artifactID)
	return scanVersionRow(row)
}

// Stat returns artifact metadata without fetching/decrypting content.
func (s *Service) Stat(ctx context.Context, principalID uuid.UUID, projectID uuid.UUID, logicalPath string) (Artifact, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapArtifactRead); err != nil {
		return Artifact{}, err
	}

	var a Artifact
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id, project_id, logical_path, content_type, current_version,
			        created_by_principal, created_by_client, created_at, updated_at, deleted_at
			 FROM artifacts WHERE project_id = $1 AND logical_path = $2`,
			projectID, logicalPath,
		).Scan(&a.ID, &a.ProjectID, &a.LogicalPath, &a.ContentType, &a.CurrentVersion,
			&a.CreatedByPrincipal, &a.CreatedByClient, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("artifacts: stat: %w", err)
	}
	return a, nil
}

// List returns non-deleted artifacts in a project, newest-updated first.
func (s *Service) List(ctx context.Context, principalID uuid.UUID, projectID uuid.UUID) ([]Artifact, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapArtifactRead); err != nil {
		return nil, err
	}

	var out []Artifact
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, project_id, logical_path, content_type, current_version,
			        created_by_principal, created_by_client, created_at, updated_at, deleted_at
			 FROM artifacts WHERE project_id = $1 AND deleted_at IS NULL ORDER BY updated_at DESC`,
			projectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Artifact
			if err := rows.Scan(&a.ID, &a.ProjectID, &a.LogicalPath, &a.ContentType, &a.CurrentVersion,
				&a.CreatedByPrincipal, &a.CreatedByClient, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("artifacts: list: %w", err)
	}
	return out, nil
}

// History returns every version of an artifact, oldest first.
func (s *Service) History(ctx context.Context, principalID uuid.UUID, projectID uuid.UUID, logicalPath string) ([]Version, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapArtifactRead); err != nil {
		return nil, err
	}

	var out []Version
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		artifactID, deleted, err := lookupArtifactID(ctx, tx, projectID, logicalPath)
		if err != nil {
			return err
		}
		if deleted {
			return ErrDeleted
		}
		rows, err := tx.Query(ctx,
			`SELECT id, project_id, artifact_id, version, content_hash, storage_key, size_bytes,
			        parent_version, created_by_principal, created_by_client, created_at
			 FROM artifact_versions WHERE artifact_id = $1 ORDER BY version ASC`,
			artifactID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v Version
			if err := rows.Scan(&v.ID, &v.ProjectID, &v.ArtifactID, &v.Version, &v.ContentHash, &v.StorageKey,
				&v.SizeBytes, &v.ParentVersion, &v.CreatedByPrincipal, &v.CreatedByClient, &v.CreatedAt); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("artifacts: history: %w", err)
	}
	return out, nil
}

// Delete soft-deletes an artifact (tombstone) — see docs/adr/0013. Version
// history and audit trail are kept; physical storage cleanup is a separate,
// explicit PurgeArtifact call, not an automatic consequence of Delete.
func (s *Service) Delete(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID uuid.UUID, logicalPath string) error {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapArtifactDelete); err != nil {
		return err
	}

	return db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		artifactID, deleted, err := lookupArtifactID(ctx, tx, projectID, logicalPath)
		if err != nil {
			return err
		}
		if deleted {
			return nil // already deleted: idempotent
		}

		if _, err := tx.Exec(ctx, `UPDATE artifacts SET deleted_at = now() WHERE id = $1`, artifactID); err != nil {
			return fmt.Errorf("artifacts: delete: %w", err)
		}

		eventID, err := events.Publish(ctx, tx, projectID, "artifact.deleted", "artifact", &artifactID, events.PayloadKindMetadataOnly)
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID, ClientID: clientID,
			Action: "artifact.delete", ResourceType: "artifact", ResourceID: &artifactID,
			Result: audit.ResultAllow, EventID: &eventID,
		})
	})
}
