package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/quota"
	"github.com/koduj-dev/context-commander/internal/storage"
)

// ErrForbidden is returned when AuthzService.Can denies the requested
// capability. Distinct from ErrNotFound so a caller can tell "you can't see
// this" apart from "this doesn't exist" — which of those an adapter
// actually surfaces to the caller (to avoid leaking existence) is that
// adapter's call, not this package's.
var ErrForbidden = errors.New("artifacts: forbidden")

// ErrBlockedByGate is returned when the "artifact.before_write" gate hook
// (internal/gates) evaluates to DENY — e.g. the secret-scanner gate
// finding secret-like content — before any crypto/storage work happens.
var ErrBlockedByGate = errors.New("artifacts: write blocked by gate policy")

type Service struct {
	pool   *db.Pool
	crypto *crypto.Provider
	store  storage.ObjectStore
	authz  *auth.AuthzService
	gates  *gates.Engine
}

// NewService wires Service to AuthzService deliberately: every method here
// calls Can() itself before touching data, rather than trusting an adapter
// to have checked already. docs/architecture-cookbook.md's own guidance
// ("a single AuthzService.Can() call from one middleware/interceptor for
// all adapters") is satisfied more robustly this way than by per-adapter
// middleware would: a domain service is the one chokepoint every adapter
// (HTTP, MCP, CLI) *and* every other domain service that might call into
// artifacts necessarily goes through, whereas per-adapter middleware is
// still N places that all have to remember to add the check — exactly the
// "someone adds a handler and forgets" failure mode the pitfall warns
// about. RLS (docs/adr/0002) remains the defense-in-depth layer beneath
// this, not a replacement for it.
func NewService(pool *db.Pool, cryptoProvider *crypto.Provider, store storage.ObjectStore, authz *auth.AuthzService, gatesEngine *gates.Engine) *Service {
	return &Service{pool: pool, crypto: cryptoProvider, store: store, authz: authz, gates: gatesEngine}
}

func (s *Service) checkCapability(ctx context.Context, principalID, projectID uuid.UUID, cap auth.Capability) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, cap)
	if err != nil {
		return fmt.Errorf("artifacts: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// Write follows the failure-safe lifecycle from docs/adr/0013:
//  1. a short transaction resolves/creates the Artifact row and encrypts
//     the content (encryption needs project_keys, hence a transaction);
//  2. the ciphertext is uploaded to ObjectStore — outside any transaction,
//     since object stores don't participate in Postgres transactions;
//  3. a second transaction does the atomic optimistic-concurrency check,
//     inserts the immutable Version + its envelope, and writes audit +
//     outbox rows together.
//
// If step 3 fails, the object uploaded in step 2 becomes an orphan
// candidate — see SweepOrphanObjects — rather than the DB ever pointing at
// an object that was never actually written.
func (s *Service) Write(
	ctx context.Context,
	principalID uuid.UUID, clientID *uuid.UUID,
	projectID uuid.UUID, logicalPath, contentType string,
	plaintext []byte, expectedVersion int,
) (Version, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapArtifactWrite); err != nil {
		return Version{}, err
	}
	if int64(len(plaintext)) > MaxSize {
		return Version{}, ErrTooLarge
	}

	var orgID *uuid.UUID
	if err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT n.organization_id FROM projects p JOIN namespaces n ON n.id = p.namespace_id WHERE p.id = $1`,
			projectID,
		).Scan(&orgID)
	}); err != nil {
		return Version{}, fmt.Errorf("artifacts: resolve organization: %w", err)
	}

	gateResult, err := s.gates.Evaluate(ctx, principalID, projectID, orgID, "artifact.before_write", plaintext)
	if err != nil {
		return Version{}, fmt.Errorf("artifacts: gate evaluation: %w", err)
	}
	if gateResult.Verdict == gates.Deny {
		return Version{}, fmt.Errorf("%w: %s", ErrBlockedByGate, gateResult.Guidance)
	}
	// ALLOW_WITH_GUIDANCE / ALLOW_WITH_REDACTION: v1 does not thread
	// guidance back to the caller or apply a redaction to plaintext before
	// encryption — both proceed as ALLOW. Surfacing guidance and applying
	// redaction is adapter-phase work (Fáze 3).

	principal := db.Principal{ID: principalID}
	sum := sha256.Sum256(plaintext)
	contentHash := hex.EncodeToString(sum[:])
	versionID := uuid.New()
	nextVersion := expectedVersion + 1

	var artifactID uuid.UUID
	var ciphertext, nonce, wrappedDEK []byte
	var projectKeyVersion int

	err = db.WithPrincipal(ctx, s.pool, principal, func(tx pgx.Tx) error {
		id, currentVersion, deleted, err := upsertArtifactStub(ctx, tx, projectID, logicalPath, contentType, principalID, clientID)
		if err != nil {
			return err
		}
		if deleted {
			return ErrDeleted
		}
		artifactID = id
		if currentVersion != expectedVersion {
			return ErrConflict // fail fast, before doing any crypto/storage work
		}
		// Before the upload, so a full namespace does not cost an upload
		// and an orphan. Advisory: the insert's trigger decides
		// (docs/adr/0021).
		if _, err := tx.Exec(ctx, `SELECT cc_check_project_storage($1, $2)`, projectID, int64(len(plaintext))); err != nil {
			return quota.Translate(err)
		}

		ec := crypto.EncryptionContext{
			ProjectID: projectID, ResourceType: "artifact_version",
			ResourceID: versionID, ResourceVersion: uint64(nextVersion), //nolint:gosec // nextVersion is a small positive counter
		}
		ciphertext, nonce, wrappedDEK, projectKeyVersion, err = s.crypto.Encrypt(ctx, tx, ec, plaintext)
		return err
	})
	if err != nil {
		return Version{}, err
	}

	storageKey := fmt.Sprintf("projects/%s/artifacts/%s/versions/%s", projectID, artifactID, versionID)
	if _, err := s.store.Put(ctx, storageKey, bytes.NewReader(ciphertext), int64(len(ciphertext))); err != nil {
		return Version{}, fmt.Errorf("artifacts: upload: %w", err)
	}

	var result Version
	err = db.WithPrincipal(ctx, s.pool, principal, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE artifacts SET current_version = $1, updated_at = now() WHERE id = $2 AND current_version = $3`,
			nextVersion, artifactID, expectedVersion,
		)
		if err != nil {
			return fmt.Errorf("update artifact current_version: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}

		var parentVersion *int
		if expectedVersion > 0 {
			v := expectedVersion
			parentVersion = &v
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO artifact_versions (id, project_id, artifact_id, version, content_hash, storage_key, size_bytes, parent_version, created_by_principal, created_by_client)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			versionID, projectID, artifactID, nextVersion, contentHash, storageKey, int64(len(plaintext)), parentVersion, principalID, clientID,
		)
		if err != nil {
			return fmt.Errorf("insert artifact_versions: %w", err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO artifact_version_envelopes (artifact_version_id, project_id, nonce, wrapped_dek, project_key_version)
			 VALUES ($1, $2, $3, $4, $5)`,
			versionID, projectID, nonce, wrappedDEK, projectKeyVersion,
		)
		if err != nil {
			return fmt.Errorf("insert artifact_version_envelopes: %w", err)
		}

		eventID, err := events.Publish(ctx, tx, projectID, "artifact.updated", "artifact_version", &versionID, events.PayloadKindMetadataOnly)
		if err != nil {
			return err
		}

		if err := audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID, ClientID: clientID,
			Action: "artifact.write", ResourceType: "artifact_version", ResourceID: &versionID,
			Result: audit.ResultAllow, EventID: &eventID,
		}); err != nil {
			return err
		}

		result = Version{
			ID: versionID, ProjectID: projectID, ArtifactID: artifactID, Version: nextVersion,
			ContentHash: contentHash, StorageKey: storageKey, SizeBytes: int64(len(plaintext)),
			ParentVersion: parentVersion, CreatedByPrincipal: principalID, CreatedByClient: clientID,
			CreatedAt: time.Now().UTC(),
		}
		return nil
	})
	if err != nil {
		// storageKey uploaded above is now an orphan candidate if this
		// transaction didn't commit — see SweepOrphanObjects.
		return Version{}, quota.Translate(err)
	}
	return result, nil
}

// Read fetches a specific version (or the latest, if version is nil) and
// returns it decrypted, verifying the plaintext still hashes to
// ContentHash as a defense-in-depth integrity check on top of AES-GCM's own
// authentication.
func (s *Service) Read(ctx context.Context, principalID uuid.UUID, projectID uuid.UUID, logicalPath string, version *int) (Version, []byte, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapArtifactRead); err != nil {
		return Version{}, nil, err
	}

	principal := db.Principal{ID: principalID}

	var v Version
	var nonce, wrappedDEK []byte
	var projectKeyVersion int

	err := db.WithPrincipal(ctx, s.pool, principal, func(tx pgx.Tx) error {
		artifactID, deleted, err := lookupArtifactID(ctx, tx, projectID, logicalPath)
		if err != nil {
			return err
		}
		if deleted {
			return ErrDeleted
		}

		row, err := queryVersion(ctx, tx, artifactID, version)
		if err != nil {
			return err
		}
		v = row.Version
		nonce = row.Nonce
		wrappedDEK = row.WrappedDEK
		projectKeyVersion = row.ProjectKeyVersion
		return nil
	})
	if err != nil {
		return Version{}, nil, err
	}

	rc, err := s.store.Get(ctx, v.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return Version{}, nil, fmt.Errorf("artifacts: %s/%s v%d: ciphertext missing from storage: %w", projectID, logicalPath, v.Version, err)
		}
		return Version{}, nil, fmt.Errorf("artifacts: fetch ciphertext: %w", err)
	}
	ciphertext, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil {
		return Version{}, nil, fmt.Errorf("artifacts: read ciphertext: %w", readErr)
	}
	if closeErr != nil {
		return Version{}, nil, fmt.Errorf("artifacts: close ciphertext reader: %w", closeErr)
	}

	var plaintext []byte
	err = db.WithPrincipal(ctx, s.pool, principal, func(tx pgx.Tx) error {
		ec := crypto.EncryptionContext{
			ProjectID: projectID, ResourceType: "artifact_version",
			ResourceID: v.ID, ResourceVersion: uint64(v.Version), //nolint:gosec // v.Version is a small positive counter
		}
		var err error
		plaintext, err = s.crypto.Decrypt(ctx, tx, ec, ciphertext, nonce, wrappedDEK, projectKeyVersion)
		return err
	})
	if err != nil {
		return Version{}, nil, fmt.Errorf("artifacts: decrypt: %w", err)
	}

	sum := sha256.Sum256(plaintext)
	if hex.EncodeToString(sum[:]) != v.ContentHash {
		return Version{}, nil, fmt.Errorf("artifacts: %s/%s v%d: content hash mismatch after decrypt (storage/crypto layer inconsistency)", projectID, logicalPath, v.Version)
	}

	return v, plaintext, nil
}
