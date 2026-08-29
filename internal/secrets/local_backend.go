package secrets

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
)

// LocalEncryptedBackend is the v1 (and, for now, only) Backend: it stores
// ciphertext in secret_values (migrations/000007_secrets.up.sql), wrapped
// through the same project-key envelope internal/artifacts uses for
// content — see docs/adr/0005. Using crypto.Provider here is an
// implementation detail of THIS backend's own at-rest storage, not a
// violation of the SECRET-vs-CONTENT-PAYLOAD classification rule
// (docs/adr/0011): that rule is about how OTHER domains reference a
// secret (only ever via secret://, never inline), not about how a backend
// protects its own storage.
type LocalEncryptedBackend struct {
	pool   *db.Pool
	crypto *crypto.Provider
}

func NewLocalEncryptedBackend(pool *db.Pool, cryptoProvider *crypto.Provider) *LocalEncryptedBackend {
	return &LocalEncryptedBackend{pool: pool, crypto: cryptoProvider}
}

// Store upserts (ref.ScopeRef, ref.Name)'s value. version is bumped on
// every call to the same name so the AEAD associated data
// (crypto.EncryptionContext.ResourceVersion) binds each ciphertext to
// exactly the update that produced it — the same reason artifact versions
// carry their own version number into AAD (docs/adr/0005 §AAD).
func (b *LocalEncryptedBackend) Store(ctx context.Context, principalID uuid.UUID, ref Reference, plaintext []byte) error {
	return db.WithPrincipal(ctx, b.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var id uuid.UUID
		var version int
		err := tx.QueryRow(ctx,
			`SELECT id, version FROM secret_values WHERE project_id = $1 AND name = $2`,
			ref.ScopeRef, ref.Name,
		).Scan(&id, &version)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			id = uuid.New()
			version = 1
		case err != nil:
			return fmt.Errorf("secrets: store: lookup existing: %w", err)
		default:
			version++
		}

		ec := crypto.EncryptionContext{
			ProjectID: ref.ScopeRef, ResourceType: "secret",
			ResourceID: id, ResourceVersion: uint64(version), //nolint:gosec // version is a small positive counter
		}
		ciphertext, nonce, wrappedDEK, projectKeyVersion, err := b.crypto.Encrypt(ctx, tx, ec, plaintext)
		if err != nil {
			return fmt.Errorf("secrets: store: encrypt: %w", err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO secret_values (id, project_id, name, version, ciphertext, nonce, wrapped_dek, project_key_version)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 ON CONFLICT (project_id, name) DO UPDATE SET
			   version = EXCLUDED.version, ciphertext = EXCLUDED.ciphertext, nonce = EXCLUDED.nonce,
			   wrapped_dek = EXCLUDED.wrapped_dek, project_key_version = EXCLUDED.project_key_version,
			   updated_at = now()`,
			id, ref.ScopeRef, ref.Name, version, ciphertext, nonce, wrappedDEK, projectKeyVersion,
		)
		if err != nil {
			return fmt.Errorf("secrets: store: upsert: %w", err)
		}
		return nil
	})
}

func (b *LocalEncryptedBackend) Fetch(ctx context.Context, principalID uuid.UUID, ref Reference) ([]byte, error) {
	var plaintext []byte
	err := db.WithPrincipal(ctx, b.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var id uuid.UUID
		var version, projectKeyVersion int
		var ciphertext, nonce, wrappedDEK []byte
		err := tx.QueryRow(ctx,
			`SELECT id, version, ciphertext, nonce, wrapped_dek, project_key_version
			 FROM secret_values WHERE project_id = $1 AND name = $2`,
			ref.ScopeRef, ref.Name,
		).Scan(&id, &version, &ciphertext, &nonce, &wrappedDEK, &projectKeyVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("secrets: fetch: %w", err)
		}

		ec := crypto.EncryptionContext{
			ProjectID: ref.ScopeRef, ResourceType: "secret",
			ResourceID: id, ResourceVersion: uint64(version), //nolint:gosec // version is a small positive counter
		}
		plaintext, err = b.crypto.Decrypt(ctx, tx, ec, ciphertext, nonce, wrappedDEK, projectKeyVersion)
		if err != nil {
			return fmt.Errorf("secrets: decrypt: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func (b *LocalEncryptedBackend) Delete(ctx context.Context, principalID uuid.UUID, ref Reference) error {
	return db.WithPrincipal(ctx, b.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM secret_values WHERE project_id = $1 AND name = $2`, ref.ScopeRef, ref.Name)
		if err != nil {
			return fmt.Errorf("secrets: delete: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (b *LocalEncryptedBackend) List(ctx context.Context, principalID uuid.UUID, scope string, scopeRef uuid.UUID) ([]string, error) {
	var names []string
	err := db.WithPrincipal(ctx, b.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if scope != "project" {
			return fmt.Errorf("secrets: list: unsupported scope %q", scope)
		}
		rows, err := tx.Query(ctx, `SELECT name FROM secret_values WHERE project_id = $1 ORDER BY name`, scopeRef)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			names = append(names, name)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("secrets: list: %w", err)
	}
	return names, nil
}
