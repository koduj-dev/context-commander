// Package crypto implements envelope encryption: root/installation key ->
// project key -> DEK per resource — see
// docs/adr/0005-key-hierarchy-and-artifact-size-limit.md.
package crypto

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Provider is the Postgres-backed CryptoProvider. Encrypt/Decrypt need a
// pgx.Tx because resolving/creating the project key requires reading or
// writing project_keys — callers get one via internal/db.WithPrincipal for
// any normal (project-scoped) call, same as every other repository in this
// codebase.
type Provider struct {
	root RootKeyProvider
	keys projectKeyStore
}

func NewProvider(root RootKeyProvider) *Provider {
	return &Provider{root: root, keys: projectKeyStore{}}
}

// Encrypt encrypts plaintext under a fresh, per-call DEK, itself wrapped by
// the project's current key (auto-provisioning key_version 1 the first time
// a project is encrypted into). AAD binds the ciphertext to ec — see
// docs/adr/0005 §AAD and EncryptionContext.EncodeAAD.
func (p *Provider) Encrypt(ctx context.Context, tx pgx.Tx, ec EncryptionContext, plaintext []byte) (ciphertext, nonce, wrappedDEK []byte, projectKeyVersion int, err error) {
	projectKey, version, err := p.ensureProjectKey(ctx, tx, ec.ProjectID)
	if err != nil {
		return nil, nil, nil, 0, err
	}

	dek, err := generateKey()
	if err != nil {
		return nil, nil, nil, 0, err
	}

	aad, err := ec.EncodeAAD()
	if err != nil {
		return nil, nil, nil, 0, err
	}

	ciphertext, nonce, err = seal(dek, plaintext, aad)
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("crypto: seal content: %w", err)
	}

	dekCiphertext, dekNonce, err := seal(projectKey, dek[:], nil)
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("crypto: wrap dek: %w", err)
	}

	return ciphertext, nonce, packWrapped(dekCiphertext, dekNonce), version, nil
}

// Decrypt reverses Encrypt. ec must be exactly the EncryptionContext used to
// encrypt — any mismatch (wrong project, resource type/id/version) fails
// with ErrAuthenticationFailed, indistinguishable from a tampered
// ciphertext or wrong key (docs/adr/0005 acceptance criteria).
func (p *Provider) Decrypt(ctx context.Context, tx pgx.Tx, ec EncryptionContext, ciphertext, nonce, wrappedDEK []byte, projectKeyVersion int) ([]byte, error) {
	row, err := p.keys.ByVersion(ctx, tx, ec.ProjectID, projectKeyVersion)
	if err != nil {
		return nil, err
	}
	projectKey, err := p.unwrapProjectKey(ctx, row)
	if err != nil {
		return nil, err
	}

	dekCiphertext, dekNonce, err := unpackWrapped(wrappedDEK)
	if err != nil {
		return nil, err
	}
	dekBytes, err := open(projectKey, dekCiphertext, dekNonce, nil)
	if err != nil {
		return nil, err
	}
	var dek [KeySize]byte
	copy(dek[:], dekBytes)

	aad, err := ec.EncodeAAD()
	if err != nil {
		return nil, err
	}
	return open(dek, ciphertext, nonce, aad)
}

// RotateProjectKey creates project_keys version N+1 for projectID. It does
// NOT touch any existing ArtifactVersionEnvelope — see docs/adr/0005: new
// encryptions use the new version, existing ones keep referencing whatever
// version they were encrypted under (project_keys rows are never deleted),
// so nothing needs rewrapping as a consequence of rotating.
func (p *Provider) RotateProjectKey(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	_, _, err := p.newProjectKeyVersion(ctx, tx, projectID)
	return err
}

// RotateRootKey rewraps every project_keys row from whatever root key
// `previous` provides to the (new) key `p`'s own RootKeyProvider currently
// provides. Both are needed explicitly: once a root key stops being
// "current," nothing else in this package retains access to it (the whole
// point of not storing it in the DB — docs/adr/0002's role-model note
// applies here too), so the caller performing a rotation must supply the
// outgoing key material itself for this one operation, alongside the
// Provider's own (incoming) key. An EnvRootKeyProvider constructed from the
// old key value is a normal way to satisfy `previous`.
//
// This is a cross-project maintenance operation — tx must be an RLS-exempt
// transaction (cc_owner-connected via db.WithoutPrincipal), never a normal
// per-principal WithPrincipal transaction, since it has to see and update
// every project's keys regardless of who's calling it (docs/adr/0005).
func (p *Provider) RotateRootKey(ctx context.Context, tx pgx.Tx, previous RootKeyProvider) error {
	newKey, newRef, err := p.root.CurrentKey(ctx)
	if err != nil {
		return fmt.Errorf("crypto: read new root key: %w", err)
	}
	oldKey, oldRef, err := previous.CurrentKey(ctx)
	if err != nil {
		return fmt.Errorf("crypto: read previous root key: %w", err)
	}

	rows, err := tx.Query(ctx, `SELECT id, key_version, wrapped_key, root_key_ref FROM project_keys`)
	if err != nil {
		return fmt.Errorf("crypto: list project keys: %w", err)
	}
	defer rows.Close()

	type target struct {
		id         uuid.UUID
		projectKey [KeySize]byte
	}
	var targets []target

	for rows.Next() {
		var (
			id         uuid.UUID
			version    int
			wrappedKey []byte
			rootKeyRef string
		)
		if err := rows.Scan(&id, &version, &wrappedKey, &rootKeyRef); err != nil {
			return fmt.Errorf("crypto: scan project key row: %w", err)
		}
		if rootKeyRef == newRef {
			continue // already wrapped under the current root key
		}
		if rootKeyRef != oldRef {
			return fmt.Errorf(
				"crypto: project key %s is wrapped under root key %q, which matches neither the previous key %q nor the current key %q",
				id, rootKeyRef, oldRef, newRef)
		}

		ciphertext, nonce, err := unpackWrapped(wrappedKey)
		if err != nil {
			return fmt.Errorf("crypto: unpack project key %s: %w", id, err)
		}
		unwrapped, err := open(oldKey, ciphertext, nonce, nil)
		if err != nil {
			return fmt.Errorf("crypto: unwrap project key %s with previous root key: %w", id, err)
		}
		var pk [KeySize]byte
		copy(pk[:], unwrapped)
		targets = append(targets, target{id: id, projectKey: pk})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("crypto: iterate project keys: %w", err)
	}

	for _, t := range targets {
		ciphertext, nonce, err := seal(newKey, t.projectKey[:], nil)
		if err != nil {
			return fmt.Errorf("crypto: rewrap project key %s: %w", t.id, err)
		}
		_, err = tx.Exec(ctx,
			`UPDATE project_keys SET wrapped_key = $1, root_key_ref = $2 WHERE id = $3`,
			packWrapped(ciphertext, nonce), newRef, t.id,
		)
		if err != nil {
			return fmt.Errorf("crypto: store rewrapped project key %s: %w", t.id, err)
		}
	}
	return nil
}

func (p *Provider) ensureProjectKey(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) ([KeySize]byte, int, error) {
	row, err := p.keys.Latest(ctx, tx, projectID)
	if errors.Is(err, ErrNoProjectKey) {
		return p.newProjectKeyVersion(ctx, tx, projectID)
	}
	if err != nil {
		return [KeySize]byte{}, 0, err
	}
	key, err := p.unwrapProjectKey(ctx, row)
	return key, row.Version, err
}

func (p *Provider) newProjectKeyVersion(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) ([KeySize]byte, int, error) {
	prev, err := p.keys.Latest(ctx, tx, projectID)
	nextVersion := 1
	if err == nil {
		nextVersion = prev.Version + 1
	} else if !errors.Is(err, ErrNoProjectKey) {
		return [KeySize]byte{}, 0, err
	}

	projectKey, err := generateKey()
	if err != nil {
		return [KeySize]byte{}, 0, err
	}

	rootKey, rootRef, err := p.root.CurrentKey(ctx)
	if err != nil {
		return [KeySize]byte{}, 0, fmt.Errorf("crypto: read root key: %w", err)
	}

	ciphertext, nonce, err := seal(rootKey, projectKey[:], nil)
	if err != nil {
		return [KeySize]byte{}, 0, fmt.Errorf("crypto: wrap project key: %w", err)
	}

	if err := p.keys.Insert(ctx, tx, projectID, nextVersion, packWrapped(ciphertext, nonce), rootRef); err != nil {
		return [KeySize]byte{}, 0, err
	}
	return projectKey, nextVersion, nil
}

func (p *Provider) unwrapProjectKey(ctx context.Context, row projectKeyRow) ([KeySize]byte, error) {
	rootKey, currentRef, err := p.root.CurrentKey(ctx)
	if err != nil {
		return [KeySize]byte{}, fmt.Errorf("crypto: read root key: %w", err)
	}
	if row.RootKeyRef != currentRef {
		return [KeySize]byte{}, fmt.Errorf(
			"crypto: project key was wrapped under root key %q, current root key is %q — RotateRootKey did not complete for this row",
			row.RootKeyRef, currentRef)
	}

	ciphertext, nonce, err := unpackWrapped(row.WrappedKey)
	if err != nil {
		return [KeySize]byte{}, err
	}

	plaintext, err := open(rootKey, ciphertext, nonce, nil)
	if err != nil {
		return [KeySize]byte{}, err
	}

	var key [KeySize]byte
	copy(key[:], plaintext)
	return key, nil
}
