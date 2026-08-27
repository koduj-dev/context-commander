package db

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // registers the "postgres://" database driver
	"github.com/golang-migrate/migrate/v4/source/iofs"

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
