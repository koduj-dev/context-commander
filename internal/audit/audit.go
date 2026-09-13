// Package audit defines the allowlisted audit log — see
// docs/adr/0011-encryption-classification.md. Columns are a fixed
// allowlist; there is deliberately no free-form jsonb/content column here.
// A sensitive payload that must accompany an audit row goes into
// encrypted_audit_payload as CONTENT PAYLOAD, never into this table.
package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/paging"
)

type Result string

const (
	ResultAllow Result = "allow"
	ResultDeny  Result = "deny"
	ResultError Result = "error"
)

type Entry struct {
	// Exactly one of ProjectID / OrganizationID is set — the scope the
	// audited change belongs to, and therefore who may read the row
	// (migrations/000023). Organizations are a peer scope to projects
	// (docs/adr/0006), not something nested below them.
	ProjectID      *uuid.UUID
	OrganizationID *uuid.UUID
	PrincipalID    *uuid.UUID
	ClientID       *uuid.UUID
	Action         string
	ResourceType   string
	ResourceID     *uuid.UUID
	Result         Result
	RequestID      *string
	EventID        *uuid.UUID
}

// Write inserts an audit_log row. Callers must pass tx from
// internal/db.WithPrincipal/WithoutPrincipal, in the SAME transaction as
// the mutation being audited — never as a separate step afterwards, or a
// crash between the mutation and the audit write would silently lose the
// record.
func Write(ctx context.Context, tx pgx.Tx, e Entry) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO audit_log (project_id, organization_id, principal_id, client_id, action, resource_type, resource_id, result, request_id, event_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		e.ProjectID, e.OrganizationID, e.PrincipalID, e.ClientID, e.Action, e.ResourceType, e.ResourceID, e.Result, e.RequestID, e.EventID,
	)
	if err != nil {
		return fmt.Errorf("audit: write: %w", err)
	}
	return nil
}

// Record is one row read back from the audit log — Entry plus the two
// columns Write's caller never needs to supply (they're assigned by the
// database).
type Record struct {
	ID        int64
	Timestamp time.Time
	Entry
}

// QueryOptions filters a Query call. Action/ResourceType are exact-match,
// empty means "no filter"; Since/Until are inclusive bounds on ts. Paging
// carries the limit and the cursor, the same opaque token every other list
// in this codebase uses (internal/paging) — audit predates that package and
// used a raw before_id, which made it the one endpoint a client had to page
// differently from all the others.
type QueryOptions struct {
	Action       string
	ResourceType string
	Since        *time.Time
	Until        *time.Time
	Paging       paging.Options
}

// Query returns audit_log rows for projectID, most recent first. Every
// caller-facing entry point (adapters, future internal/audit.Service) must
// scope this by project — there is no unscoped "query everything" here,
// matching the same project-first shape as every other domain's List.
func Query(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, opts QueryOptions) (paging.Page[Record], error) {
	beforeID, hasCursor, err := opts.Paging.DecodeSeq()
	if err != nil {
		return paging.Page[Record]{}, err
	}
	limit := opts.Paging.ClampedLimit()

	query := `SELECT id, ts, project_id, principal_id, client_id, action, resource_type, resource_id, result, request_id, event_id
		FROM audit_log WHERE project_id = $1`
	args := []any{projectID}

	if opts.Action != "" {
		args = append(args, opts.Action)
		query += fmt.Sprintf(" AND action = $%d", len(args))
	}
	if opts.ResourceType != "" {
		args = append(args, opts.ResourceType)
		query += fmt.Sprintf(" AND resource_type = $%d", len(args))
	}
	if opts.Since != nil {
		args = append(args, *opts.Since)
		query += fmt.Sprintf(" AND ts >= $%d", len(args))
	}
	if opts.Until != nil {
		args = append(args, *opts.Until)
		query += fmt.Sprintf(" AND ts <= $%d", len(args))
	}
	if hasCursor {
		// A plain "id < $N" against ORDER BY ts DESC, id DESC is a
		// deliberate simplification, not a strict (ts, id) keyset: id is a
		// bigserial that only ever increases with insertion order, and ts
		// is assigned at insertion too, so "id < last seen id" and
		// "(ts, id) < (last seen ts, last seen id)" agree in practice. A
		// stricter composite cursor isn't worth it unless that stops
		// holding (e.g. ts is ever backfilled/edited).
		args = append(args, beforeID)
		query += fmt.Sprintf(" AND id < $%d", len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY ts DESC, id DESC LIMIT $%d", len(args))

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return paging.Page[Record]{}, fmt.Errorf("audit: query: %w", err)
	}
	defer rows.Close()

	out := make([]Record, 0, limit)
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.Timestamp, &r.ProjectID, &r.PrincipalID, &r.ClientID,
			&r.Action, &r.ResourceType, &r.ResourceID, &r.Result, &r.RequestID, &r.EventID); err != nil {
			return paging.Page[Record]{}, fmt.Errorf("audit: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return paging.Page[Record]{}, fmt.Errorf("audit: iterate: %w", err)
	}

	items, hasMore := paging.More(out, limit)
	page := paging.Page[Record]{Items: items}
	if hasMore {
		page.NextCursor = paging.EncodeSeq(items[len(items)-1].ID)
	}
	return page, nil
}

// WriteWithPayload is Write plus a CONTENT PAYLOAD side table for callers
// that need to attach a sensitive payload to an audit row (e.g. what
// changed, not just that something changed) — see
// docs/adr/0011-encryption-classification.md: this table exists precisely
// so that payload never has to go into audit_log's fixed allowlisted
// columns. e.ProjectID is required (crypto.EncryptionContext's key
// hierarchy is per-project — see docs/adr/0005) even though it's optional
// on a plain Write.
func WriteWithPayload(ctx context.Context, tx pgx.Tx, e Entry, payload []byte, cryptoProvider *crypto.Provider) error {
	if e.ProjectID == nil {
		return fmt.Errorf("audit: write with payload: ProjectID is required")
	}

	var auditLogID int64
	err := tx.QueryRow(ctx,
		`INSERT INTO audit_log (project_id, organization_id, principal_id, client_id, action, resource_type, resource_id, result, request_id, event_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		e.ProjectID, e.OrganizationID, e.PrincipalID, e.ClientID, e.Action, e.ResourceType, e.ResourceID, e.Result, e.RequestID, e.EventID,
	).Scan(&auditLogID)
	if err != nil {
		return fmt.Errorf("audit: write with payload: insert audit_log: %w", err)
	}

	var resourceID uuid.UUID
	if e.ResourceID != nil {
		resourceID = *e.ResourceID
	}
	ec := crypto.EncryptionContext{
		ProjectID:       *e.ProjectID,
		ResourceType:    "audit_payload",
		ResourceID:      resourceID,
		ResourceVersion: uint64(auditLogID), //nolint:gosec // auditLogID is a small positive bigserial counter
	}
	ciphertext, nonce, wrappedDEK, projectKeyVersion, err := cryptoProvider.Encrypt(ctx, tx, ec, payload)
	if err != nil {
		return fmt.Errorf("audit: write with payload: encrypt: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO encrypted_audit_payload (audit_log_id, ciphertext, nonce, wrapped_dek, project_key_version)
		 VALUES ($1, $2, $3, $4, $5)`,
		auditLogID, ciphertext, nonce, wrappedDEK, projectKeyVersion,
	)
	if err != nil {
		return fmt.Errorf("audit: write with payload: insert encrypted_audit_payload: %w", err)
	}
	return nil
}

// ReadPayload decrypts the payload WriteWithPayload attached to auditLogID.
// projectID and resourceID must match what WriteWithPayload was called
// with for that row — crypto.EncryptionContext binds ciphertext to its
// exact (project, resource type/id/version) tuple, so any mismatch fails
// authentication rather than returning wrong/stale plaintext.
func ReadPayload(ctx context.Context, tx pgx.Tx, cryptoProvider *crypto.Provider, projectID uuid.UUID, auditLogID int64, resourceID uuid.UUID) ([]byte, error) {
	var ciphertext, nonce, wrappedDEK []byte
	var projectKeyVersion int
	err := tx.QueryRow(ctx,
		`SELECT ciphertext, nonce, wrapped_dek, project_key_version FROM encrypted_audit_payload WHERE audit_log_id = $1`,
		auditLogID,
	).Scan(&ciphertext, &nonce, &wrappedDEK, &projectKeyVersion)
	if err != nil {
		return nil, fmt.Errorf("audit: read payload: %w", err)
	}

	ec := crypto.EncryptionContext{
		ProjectID: projectID, ResourceType: "audit_payload",
		ResourceID: resourceID, ResourceVersion: uint64(auditLogID), //nolint:gosec // auditLogID is a small positive bigserial counter
	}
	plaintext, err := cryptoProvider.Decrypt(ctx, tx, ec, ciphertext, nonce, wrappedDEK, projectKeyVersion)
	if err != nil {
		return nil, fmt.Errorf("audit: decrypt payload: %w", err)
	}
	return plaintext, nil
}
