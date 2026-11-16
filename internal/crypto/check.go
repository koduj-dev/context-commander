package crypto

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/koduj-dev/context-commander/internal/db"
)

// ErrWrongRootKey means the database holds project keys and none of them is
// wrapped under the current root key: nothing encrypted can be read.
var ErrWrongRootKey = errors.New("crypto: wrong root key")

// RootKeyUse is how many project keys one root key wraps.
type RootKeyUse struct {
	Ref         string
	ProjectKeys int64
}

// RootKeyCheck is what the database says about the current root key.
type RootKeyCheck struct {
	Current  string       // the current key's fingerprint
	Matching int64        // project keys wrapped under it
	Others   []RootKeyUse // project keys wrapped under any other key
}

// Fresh reports an installation that has encrypted nothing yet.
func (c RootKeyCheck) Fresh() bool { return c.Matching == 0 && len(c.Others) == 0 }

// CheckRootKey compares the current root key with the fingerprints the
// project keys were wrapped under (project_keys.root_key_ref). It returns
// ErrWrongRootKey, with both fingerprints in the message, when there are
// project keys and not one matches. Some matching and some not is an
// unfinished rotation: no error, and Others names the rest.
//
// It reads through cc_root_key_refs(), so it works as cc_app with no
// principal, before anything listens.
func CheckRootKey(ctx context.Context, pool *db.Pool, root RootKeyProvider) (RootKeyCheck, error) {
	_, current, err := root.CurrentKey(ctx)
	if err != nil {
		return RootKeyCheck{}, fmt.Errorf("crypto: read root key: %w", err)
	}
	check := RootKeyCheck{Current: current}
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT root_key_ref, project_keys FROM cc_root_key_refs()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var use RootKeyUse
			if err := rows.Scan(&use.Ref, &use.ProjectKeys); err != nil {
				return err
			}
			if use.Ref == current {
				check.Matching = use.ProjectKeys
			} else {
				check.Others = append(check.Others, use)
			}
		}
		return rows.Err()
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42883" { // undefined_function
			return RootKeyCheck{}, fmt.Errorf("crypto: check root key: the database schema is older than this ccd, run `ccd migrate` first: %w", err)
		}
		return RootKeyCheck{}, fmt.Errorf("crypto: check root key: %w", err)
	}
	if check.Matching == 0 && len(check.Others) > 0 {
		return check, fmt.Errorf(
			"%w: CC_ROOT_KEY has fingerprint %q, but the project keys are wrapped under %s. "+
				"Start with the key the data was written under (after a restore, the key the backup was taken with; "+
				"docs/backup-restore.md shows how to compare fingerprints)",
			ErrWrongRootKey, current, describeUses(check.Others))
	}
	return check, nil
}

func describeUses(uses []RootKeyUse) string {
	parts := make([]string, len(uses))
	for i, u := range uses {
		parts[i] = fmt.Sprintf("%q (%d)", u.Ref, u.ProjectKeys)
	}
	return strings.Join(parts, ", ")
}

// RotateInstallationRootKey is RotateRootKey for the whole installation, as
// `ccd root-key rotate` runs it: every project key wrapped under previous is
// rewrapped under p's current key, in one transaction, and it returns how
// many were.
//
// pool must connect as cc_owner. Through cc_app, row-level security would
// hide every project key, and RotateRootKey would rewrap none of them and
// report success; so it first compares what the connection sees with what
// cc_root_key_refs() counts, and refuses if they differ. The table is locked
// against new project keys for the length of the transaction.
func (p *Provider) RotateInstallationRootKey(ctx context.Context, pool *db.Pool, previous RootKeyProvider) (int64, error) {
	_, newRef, err := p.root.CurrentKey(ctx)
	if err != nil {
		return 0, fmt.Errorf("crypto: read new root key: %w", err)
	}
	_, oldRef, err := previous.CurrentKey(ctx)
	if err != nil {
		return 0, fmt.Errorf("crypto: read previous root key: %w", err)
	}
	if newRef == oldRef {
		return 0, fmt.Errorf("crypto: the previous and the new root key are the same key (%q)", newRef)
	}
	var rewrapped int64
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE project_keys IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return fmt.Errorf("crypto: lock project keys (connect as cc_owner): %w", err)
		}
		var visible, total int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_keys`).Scan(&visible); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(project_keys), 0)::bigint FROM cc_root_key_refs()`).Scan(&total); err != nil {
			return err
		}
		if visible != total {
			return fmt.Errorf("crypto: this connection sees %d of %d project keys: connect as cc_owner, the role `ccd migrate` uses", visible, total)
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_keys WHERE root_key_ref = $1`, oldRef).Scan(&rewrapped); err != nil {
			return err
		}
		return p.RotateRootKey(ctx, tx, previous)
	})
	if err != nil {
		return 0, err
	}
	return rewrapped, nil
}
