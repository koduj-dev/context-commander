// Package dbtest gives each integration test its own real Postgres
// database — see docs/testing.md "Why integration tests use a real Postgres,
// not mocks": real Postgres
// in CI, not mocks, because multi-tenant/RLS bugs only show up against the
// real thing.
//
// One container per test binary, one database per test. The distinction is
// the whole design: the isolation these tests need is isolation of *data*
// (so one test's rows and RLS decisions can't be seen by another), and that
// does not require a container each. It used to be a container each —
// ~2.4s of image start plus 18 migrations per test, 209 times across the
// repo, which was roughly eight minutes of the suite spent watching
// Postgres boot. Now the container starts once per package and every test
// gets a fresh database cloned from an already-migrated template, which
// costs milliseconds and isolates exactly as much.
//
// What is shared across tests in a package, and therefore must not be
// mutated by one: the cluster's roles (cc_owner/cc_app, created by
// deploy/postgres/init/01-roles.sql at initdb) and the template database.
// Everything a test writes lives in its own database and is dropped with it.
package dbtest

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/koduj-dev/context-commander/internal/db"
)

// DSNs holds connection strings for both DB roles a test needs — see
// docs/adr/0002-non-recursive-rls.md. Fixture setup (inserting rows to test
// against) should generally use Owner (RLS-exempt); the actual behavior
// under test should run against App, the same role the production server
// connects as, or an RLS test proves nothing.
type DSNs struct {
	Owner string
	App   string
}

const (
	superuser     = "postgres"
	superPassword = "postgres_test_only" //nolint:gosec // disposable per-run container, not a real credential

	// templateDB is migrated once and then only ever read as a CREATE
	// DATABASE template. Nothing connects to it afterwards — Postgres
	// refuses to copy a database that has other sessions attached.
	templateDB = "context_commander"
)

// cluster is the per-process Postgres. Started lazily on the first New and
// left running until the test binary exits, at which point testcontainers'
// reaper (Ryuk, started alongside it) removes it. There is no TestMain to
// hang an explicit shutdown on, and adding one to all nineteen packages
// that use this would be a lot of ceremony to duplicate what the reaper
// already does.
type cluster struct {
	host string
	port string
	// container runs pg_dump/pg_restore for DumpAndRestore: the client
	// tools that match the server are the ones inside its image.
	container *tcpostgres.PostgresContainer
}

var (
	startOnce  sync.Once
	shared     cluster
	startErr   error
	createMu   sync.Mutex
	dbSequence atomic.Uint64

	// The pgvector cluster (NewPgvector) is a second, separate container,
	// started only by the tests that ask for it.
	pgvectorOnce sync.Once
	pgvector     cluster
	pgvectorErr  error
)

func adminDSN(c cluster) string {
	// The "postgres" maintenance database, not the template: CREATE
	// DATABASE cannot run from inside the database being copied.
	return fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", superuser, superPassword, c.host, c.port)
}

// start brings up the container and migrates the template database. Runs
// at most once per test binary; every error it produces is replayed to
// every later caller, so a failure to start is never mistaken for a test
// failure in whichever test happened to be first.
func start(ctx context.Context) (cluster, error) {
	return startCluster(ctx, "postgres:16-alpine", nil, db.Migrate)
}

// startCluster is start for any image: the init scripts after the roles,
// and the migrations to apply to the template as cc_owner.
func startCluster(ctx context.Context, image string, initScripts []string, migrate func(ownerDSN string) error) (cluster, error) {
	container, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase(templateDB),
		tcpostgres.WithUsername(superuser),
		tcpostgres.WithPassword(superPassword),
		// Creates cc_owner/cc_app with the exact non-superuser attributes
		// production uses. Roles are cluster-wide, so this runs once and
		// serves every per-test database cloned below.
		tcpostgres.WithInitScripts(append([]string{"../../deploy/postgres/init/01-roles.sql"}, initScripts...)...),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return cluster{}, fmt.Errorf("start postgres container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return cluster{}, fmt.Errorf("container host: %w", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return cluster{}, fmt.Errorf("container port: %w", err)
	}
	c := cluster{host: host, port: port.Port(), container: container}

	// Migrate the template once, as cc_owner — the same role a real
	// deployment migrates as (docs/adr/0002), so the per-migration GRANTs
	// to cc_app land with the same ownership they would in production and
	// are copied into every clone.
	ownerDSN := fmt.Sprintf("postgres://cc_owner:cc_owner_dev_password@%s:%s/%s?sslmode=disable", c.host, c.port, templateDB)
	if err := migrate(ownerDSN); err != nil {
		return cluster{}, fmt.Errorf("migrate template: %w", err)
	}

	// golang-migrate closes its own connection, but a lingering backend
	// would make every CREATE DATABASE below fail with "source database is
	// being accessed by other users" — a confusing failure to debug from a
	// test, so close the door explicitly rather than trusting the timing.
	if err := disconnectTemplate(ctx, c); err != nil {
		return cluster{}, err
	}
	return c, nil
}

func disconnectTemplate(ctx context.Context, c cluster) error {
	conn, err := pgx.Connect(ctx, adminDSN(c))
	if err != nil {
		return fmt.Errorf("connect as superuser: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	_, err = conn.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		 WHERE datname = $1 AND pid <> pg_backend_pid()`, templateDB)
	if err != nil {
		return fmt.Errorf("detach template sessions: %w", err)
	}
	return nil
}

// New returns DSNs for a database of this test's own, cloned from the
// migrated template, and drops it when the test ends.
//
// The signature and contract are unchanged from when this started a whole
// container per test: callers get a private, fully migrated database and
// have nothing to clean up.
func New(t *testing.T) DSNs {
	t.Helper()
	ctx := context.Background()

	startOnce.Do(func() { shared, startErr = start(ctx) })
	if startErr != nil {
		t.Fatalf("dbtest: %v", startErr)
	}
	return newDatabase(t, shared)
}

// NewPgvector is New on a Postgres that has the pgvector extension
// (pgvector/pgvector:pg16), with the core migrations and the optional
// pgvector set applied (docs/adr/0028). The extension is created by the
// superuser through deploy/postgres/init/02-pgvector.sql, as in the compose
// deployment.
//
// It skips the test when that image cannot be started — pulled, say, on a
// machine without network — because pgvector storage is an opt-in backend
// and its absence on a developer machine says nothing about the code.
func NewPgvector(t *testing.T) DSNs {
	t.Helper()
	ctx := context.Background()
	pgvectorOnce.Do(func() {
		pgvector, pgvectorErr = startCluster(ctx, "pgvector/pgvector:pg16",
			[]string{"../../deploy/postgres/init/02-pgvector.sql"},
			func(ownerDSN string) error {
				if err := db.Migrate(ownerDSN); err != nil {
					return err
				}
				return db.MigratePgvector(ctx, ownerDSN)
			})
	})
	if pgvectorErr != nil {
		t.Skipf("dbtest: pgvector/pgvector:pg16 unavailable, skipping: %v", pgvectorErr)
	}
	return newDatabase(t, pgvector)
}

func newDatabase(t *testing.T, c cluster) DSNs {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("cc_test_%d", dbSequence.Add(1))
	if err := cloneTemplate(ctx, c, name); err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), c, name); err != nil {
			// Logged, not fatal: the database dies with the container a
			// few seconds later either way, and failing a test that
			// otherwise passed because of test-harness cleanup would be
			// its own kind of lie.
			t.Logf("dbtest: drop %s: %v", name, err)
		}
	})

	return DSNs{
		Owner: fmt.Sprintf("postgres://cc_owner:cc_owner_dev_password@%s:%s/%s?sslmode=disable", c.host, c.port, name),
		App:   fmt.Sprintf("postgres://cc_app:cc_app_dev_password@%s:%s/%s?sslmode=disable", c.host, c.port, name),
	}
}

// cloneTemplate creates one test's database. Serialized: Postgres allows
// only one CREATE DATABASE from a given template at a time, and tests in a
// package are free to call New from parallel subtests.
//
// OWNER cc_owner is explicit — without it the clone would belong to the
// superuser that created it, and cc_owner would no longer be the
// table-owning, RLS-exempt role the whole authorization model is built on
// (docs/adr/0002). A clone that quietly changed which role owns the schema
// would make RLS tests prove the wrong thing.
func cloneTemplate(ctx context.Context, c cluster, name string) error {
	createMu.Lock()
	defer createMu.Unlock()

	conn, err := pgx.Connect(ctx, adminDSN(c))
	if err != nil {
		return fmt.Errorf("connect as superuser: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	// Identifiers cannot be parameterized, so name is built here and never
	// taken from a test: it is always "cc_test_<counter>".
	_, err = conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s TEMPLATE %s OWNER cc_owner`,
		pgx.Identifier{name}.Sanitize(), pgx.Identifier{templateDB}.Sanitize()))
	if err != nil {
		return fmt.Errorf("clone template into %s: %w", name, err)
	}
	return nil
}

func dropDatabase(ctx context.Context, c cluster, name string) error {
	conn, err := pgx.Connect(ctx, adminDSN(c))
	if err != nil {
		return fmt.Errorf("connect as superuser: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	// WITH (FORCE) terminates whatever the test left connected. A test that
	// forgets to close a pool is a leak worth knowing about, but it is not
	// a reason to leave a database behind for every later test to trip over.
	_, err = conn.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, pgx.Identifier{name}.Sanitize()))
	return err
}

// DumpAndRestore backs up the database behind from and restores it into a
// new, empty one, the way docs/backup-restore.md says to: pg_dump as the
// superuser in the custom format, then pg_restore as the superuser with
// owners kept, into a database that belongs to cc_owner. It returns DSNs
// for the restored copy, which is dropped when the test ends.
//
// The target is created empty (from template1), not cloned from the
// migrated template, so everything in it comes from the dump.
func DumpAndRestore(t *testing.T, from DSNs) DSNs {
	t.Helper()
	ctx := context.Background()

	// Only the shared cluster: from must come from New, whose startOnce
	// is what makes reading shared here safe.
	c := shared
	if c.container == nil || !strings.Contains(from.Owner, ":"+c.port+"/") {
		t.Fatal("dbtest: DumpAndRestore needs a database from New")
	}
	u, err := url.Parse(from.Owner)
	if err != nil {
		t.Fatalf("dbtest: parse DSN: %v", err)
	}
	source := strings.TrimPrefix(u.Path, "/")
	target := fmt.Sprintf("cc_restored_%d", dbSequence.Add(1))
	dump := "/tmp/" + source + ".dump"

	run := func(cmd ...string) {
		t.Helper()
		code, out, err := c.container.Exec(ctx, cmd, tcexec.Multiplexed())
		if err != nil {
			t.Fatalf("dbtest: %s: %v", cmd[0], err)
		}
		if code != 0 {
			msg, _ := io.ReadAll(out)
			t.Fatalf("dbtest: %s exited %d: %s", strings.Join(cmd, " "), code, msg)
		}
	}

	run("pg_dump", "-U", superuser, "-d", source, "--format=custom", "-f", dump)

	// Under createMu like cloneTemplate: one CREATE DATABASE at a time.
	createMu.Lock()
	conn, err := pgx.Connect(ctx, adminDSN(c))
	if err == nil {
		_, err = conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s OWNER cc_owner`, pgx.Identifier{target}.Sanitize()))
		_ = conn.Close(ctx)
	}
	createMu.Unlock()
	if err != nil {
		t.Fatalf("dbtest: create %s: %v", target, err)
	}
	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), c, target); err != nil {
			t.Logf("dbtest: drop %s: %v", target, err)
		}
	})

	run("pg_restore", "-U", superuser, "-d", target, "--exit-on-error", "--single-transaction", dump)
	run("rm", "-f", dump)

	return DSNs{
		Owner: fmt.Sprintf("postgres://cc_owner:cc_owner_dev_password@%s:%s/%s?sslmode=disable", c.host, c.port, target),
		App:   fmt.Sprintf("postgres://cc_app:cc_app_dev_password@%s:%s/%s?sslmode=disable", c.host, c.port, target),
	}
}
