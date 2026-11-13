package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // registers the "postgres://" database driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/migrations"
)

// Migrate applies all pending migrations from the embedded migrations.FS to
// ownerDSN. Must connect as cc_owner (or an equivalent migration role) —
// see docs/adr/0002-non-recursive-rls.md: cc_app never owns schema objects.
func Migrate(ownerDSN string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("db: load embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, ownerDSN)
	if err != nil {
		return fmt.Errorf("db: init migrator: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: apply migrations: %w", err)
	}
	return nil
}

// PgvectorMigrationsTable is the version table of the optional pgvector
// migration set (docs/adr/0028). Separate from golang-migrate's default
// schema_migrations, so the two sequences number independently and the
// core one never learns that the optional one exists.
const PgvectorMigrationsTable = "schema_migrations_pgvector"

// ErrPgvectorExtensionMissing is a database without the pgvector extension.
var ErrPgvectorExtensionMissing = errors.New("db: the pgvector extension is not installed in this database — " +
	"it is not a trusted extension, so a superuser must run `CREATE EXTENSION vector;` in it once " +
	"(the pgvector/pgvector:pg16 image ships it; deploy/postgres/init/02-pgvector.sql does this for a new compose cluster)")

// MigratePgvector applies the optional pgvector migration set after the
// core one. Run it only on an installation that chose
// --embeddings-store=pgvector: it creates a plaintext vector column, the
// documented exception to ADR 0011.
func MigratePgvector(ctx context.Context, ownerDSN string) error {
	conn, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		return fmt.Errorf("db: connect: %w", err)
	}
	var has bool
	err = conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector')`).Scan(&has)
	_ = conn.Close(ctx)
	if err != nil {
		return fmt.Errorf("db: check for pgvector: %w", err)
	}
	if !has {
		return ErrPgvectorExtensionMissing
	}

	src, err := iofs.New(migrations.PgvectorFS, "pgvector")
	if err != nil {
		return fmt.Errorf("db: load embedded pgvector migrations: %w", err)
	}
	dsn, err := withMigrationsTable(ownerDSN, PgvectorMigrationsTable)
	if err != nil {
		return err
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("db: init pgvector migrator: %w", err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: apply pgvector migrations: %w", err)
	}
	return nil
}

// withMigrationsTable points golang-migrate's postgres driver at another
// version table through its x-migrations-table parameter. URL-form DSNs
// only: golang-migrate itself takes nothing else.
func withMigrationsTable(dsn, table string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", fmt.Errorf("db: the pgvector migrations need a postgres:// URL, not a keyword/value connection string")
	}
	q := u.Query()
	q.Set("x-migrations-table", table)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// LatestPgvectorMigration is the highest version in the embedded pgvector
// set: what a database must be at for pgvector storage to be usable.
func LatestPgvectorMigration() (uint, error) {
	entries, err := fs.ReadDir(migrations.PgvectorFS, "pgvector")
	if err != nil {
		return 0, err
	}
	var versions []uint
	for _, e := range entries {
		head, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			continue
		}
		v, err := strconv.ParseUint(head, 10, 32)
		if err != nil {
			continue
		}
		versions = append(versions, uint(v))
	}
	if len(versions) == 0 {
		return 0, errors.New("db: no embedded pgvector migrations")
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions[len(versions)-1], nil
}

// CheckPgvectorSchema says whether the database behind pool has the
// pgvector set applied, cleanly, up to the version this binary embeds. The
// server calls it before it starts with --embeddings-store=pgvector, so a
// missing step is a refusal to start with the command that fixes it rather
// than a search that fails at the first request.
func CheckPgvectorSchema(ctx context.Context, p *Pool) error {
	want, err := LatestPgvectorMigration()
	if err != nil {
		return err
	}
	const hint = "run `ccd migrate --pgvector` with the owner connection (docs/semantic-search.md)"
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+PgvectorMigrationsTable).Scan(&exists); err != nil {
		return fmt.Errorf("db: check pgvector schema: %w", err)
	}
	if !exists {
		return fmt.Errorf("db: --embeddings-store=pgvector, but the pgvector schema is not in this database: %s", hint)
	}
	var version int64
	var dirty bool
	// The table name is a constant of this package, not input.
	err = p.pool.QueryRow(ctx, `SELECT version, dirty FROM `+PgvectorMigrationsTable+` LIMIT 1`).Scan(&version, &dirty)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("db: --embeddings-store=pgvector, but no pgvector migration has been applied: %s", hint)
	}
	if err != nil {
		return fmt.Errorf("db: read pgvector schema version (%s): %w", hint, err)
	}
	if dirty {
		return fmt.Errorf("db: the pgvector migration %d failed half-way and is marked dirty; fix the database, then %s", version, hint)
	}
	if version < int64(want) { //nolint:gosec // a small migration number
		return fmt.Errorf("db: the pgvector schema is at version %d and this binary needs %d: %s", version, want, hint)
	}
	return nil
}
