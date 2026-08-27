// Package dbtest spins up a disposable Postgres container (via
// testcontainers-go) with the same role model as production/dev
// (deploy/postgres/init/01-roles.sql) and the current schema applied, for
// integration tests that need a real database — see
// docs/architecture-cookbook.md "Testování": real Postgres in CI, not
// mocks, because multi-tenant/RLS bugs only show up against the real thing.
package dbtest

import (
	"context"
	"fmt"
	"testing"

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

// New starts a Postgres container, applies deploy/postgres/init/01-roles.sql
// and every embedded migration, and registers cleanup with t.Cleanup. The
// returned DSNs are only valid for the lifetime of the test.
func New(t *testing.T) DSNs {
	t.Helper()
	ctx := context.Background()

	const (
		superuser     = "postgres"
		superPassword = "postgres_test_only" //nolint:gosec // disposable per-test container, not a real credential
		dbName        = "context_commander"
	)

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase(dbName),
		tcpostgres.WithUsername(superuser),
		tcpostgres.WithPassword(superPassword),
		tcpostgres.WithInitScripts("../../deploy/postgres/init/01-roles.sql"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("dbtest: start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("dbtest: terminate container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("dbtest: container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("dbtest: container port: %v", err)
	}

	dsns := DSNs{
		Owner: fmt.Sprintf("postgres://cc_owner:cc_owner_dev_password@%s:%s/%s?sslmode=disable", host, port.Port(), dbName),
		App:   fmt.Sprintf("postgres://cc_app:cc_app_dev_password@%s:%s/%s?sslmode=disable", host, port.Port(), dbName),
	}

	if err := db.Migrate(dsns.Owner); err != nil {
		t.Fatalf("dbtest: migrate: %v", err)
	}

	return dsns
}
