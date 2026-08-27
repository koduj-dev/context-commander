package crypto

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrNoProjectKey = errors.New("crypto: project has no key yet")

// projectKeyStore is the Postgres-backed repository behind project_keys —
// see docs/adr/0005-key-hierarchy-and-artifact-size-limit.md and
// docs/adr/0010-immutable-artifact-version.md.
type projectKeyStore struct{}

type projectKeyRow struct {
	Version    int
	WrappedKey []byte
	RootKeyRef string
}

func (s *projectKeyStore) Latest(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) (projectKeyRow, error) {
	var row projectKeyRow
	err := tx.QueryRow(ctx,
		`SELECT key_version, wrapped_key, root_key_ref FROM project_keys
		 WHERE project_id = $1 ORDER BY key_version DESC LIMIT 1`,
		projectID,
	).Scan(&row.Version, &row.WrappedKey, &row.RootKeyRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return projectKeyRow{}, ErrNoProjectKey
	}
	if err != nil {
		return projectKeyRow{}, fmt.Errorf("crypto: get latest project key: %w", err)
	}
	return row, nil
}

func (s *projectKeyStore) ByVersion(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, version int) (projectKeyRow, error) {
	var row projectKeyRow
	err := tx.QueryRow(ctx,
		`SELECT key_version, wrapped_key, root_key_ref FROM project_keys
		 WHERE project_id = $1 AND key_version = $2`,
		projectID, version,
	).Scan(&row.Version, &row.WrappedKey, &row.RootKeyRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return projectKeyRow{}, ErrNoProjectKey
	}
	if err != nil {
		return projectKeyRow{}, fmt.Errorf("crypto: get project key version %d: %w", version, err)
	}
	return row, nil
}

func (s *projectKeyStore) Insert(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, version int, wrappedKey []byte, rootKeyRef string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO project_keys (project_id, key_version, wrapped_key, root_key_ref) VALUES ($1, $2, $3, $4)`,
		projectID, version, wrappedKey, rootKeyRef,
	)
	if err != nil {
		return fmt.Errorf("crypto: insert project key: %w", err)
	}
	return nil
}
