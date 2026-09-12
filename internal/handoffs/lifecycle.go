package handoffs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
)

// selectColumns is the plaintext-safe column list every handoff read shares.
// Kept in one place so a lifecycle column added to the table can't be
// silently returned by Get but forgotten by List — the two used to drift
// exactly that way when the list was duplicated per query.
const selectColumns = `id, project_id, created_by_principal, created_by_client, source_session_id, status, created_at,
	        claimed_at, claimed_by_principal, claimed_by_client, closed_at, closed_by_principal, closed_by_client`

// scanHandoff reads selectColumns off one row, in that exact order.
func scanHandoff(row pgx.Row, h *Handoff) error {
	return row.Scan(&h.ID, &h.ProjectID, &h.CreatedByPrincipal, &h.CreatedByClient, &h.SourceSessionID, &h.Status, &h.CreatedAt,
		&h.ClaimedAt, &h.ClaimedByPrincipal, &h.ClaimedByClient, &h.ClosedAt, &h.ClosedByPrincipal, &h.ClosedByClient)
}

// transitionError turns "the conditional UPDATE matched no row" into the
// reason it matched none. Called only after a failed transition, in the
// same transaction, so the status it reports is the one that actually
// blocked the write rather than a re-read that could have moved again.
func transitionError(ctx context.Context, tx pgx.Tx, projectID, handoffID uuid.UUID, want string) error {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM handoffs WHERE id = $1 AND project_id = $2`, handoffID, projectID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("handoffs: read status: %w", err)
	}
	if want == StatusClaimed && status == StatusClaimed {
		return ErrAlreadyClaimed
	}
	return fmt.Errorf("%w: handoff is %s", ErrInvalidTransition, status)
}

// Claim takes responsibility for continuing a handoff's work: open →
// claimed, recording which principal and client took it.
//
// The status precondition lives in the UPDATE's WHERE clause, not in a
// read-then-write pair, so two clients racing to claim the same handoff
// serialize on the row lock and exactly one wins — the loser gets
// ErrAlreadyClaimed. That is the whole point of the state: a handoff is a
// coordination object between clients that never talk to each other, so the
// race is the normal case, not the exotic one.
//
// Claiming is a CapHandoffWrite operation, not a read: it changes what
// other agents will decide to do.
func (s *Service) Claim(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, handoffID uuid.UUID) (Handoff, error) {
	return s.transition(ctx, principalID, clientID, projectID, handoffID, StatusClaimed)
}

// Close declares a handoff's work finished: open or claimed → closed.
//
// Closing straight from open is allowed on purpose — a handoff can become
// obsolete (the work was done another way, or the plan changed) without any
// client ever claiming it, and forcing a claim first would mean recording
// an agent as having continued work it never touched.
func (s *Service) Close(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, handoffID uuid.UUID) (Handoff, error) {
	return s.transition(ctx, principalID, clientID, projectID, handoffID, StatusClosed)
}

func (s *Service) transition(
	ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID,
	projectID, handoffID uuid.UUID, target string,
) (Handoff, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapHandoffWrite); err != nil {
		return Handoff{}, err
	}

	var sql, eventType, auditAction string
	switch target {
	case StatusClaimed:
		sql = `UPDATE handoffs SET status = 'claimed', claimed_at = $1, claimed_by_principal = $2, claimed_by_client = $3
		       WHERE id = $4 AND project_id = $5 AND status = 'open'
		       RETURNING ` + selectColumns
		eventType, auditAction = "handoff.claimed", "handoff.claim"
	case StatusClosed:
		sql = `UPDATE handoffs SET status = 'closed', closed_at = $1, closed_by_principal = $2, closed_by_client = $3
		       WHERE id = $4 AND project_id = $5 AND status IN ('open', 'claimed')
		       RETURNING ` + selectColumns
		eventType, auditAction = "handoff.closed", "handoff.close"
	default:
		return Handoff{}, fmt.Errorf("handoffs: unknown target status %q", target)
	}

	var h Handoff
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		now := time.Now().UTC()
		err := scanHandoff(tx.QueryRow(ctx, sql, now, principalID, clientID, handoffID, projectID), &h)
		if errors.Is(err, pgx.ErrNoRows) {
			// No row matched: either the handoff doesn't exist (or isn't
			// visible under RLS), or its status wasn't one this transition
			// starts from. transitionError tells those apart.
			return transitionError(ctx, tx, projectID, handoffID, target)
		}
		if err != nil {
			return fmt.Errorf("handoffs: %s: %w", auditAction, err)
		}

		// Same single-transaction mutation+outbox+audit pattern Create
		// uses: a lifecycle change that isn't in the audit log is exactly
		// the provenance gap internal/audit exists to prevent.
		eventID, err := events.Publish(ctx, tx, projectID, eventType, "handoff", &handoffID, events.PayloadKindMetadataOnly)
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID, ClientID: clientID,
			Action: auditAction, ResourceType: "handoff", ResourceID: &handoffID,
			Result: audit.ResultAllow, EventID: &eventID,
		})
	})
	if err != nil {
		return Handoff{}, err
	}
	return h, nil
}

// Current returns the handoff a newly-arrived agent should continue from:
// the most recently created one still in play (open or claimed), metadata
// only. Returns ErrNotFound when every handoff in the project is closed —
// "nothing to take over" is a legitimate answer, and callers that want the
// closed history ask List for it.
//
// This exists because "which handoff am I supposed to read?" is otherwise a
// question only the human knows the answer to: an agent handed a project
// and a token has a list of UUIDs and no way to pick. Newest-still-open is
// the answer the workflow implies — a later handoff supersedes an earlier
// one that nobody closed.
func (s *Service) Current(ctx context.Context, principalID, projectID uuid.UUID) (Handoff, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapHandoffRead); err != nil {
		return Handoff{}, err
	}

	var h Handoff
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		err := scanHandoff(tx.QueryRow(ctx,
			`SELECT `+selectColumns+`
			 FROM handoffs
			 WHERE project_id = $1 AND status IN ('open', 'claimed')
			 ORDER BY created_at DESC, id DESC LIMIT 1`,
			projectID,
		), &h)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("handoffs: current: %w", err)
		}
		return nil
	})
	if err != nil {
		return Handoff{}, err
	}
	return h, nil
}
