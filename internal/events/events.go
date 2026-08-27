// Package events implements the transactional outbox — see
// docs/adr/0013-transactional-outbox-and-storage-lifecycle.md. Publish must
// be called from inside the SAME database transaction as the domain
// mutation it describes; the feed/transport that actually delivers these to
// consumers (SSE/polling with a resume cursor) is Phase 9 — this package
// only owns the outbox table itself, which has to exist as early as the
// first mutation that needs it (Phase 3).
package events

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PayloadKind classifies what a consumer can expect in/via an event — see
// docs/adr/0011-encryption-classification.md.
type PayloadKind string

const (
	PayloadKindMetadataOnly     PayloadKind = "metadata_only"
	PayloadKindEncryptedPayload PayloadKind = "encrypted_payload_ref"
)

type Event struct {
	Seq          int64
	EventID      uuid.UUID
	ProjectID    uuid.UUID
	Type         string
	ResourceType string
	ResourceID   *uuid.UUID
	PayloadKind  PayloadKind
}

// Publish inserts a domain_events row and returns its event_id (for
// embedding in the audit_log row of the same transaction, tying the two
// together for provenance).
func Publish(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, eventType, resourceType string, resourceID *uuid.UUID, kind PayloadKind) (uuid.UUID, error) {
	var eventID uuid.UUID
	err := tx.QueryRow(ctx,
		`INSERT INTO domain_events (project_id, type, resource_type, resource_id, payload_kind)
		 VALUES ($1, $2, $3, $4, $5) RETURNING event_id`,
		projectID, eventType, resourceType, resourceID, kind,
	).Scan(&eventID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("events: publish: %w", err)
	}
	return eventID, nil
}

// ListSince returns up to limit events for projectID with seq > afterSeq,
// ordered by seq — the primitive Phase 9's feed adapter will wrap with an
// actual transport. Exercised directly by this package's own tests in the
// meantime so the outbox's correctness doesn't wait for Phase 9 to prove it.
func ListSince(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, afterSeq int64, limit int) ([]Event, error) {
	rows, err := tx.Query(ctx,
		`SELECT seq, event_id, project_id, type, resource_type, resource_id, payload_kind
		 FROM domain_events WHERE project_id = $1 AND seq > $2 ORDER BY seq ASC LIMIT $3`,
		projectID, afterSeq, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("events: list: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Seq, &e.EventID, &e.ProjectID, &e.Type, &e.ResourceType, &e.ResourceID, &e.PayloadKind); err != nil {
			return nil, fmt.Errorf("events: scan: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("events: iterate: %w", err)
	}
	return out, nil
}
