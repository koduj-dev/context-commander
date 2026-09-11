package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/paging"
)

// foreignKeyViolation is Postgres's SQLSTATE for a foreign key constraint
// violation — see migrations/000015's composite lineage FKs.
const foreignKeyViolation = "23503"

// asInvalidReference maps a composite-FK violation (a caller-supplied
// conversation/session/derived-from ID that doesn't belong to this
// project/conversation) to ErrInvalidReference instead of leaking the raw
// constraint-violation error.
func asInvalidReference(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
		return ErrInvalidReference
	}
	return err
}

// Service is the authorization chokepoint for history (docs/adr/0014).
type Service struct {
	pool   *db.Pool
	crypto *crypto.Provider
	authz  *auth.AuthzService
}

func NewService(pool *db.Pool, cryptoProvider *crypto.Provider, authz *auth.AuthzService) *Service {
	return &Service{pool: pool, crypto: cryptoProvider, authz: authz}
}

func (s *Service) checkCapability(ctx context.Context, principalID, projectID uuid.UUID, cap auth.Capability) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, cap)
	if err != nil {
		return fmt.Errorf("history: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (s *Service) CreateConversation(ctx context.Context, principalID, projectID uuid.UUID) (Conversation, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapHistoryWrite); err != nil {
		return Conversation{}, err
	}

	c := Conversation{ID: uuid.New(), ProjectID: projectID, CreatedByPrincipal: principalID, CreatedAt: time.Now().UTC()}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO conversations (id, project_id, created_by_principal, created_at) VALUES ($1, $2, $3, $4)`,
			c.ID, c.ProjectID, c.CreatedByPrincipal, c.CreatedAt,
		)
		return err
	})
	if err != nil {
		return Conversation{}, fmt.Errorf("history: create conversation: %w", err)
	}
	return c, nil
}

func (s *Service) CreateExternalSession(
	ctx context.Context,
	principalID, projectID, conversationID uuid.UUID,
	provider, client, providerSessionID string,
	parentExternalSessionID *uuid.UUID, relation string, startedAt time.Time,
) (ExternalSession, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapHistoryWrite); err != nil {
		return ExternalSession{}, err
	}

	es := ExternalSession{
		ID: uuid.New(), ProjectID: projectID, ConversationID: conversationID,
		Provider: provider, Client: client, ProviderSessionID: providerSessionID,
		ParentExternalSessionID: parentExternalSessionID, Relation: relation, StartedAt: startedAt,
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO external_sessions (id, project_id, conversation_id, provider, client, provider_session_id, parent_external_session_id, relation, started_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			es.ID, es.ProjectID, es.ConversationID, es.Provider, es.Client, es.ProviderSessionID,
			es.ParentExternalSessionID, es.Relation, es.StartedAt,
		)
		return err
	})
	if err != nil {
		return ExternalSession{}, fmt.Errorf("history: create external session: %w", asInvalidReference(err))
	}
	return es, nil
}

// AppendHistoryObject inserts a new, immutable HistoryObject. There is
// deliberately no Update method anywhere in this package — compaction
// never overwrites original history (§10.3): a compacted/summarized
// representation is always a new row with DerivedFrom set.
func (s *Service) AppendHistoryObject(
	ctx context.Context,
	principalID, projectID, conversationID, externalSessionID uuid.UUID,
	objType string, derivedFrom *uuid.UUID, plaintext []byte,
) (HistoryObject, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapHistoryWrite); err != nil {
		return HistoryObject{}, err
	}
	if int64(len(plaintext)) > MaxContentSize {
		return HistoryObject{}, ErrTooLarge
	}

	sum := sha256.Sum256(plaintext)
	contentHash := hex.EncodeToString(sum[:])
	id := uuid.New()
	createdAt := time.Now().UTC()

	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		ec := crypto.EncryptionContext{
			ProjectID: projectID, ResourceType: "history_object",
			ResourceID: id, ResourceVersion: 1,
		}
		ciphertext, nonce, wrappedDEK, projectKeyVersion, err := s.crypto.Encrypt(ctx, tx, ec, plaintext)
		if err != nil {
			return fmt.Errorf("history: encrypt: %w", err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO history_objects (id, project_id, conversation_id, external_session_id, type, derived_from, content_hash, ciphertext, nonce, wrapped_dek, project_key_version, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			id, projectID, conversationID, externalSessionID, objType, derivedFrom, contentHash,
			ciphertext, nonce, wrappedDEK, projectKeyVersion, createdAt,
		)
		return err
	})
	if err != nil {
		return HistoryObject{}, fmt.Errorf("history: append history object: %w", asInvalidReference(err))
	}

	return HistoryObject{
		ID: id, ProjectID: projectID, ConversationID: conversationID, ExternalSessionID: externalSessionID,
		Type: objType, DerivedFrom: derivedFrom, ContentHash: contentHash, Body: plaintext, CreatedAt: createdAt,
	}, nil
}

// ListHistoryObjects returns one page of a conversation's history objects,
// decrypted, oldest first — keyed on (created_at, id) ascending, so a
// caller reading a long conversation walks forward through it rather than
// being silently cut off at a cap.
//
// The cap this replaces (MaxListLimit, 1000) was there because the query
// had no LIMIT at all and a large conversation buffered every encrypted row
// and then every decrypted plaintext body in one transaction. Both bounds
// still apply per call — paging.MaxLimit is the ceiling now — the
// difference is that the rows past it are reachable instead of invisible.
func (s *Service) ListHistoryObjects(ctx context.Context, principalID, projectID, conversationID uuid.UUID, opts paging.Options) (paging.Page[HistoryObject], error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapHistoryRead); err != nil {
		return paging.Page[HistoryObject]{}, err
	}
	cursorTime, cursorID, hasCursor, err := opts.DecodeTimeUUID()
	if err != nil {
		return paging.Page[HistoryObject]{}, err
	}
	limit := opts.ClampedLimit()

	out := make([]HistoryObject, 0, limit)
	var hasMore bool
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		// The envelope rows are fully buffered before any Decrypt call: pgx
		// refuses a second query (crypto.Decrypt needs one, to resolve the
		// project key) on the same tx while a Rows from an earlier query is
		// still open ("conn busy").
		type envelopeRow struct {
			h                             HistoryObject
			ciphertext, nonce, wrappedDEK []byte
			projectKeyVersion             int
		}
		var rowsBuf []envelopeRow

		rows, err := tx.Query(ctx,
			`SELECT id, project_id, conversation_id, external_session_id, type, derived_from, content_hash, ciphertext, nonce, wrapped_dek, project_key_version, created_at
			 FROM history_objects
			 WHERE project_id = $1 AND conversation_id = $2
			   AND (NOT $3::bool OR (created_at, id) > ($4::timestamptz, $5::uuid))
			 ORDER BY created_at ASC, id ASC LIMIT $6`,
			projectID, conversationID, hasCursor, cursorTime, cursorID, limit+1,
		)
		if err != nil {
			return fmt.Errorf("history: list: %w", err)
		}
		for rows.Next() {
			var r envelopeRow
			if err := rows.Scan(&r.h.ID, &r.h.ProjectID, &r.h.ConversationID, &r.h.ExternalSessionID, &r.h.Type, &r.h.DerivedFrom,
				&r.h.ContentHash, &r.ciphertext, &r.nonce, &r.wrappedDEK, &r.projectKeyVersion, &r.h.CreatedAt); err != nil {
				rows.Close()
				return fmt.Errorf("history: scan: %w", err)
			}
			rowsBuf = append(rowsBuf, r)
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return fmt.Errorf("history: iterate: %w", rowsErr)
		}

		// Trim the limit+1 probe row before decrypting: it exists only to
		// answer "is there another page", and decrypting a row the caller
		// will never see is work and an audit-relevant read for nothing.
		rowsBuf, hasMore = paging.More(rowsBuf, limit)

		for _, r := range rowsBuf {
			ec := crypto.EncryptionContext{
				ProjectID: projectID, ResourceType: "history_object",
				ResourceID: r.h.ID, ResourceVersion: 1,
			}
			body, err := s.crypto.Decrypt(ctx, tx, ec, r.ciphertext, r.nonce, r.wrappedDEK, r.projectKeyVersion)
			if err != nil {
				return fmt.Errorf("history: decrypt %s: %w", r.h.ID, err)
			}
			r.h.Body = body
			out = append(out, r.h)
		}
		return nil
	})
	if err != nil {
		return paging.Page[HistoryObject]{}, err
	}

	page := paging.Page[HistoryObject]{Items: out}
	if hasMore {
		last := out[len(out)-1]
		page.NextCursor = paging.EncodeTimeUUID(last.CreatedAt, last.ID)
	}
	return page, nil
}

// GetLineage returns the lineage graph edges for a conversation: one edge
// per history_objects.derived_from relationship, plus one edge per
// external_sessions parent/relation link. Two plain queries rather than a
// single recursive CTE — the edge set itself is what callers want, not a
// pre-walked path, and callers can traverse it however they need.
func (s *Service) GetLineage(ctx context.Context, principalID, projectID, conversationID uuid.UUID) ([]LineageEdge, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapHistoryRead); err != nil {
		return nil, err
	}

	var edges []LineageEdge
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT derived_from, id FROM history_objects WHERE project_id = $1 AND conversation_id = $2 AND derived_from IS NOT NULL`,
			projectID, conversationID,
		)
		if err != nil {
			return fmt.Errorf("history: lineage (objects): %w", err)
		}
		for rows.Next() {
			var e LineageEdge
			if err := rows.Scan(&e.FromID, &e.ToID); err != nil {
				rows.Close()
				return fmt.Errorf("history: scan lineage edge: %w", err)
			}
			e.Kind = "derived"
			edges = append(edges, e)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		rows, err = tx.Query(ctx,
			`SELECT parent_external_session_id, id, relation FROM external_sessions WHERE project_id = $1 AND conversation_id = $2 AND parent_external_session_id IS NOT NULL`,
			projectID, conversationID,
		)
		if err != nil {
			return fmt.Errorf("history: lineage (sessions): %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var e LineageEdge
			if err := rows.Scan(&e.FromID, &e.ToID, &e.Kind); err != nil {
				return fmt.Errorf("history: scan lineage edge: %w", err)
			}
			edges = append(edges, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return edges, nil
}
