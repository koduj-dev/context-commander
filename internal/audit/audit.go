// Package audit defines the allowlisted audit log — see
// docs/adr/0011-encryption-classification.md. Columns are a fixed
// allowlist; there is deliberately no free-form jsonb/content column here.
// A sensitive payload that must accompany an audit row goes into
// encrypted_audit_payload as CONTENT PAYLOAD, never into this table.
package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Result string

const (
	ResultAllow Result = "allow"
	ResultDeny  Result = "deny"
	ResultError Result = "error"
)

type Entry struct {
	ProjectID    *uuid.UUID
	PrincipalID  *uuid.UUID
	ClientID     *uuid.UUID
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	Result       Result
	RequestID    *string
	EventID      *uuid.UUID
}

// Write inserts an audit_log row. Callers must pass tx from
// internal/db.WithPrincipal/WithoutPrincipal, in the SAME transaction as
// the mutation being audited — never as a separate step afterwards, or a
// crash between the mutation and the audit write would silently lose the
// record.
func Write(ctx context.Context, tx pgx.Tx, e Entry) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO audit_log (project_id, principal_id, client_id, action, resource_type, resource_id, result, request_id, event_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.ProjectID, e.PrincipalID, e.ClientID, e.Action, e.ResourceType, e.ResourceID, e.Result, e.RequestID, e.EventID,
	)
	if err != nil {
		return fmt.Errorf("audit: write: %w", err)
	}
	return nil
}
