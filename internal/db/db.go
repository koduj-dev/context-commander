// Package db provides the Postgres connection pool and the WithPrincipal
// transaction wrapper that sets RLS session context — see
// docs/adr/0002-non-recursive-rls.md and docs/adr/0003-set-local-transaction-context.md.
//
// No repository method, handler, or adapter outside this package is allowed
// to open a transaction or set app.current_principal_id for project-scoped
// work — see docs/adr/0003 for why that must be centralized.
package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Principal identifies who a database operation runs as — a User or a
// ServicePrincipal, see docs/adr/0004-principal-model.md. It intentionally
// lives here (not in internal/identity) so internal/db has no dependency on
// domain packages; internal/identity's Principal type is convertible to this
// one at the call site.
type Principal struct {
	ID uuid.UUID
}

// Pool wraps a pgxpool.Pool. Callers get transactions only through
// WithPrincipal, never a raw connection, so RLS session context can't be
// forgotten.
type Pool struct {
	pool *pgxpool.Pool
}

func NewPool(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse dsn: %w", err)
	}
	return NewPoolFromConfig(ctx, cfg)
}

// NewPoolFromConfig accepts a pre-built pgxpool.Config — e.g. to force
// MaxConns down for a test that needs to guarantee physical connection
// reuse (see db_test.go's SET LOCAL leak test), or to tune pool sizing in
// production.
func NewPoolFromConfig(ctx context.Context, cfg *pgxpool.Config) (*Pool, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return &Pool{pool: pool}, nil
}

func (p *Pool) Close() {
	p.pool.Close()
}

// setPrincipalSQL uses set_config(), not `SET LOCAL ... = $1` — Postgres's
// SET is a utility statement that does not accept a bound parameter for the
// value. set_config() is an ordinary function call with normal, safely
// bound parameters; is_local=true makes it behave exactly like SET LOCAL,
// scoped to the current transaction only. See docs/adr/0003.
const setPrincipalSQL = `SELECT set_config('app.current_principal_id', $1::text, true)`

// WithPrincipal runs fn inside a single Postgres transaction with the RLS
// session context set to principal for the lifetime of that transaction
// only. The connection returns to the pool with no residual state once the
// transaction commits or rolls back — see docs/adr/0003 for why a plain SET
// (session-level) would leak a principal across pooled connections.
func WithPrincipal(ctx context.Context, p *Pool, principal Principal, fn func(tx pgx.Tx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once Commit has succeeded

	if _, err := tx.Exec(ctx, setPrincipalSQL, principal.ID.String()); err != nil {
		return fmt.Errorf("db: set principal context: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}

// WithoutPrincipal runs fn inside a transaction with NO principal set — used
// only for operations that are inherently not project-scoped (e.g. creating
// the very first User during bootstrap, before any Principal exists). Every
// project-scoped repository method must go through WithPrincipal instead.
func WithoutPrincipal(ctx context.Context, p *Pool, fn func(tx pgx.Tx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}
