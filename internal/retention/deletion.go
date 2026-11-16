// Package retention is how project content leaves the system on purpose:
// an OWNER schedules a project for deletion, an operator's `ccd purge`
// removes what is due, and a legal hold stops both (docs/adr/0036).
//
// Scheduling, cancelling and listing run as cc_app through SECURITY DEFINER
// functions (migration 000047), because a project scheduled for deletion is
// hidden by row-level security from everyone, its OWNERs included. Purging
// and holding run as cc_owner: they delete rows the application may not
// touch.
package retention

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
)

// DefaultGrace is how long a scheduled deletion can still be cancelled.
const DefaultGrace = 7 * 24 * time.Hour

var (
	ErrForbidden    = errors.New("retention: only an OWNER can delete a project")
	ErrNeedsPerson  = errors.New("retention: only a person signed in to the interface can delete a project")
	ErrConfirmation = errors.New("retention: confirm by typing the project's path")
	ErrLegalHold    = errors.New("retention: the project is under legal hold and cannot be deleted")
	ErrState        = errors.New("retention: the project is not in a state that allows this")
)

// Pending is a project scheduled for deletion, as its OWNERs see it.
type Pending struct {
	ProjectID   uuid.UUID
	Path        string
	Name        string
	RequestedAt time.Time
	RequestedBy uuid.UUID
	PurgeAfter  time.Time
}

// Service schedules and cancels deletions.
type Service struct {
	pool  *db.Pool
	authz *auth.AuthzService
	grace time.Duration
}

func NewService(pool *db.Pool, authz *auth.AuthzService) *Service {
	return &Service{pool: pool, authz: authz, grace: DefaultGrace}
}

// SetGrace changes the grace period for new requests.
func (s *Service) SetGrace(d time.Duration) { s.grace = d }

// RequestDeletion schedules projectID for deletion. The caller must be an
// OWNER, signed in to the interface (an agent's token cannot delete a
// project), and must confirm with the project's path. The project vanishes
// for everyone at once and is purged after the grace period; its OWNERs can
// cancel until then.
func (s *Service) RequestDeletion(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID uuid.UUID, path, confirm string) (time.Time, error) {
	if strings.TrimSpace(confirm) != path {
		return time.Time{}, ErrConfirmation
	}
	allowed, err := s.authz.Can(ctx, principalID, projectID, auth.CapProjectDelete)
	if err != nil {
		return time.Time{}, fmt.Errorf("retention: authorization check: %w", err)
	}
	if !allowed {
		return time.Time{}, ErrForbidden
	}
	var purgeAfter time.Time
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := requirePerson(ctx, tx, principalID, clientID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT cc_request_project_deletion($1, make_interval(secs => $2))`,
			projectID, s.grace.Seconds()).Scan(&purgeAfter)
	})
	if err != nil {
		return time.Time{}, translate(err)
	}
	return purgeAfter, nil
}

// CancelDeletion brings a scheduled project back, for its OWNERs, until its
// grace period is over.
func (s *Service) CancelDeletion(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID uuid.UUID) error {
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := requirePerson(ctx, tx, principalID, clientID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `SELECT cc_cancel_project_deletion($1)`, projectID)
		return err
	})
	return translate(err)
}

// PendingDeletions lists the caller's own projects that wait for purge.
func (s *Service) PendingDeletions(ctx context.Context, principalID uuid.UUID) ([]Pending, error) {
	var out []Pending
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, namespace_slug, slug, name, requested_at, requested_by, purge_after FROM cc_pending_project_deletions()`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Pending, error) {
			var p Pending
			var ns, slug string
			err := r.Scan(&p.ProjectID, &ns, &slug, &p.Name, &p.RequestedAt, &p.RequestedBy, &p.PurgeAfter)
			p.Path = ns + "/" + slug
			return p, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("retention: pending deletions: %w", err)
	}
	return out, nil
}

// translate names the SQLSTATEs migration 000047's functions raise.
func translate(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "CCD01":
			return ErrForbidden
		case "CCD02":
			return ErrLegalHold
		case "CCD03":
			return fmt.Errorf("%w: %s", ErrState, pgErr.Message)
		}
	}
	if errors.Is(err, ErrNeedsPerson) {
		return err
	}
	return fmt.Errorf("retention: %w", err)
}

// requirePerson: a user principal on a browser session credential, the rule
// accepting an idea and sending a message use.
func requirePerson(ctx context.Context, tx pgx.Tx, principalID uuid.UUID, clientID *uuid.UUID) error {
	if clientID == nil {
		return ErrNeedsPerson
	}
	var kind, clientType string
	err := tx.QueryRow(ctx,
		`SELECT p.kind, c.client_type FROM client_credentials c JOIN principals p ON p.id = c.principal_id
		 WHERE c.id = $1 AND c.principal_id = $2`, *clientID, principalID,
	).Scan(&kind, &clientType)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNeedsPerson
	}
	if err != nil {
		return err
	}
	if kind != string(identity.PrincipalKindUser) || clientType != identity.SessionClientType {
		return ErrNeedsPerson
	}
	return nil
}
