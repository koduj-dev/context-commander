package handoffs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
)

// Service is the authorization chokepoint for handoffs (docs/adr/0014):
// every method checks CapHandoffRead/CapHandoffWrite before touching data.
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
		return fmt.Errorf("handoffs: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// Create encrypts body as one JSON blob and inserts the handoff, publishing
// a "handoff.created" outbox event and an audit row in the same
// transaction — the same single-transaction mutation+audit+outbox pattern
// internal/artifacts.Service.Write uses.
func (s *Service) Create(
	ctx context.Context,
	principalID uuid.UUID, clientID *uuid.UUID,
	projectID uuid.UUID, sourceSessionID *uuid.UUID,
	body Body,
) (Handoff, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapHandoffWrite); err != nil {
		return Handoff{}, err
	}

	plaintext, err := json.Marshal(body)
	if err != nil {
		return Handoff{}, fmt.Errorf("handoffs: marshal body: %w", err)
	}

	id := uuid.New()
	var result Handoff

	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		ec := crypto.EncryptionContext{
			ProjectID: projectID, ResourceType: "handoff",
			ResourceID: id, ResourceVersion: 1,
		}
		ciphertext, nonce, wrappedDEK, projectKeyVersion, err := s.crypto.Encrypt(ctx, tx, ec, plaintext)
		if err != nil {
			return fmt.Errorf("handoffs: encrypt: %w", err)
		}

		createdAt := time.Now().UTC()
		_, err = tx.Exec(ctx,
			`INSERT INTO handoffs (id, project_id, created_by_principal, created_by_client, source_session_id, status, ciphertext, nonce, wrapped_dek, project_key_version, created_at)
			 VALUES ($1, $2, $3, $4, $5, 'open', $6, $7, $8, $9, $10)`,
			id, projectID, principalID, clientID, sourceSessionID, ciphertext, nonce, wrappedDEK, projectKeyVersion, createdAt,
		)
		if err != nil {
			return fmt.Errorf("handoffs: insert: %w", err)
		}

		eventID, err := events.Publish(ctx, tx, projectID, "handoff.created", "handoff", &id, events.PayloadKindMetadataOnly)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID, ClientID: clientID,
			Action: "handoff.create", ResourceType: "handoff", ResourceID: &id,
			Result: audit.ResultAllow, EventID: &eventID,
		}); err != nil {
			return err
		}

		result = Handoff{
			ID: id, ProjectID: projectID, CreatedByPrincipal: principalID, CreatedByClient: clientID,
			SourceSessionID: sourceSessionID, Status: "open", CreatedAt: createdAt,
		}
		return nil
	})
	if err != nil {
		return Handoff{}, err
	}
	return result, nil
}

// Get fetches and decrypts a handoff's body.
func (s *Service) Get(ctx context.Context, principalID, projectID, handoffID uuid.UUID) (Handoff, Body, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapHandoffRead); err != nil {
		return Handoff{}, Body{}, err
	}

	var h Handoff
	var body Body
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var ciphertext, nonce, wrappedDEK []byte
		var projectKeyVersion int
		err := tx.QueryRow(ctx,
			`SELECT id, project_id, created_by_principal, created_by_client, source_session_id, status, created_at, ciphertext, nonce, wrapped_dek, project_key_version
			 FROM handoffs WHERE id = $1 AND project_id = $2`,
			handoffID, projectID,
		).Scan(&h.ID, &h.ProjectID, &h.CreatedByPrincipal, &h.CreatedByClient, &h.SourceSessionID, &h.Status, &h.CreatedAt,
			&ciphertext, &nonce, &wrappedDEK, &projectKeyVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("handoffs: get: %w", err)
		}

		ec := crypto.EncryptionContext{
			ProjectID: projectID, ResourceType: "handoff",
			ResourceID: h.ID, ResourceVersion: 1,
		}
		plaintext, err := s.crypto.Decrypt(ctx, tx, ec, ciphertext, nonce, wrappedDEK, projectKeyVersion)
		if err != nil {
			return fmt.Errorf("handoffs: decrypt: %w", err)
		}
		return json.Unmarshal(plaintext, &body)
	})
	if err != nil {
		return Handoff{}, Body{}, err
	}
	return h, body, nil
}

// List returns metadata only — no decryption — mirroring how
// internal/artifacts.Service.List never returns content.
func (s *Service) List(ctx context.Context, principalID, projectID uuid.UUID, statusFilter *string) ([]Handoff, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapHandoffRead); err != nil {
		return nil, err
	}

	var out []Handoff
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, project_id, created_by_principal, created_by_client, source_session_id, status, created_at
			 FROM handoffs WHERE project_id = $1 AND ($2::text IS NULL OR status = $2) ORDER BY created_at DESC`,
			projectID, statusFilter,
		)
		if err != nil {
			return fmt.Errorf("handoffs: list: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var h Handoff
			if err := rows.Scan(&h.ID, &h.ProjectID, &h.CreatedByPrincipal, &h.CreatedByClient, &h.SourceSessionID, &h.Status, &h.CreatedAt); err != nil {
				return fmt.Errorf("handoffs: scan: %w", err)
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
