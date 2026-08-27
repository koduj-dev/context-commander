# 0003. RLS principal context: `SET LOCAL` inside a transaction, set from exactly one place

## Status

Accepted (Postgres backend only)

## Context

[0002](0002-non-recursive-rls.md)'s RLS policies read `current_setting('app.current_principal_id')`.
That value has to come from somewhere, and how it gets set matters more than it looks: an ordinary
`SET app.current_principal_id = ...` is **session-level**. With a connection pool (`pgxpool`), a
"session" is a physical connection that gets handed back to the pool and reused by a *different,
unrelated request*. A plain `SET` would leave the previous request's principal attached to that
connection — the next request to grab it from the pool could silently run under someone else's
identity from RLS's point of view. This is close to a blocker, not a style preference.

## Decision

- The principal is set with `SET LOCAL`, which only lives for the duration of the current
  transaction and is automatically cleared on `COMMIT`/`ROLLBACK` — the connection returns to the
  pool with no residual state.
- Every project-scoped database operation runs inside a transaction opened by
  `internal/db.WithPrincipal`, and the principal is set **only** inside that function — via
  `set_config()`, not a literal `SET LOCAL ... = $1` statement. Postgres's `SET` is a utility
  statement processed outside the normal extended-query/parameter-binding path, so it does not
  accept a `$1` placeholder for the value (drivers either reject it or you'd be forced into unsafe
  string formatting). `set_config(setting_name, new_value, is_local)` is an ordinary SQL function —
  it takes normal, safely-bound parameters — and `is_local = true` makes it behave exactly like
  `SET LOCAL`, scoped to the current transaction:

```go
// internal/db
func WithPrincipal(ctx context.Context, pool *pgxpool.Pool, principal Principal, fn func(pgx.Tx) error) error {
    tx, err := pool.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx) // no-op once Commit has succeeded

    const setPrincipal = `SELECT set_config('app.current_principal_id', $1::text, true)`
    if _, err := tx.Exec(ctx, setPrincipal, principal.ID.String()); err != nil {
        return err
    }
    if err := fn(tx); err != nil {
        return err
    }
    return tx.Commit(ctx)
}
```

- No repository method, handler, or adapter is allowed to call `tx.Exec(ctx, "SET ...")` /
  `set_config(...)` directly, or begin a transaction on its own for project-scoped work.
  `golangci-lint` custom rule / code review checklist item: any `pool.Begin(`, `pool.Exec(ctx, "SET`,
  or `set_config(` outside `internal/db` is a bug.

## Consequences

- Repository interfaces (see [0007](0007-metadata-store-abstraction-sqlite.md)) take a `pgx.Tx` (or
  an equivalent `Querier` interface), never a `*pgxpool.Pool` directly, so a repository call outside
  `WithPrincipal` doesn't compile against the Postgres implementation.
- The negative test suite must include: two goroutines/requests interleaved on a small pool
  (`pgxpool.Config.MaxConns` set low in the test) proving principal A's `SET LOCAL` never leaks
  into principal B's query on a reused connection.
