// Package security records what the system REFUSED — failed
// authentication, denied capabilities, throttled callers — durably and
// queryably, separate from internal/audit's project-scoped record of what
// it allowed.
//
// Two things drive it, and they want the same table:
//
//   - Incident analysis. "Which credential tried what, in what order,
//     before this went wrong" is unanswerable if refusals are never
//     written down. Until this package existed, audit.ResultDeny was a
//     defined constant with zero call sites: nothing in the entire server
//     recorded a denial, and nothing logged one either.
//   - CRA Annex I (2)(l) — a product must "provide security related
//     information by recording and monitoring relevant internal activity,
//     including the access to or modification of data, services or
//     functions, with an opt-out mechanism for the user". Recorder honours
//     that opt-out; see NewRecorder.
//
// Writes are best-effort and never block the path that produced them: a
// database problem must not turn a 401 into a 500, and must certainly not
// turn a *denial* into an allow.
package security

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

// Kind is what was refused. The set is closed and matches
// migrations/000019's CHECK constraint.
type Kind string

const (
	// KindAuthenticationFailed is a presented token that did not resolve to
	// an active credential — malformed, unknown, revoked or expired.
	KindAuthenticationFailed Kind = "authentication_failed"
	// KindAuthorizationDenied is an authenticated principal refused a
	// capability on a project (internal/auth.AuthzService.Can).
	KindAuthorizationDenied Kind = "authorization_denied"
	// KindRateLimited is a caller refused because it exceeded its rate
	// limit — recorded because a client suddenly hitting its ceiling is
	// either broken or not the client it claims to be, and both are worth
	// seeing in the same timeline as the rest.
	KindRateLimited Kind = "rate_limited"
)

var ErrForbidden = errors.New("security: forbidden")

// Event is one refusal.
type Event struct {
	ID   int64
	TS   time.Time
	Kind Kind

	// PrincipalID/CredentialID/ProjectID are what the caller PRESENTED, not
	// proof that any of them exist — an unknown credential id is the most
	// interesting row in the table (migrations/000019 deliberately has no
	// foreign keys here).
	PrincipalID  *uuid.UUID
	CredentialID *uuid.UUID
	ProjectID    *uuid.UUID

	Capability string
	Reason     string
	RequestID  string
}

// Recorder writes events. Construct one per process and share it; it holds
// no state beyond the pool and the opt-out flag.
type Recorder struct {
	pool    *db.Pool
	enabled bool
}

// NewRecorder returns a Recorder. enabled=false is the CRA Annex I opt-out:
// every Record call becomes a no-op, nothing is written, and the operator
// has been told (in the serve flag's own help text and in the docs) that
// turning it off is what costs them the ability to see attacks and
// reconstruct incidents. It is off-switch, not sampling — a half-recorded
// security timeline is worse than an honestly absent one, because it reads
// as complete.
func NewRecorder(pool *db.Pool, enabled bool) *Recorder {
	return &Recorder{pool: pool, enabled: enabled}
}

// Record writes one event and logs it.
//
// Best-effort by construction: it takes no error return, because every
// caller is on a path that has already decided to refuse the request and
// none of them may fail differently because the recording failed. A write
// error is logged and dropped.
//
// It deliberately does NOT run inside the caller's transaction. A denial is
// frequently produced inside a transaction that is about to be rolled back
// (a capability check inside a domain mutation), and a record that
// disappears with the rollback would be a record of nothing.
func (r *Recorder) Record(ctx context.Context, e Event) {
	if r == nil || !r.enabled {
		return
	}

	// Logged first and unconditionally: stderr is the one sink that still
	// works when the database is the thing having the problem, and an
	// operator streaming logs wants these without polling a table.
	slog.Warn("security event",
		"kind", string(e.Kind),
		"principal_id", uuidValue(e.PrincipalID),
		"credential_id", uuidValue(e.CredentialID),
		"project_id", uuidValue(e.ProjectID),
		"capability", e.Capability,
		"reason", e.Reason,
		"request_id", e.RequestID,
	)

	// WithoutPrincipal: the caller frequently has no valid principal (that
	// is the point of an authentication failure), and this table has no RLS
	// to satisfy anyway (migrations/000019).
	err := db.WithoutPrincipal(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO security_events (kind, principal_id, credential_id, project_id, capability, reason, request_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			string(e.Kind), e.PrincipalID, e.CredentialID, e.ProjectID,
			nullIfEmpty(e.Capability), e.Reason, nullIfEmpty(e.RequestID),
		)
		return err
	})
	if err != nil {
		slog.Error("could not record security event", "kind", string(e.Kind), "error", err)
	}
}

func uuidValue(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
