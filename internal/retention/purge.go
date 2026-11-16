package retention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/storage"
)

// DefaultDeletedArtifactDays is how long a deleted artifact's versions stay
// in storage before `ccd purge` removes them. Until then an operator can
// still bring one back from the database and the store.
const DefaultDeletedArtifactDays = 30

// keptTables have a project_id but are not the project's content and stay
// after a purge: the installation's security log names projects by id and
// holds no content (migration 000019), and project_purges is the record of
// the purge itself.
var keptTables = map[string]bool{"security_events": true, "projects": true, "project_purges": true}

// Purger removes what is due. It runs as cc_owner (`ccd purge`): it deletes
// rows the application may not, artifact versions above all.
type Purger struct {
	pool  *db.Pool
	store storage.ObjectStore
	now   func() time.Time
}

func NewPurger(pool *db.Pool, store storage.ObjectStore) *Purger {
	return &Purger{pool: pool, store: store, now: time.Now}
}

// PurgeReport is what one run removed, or would remove on a dry run.
type PurgeReport struct {
	Projects  []string // paths
	Artifacts int
	Objects   int
	HeldSkips int // due, and left alone because of a legal hold
	DryRun    bool
}

// Run purges every project whose grace period is over and every artifact
// deleted more than deletedArtifactDays ago (0 keeps them forever), except
// in projects under legal hold. With dryRun it only counts.
func (p *Purger) Run(ctx context.Context, deletedArtifactDays int, dryRun bool) (PurgeReport, error) {
	r := PurgeReport{DryRun: dryRun}
	type due struct {
		id   uuid.UUID
		path string
		held bool
	}
	var projects []due
	err := db.WithoutPrincipal(ctx, p.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT p.id, n.slug || '/' || p.slug, p.legal_hold FROM projects p JOIN namespaces n ON n.id = p.namespace_id
			 WHERE p.purge_after IS NOT NULL AND p.purge_after <= $1`, p.now())
		if err != nil {
			return err
		}
		projects, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (due, error) {
			var d due
			err := row.Scan(&d.id, &d.path, &d.held)
			return d, err
		})
		return err
	})
	if err != nil {
		return r, fmt.Errorf("retention: list due projects: %w", err)
	}
	for _, d := range projects {
		if d.held {
			r.HeldSkips++
			continue
		}
		r.Projects = append(r.Projects, d.path)
		if dryRun {
			continue
		}
		n, err := p.PurgeProject(ctx, d.id, "grace period over")
		if err != nil {
			return r, err
		}
		r.Objects += n
	}

	if deletedArtifactDays > 0 {
		cutoff := p.now().Add(-time.Duration(deletedArtifactDays) * 24 * time.Hour)
		var ids []uuid.UUID
		err := db.WithoutPrincipal(ctx, p.pool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx,
				`SELECT a.id FROM artifacts a JOIN projects p ON p.id = a.project_id
				 WHERE a.deleted_at IS NOT NULL AND a.deleted_at < $1 AND NOT p.legal_hold`, cutoff)
			if err != nil {
				return err
			}
			ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			return err
		})
		if err != nil {
			return r, fmt.Errorf("retention: list deleted artifacts: %w", err)
		}
		r.Artifacts = len(ids)
		if !dryRun {
			for _, id := range ids {
				var keys []string
				err := db.WithoutPrincipal(ctx, p.pool, func(tx pgx.Tx) error {
					var err error
					keys, err = artifacts.PurgeArtifact(ctx, tx, id)
					return err
				})
				if err != nil {
					return r, err
				}
				// After the commit, as docs/adr/0013 orders it: an object
				// whose row is gone is an orphan a sweep can still find; a
				// row whose object is gone is a broken read.
				if err := artifacts.DeleteStorageKeys(ctx, p.store, keys); err != nil {
					return r, err
				}
				r.Objects += len(keys)
			}
		}
	}
	return r, nil
}

// ErrHeld is a purge refused because the project is under legal hold.
var ErrHeld = errors.New("retention: the project is under legal hold")

// PurgeProject removes a project and everything in it now, whatever its
// state, unless it is held: every row of every table with a project_id
// (except keptTables), its project-scoped skill and gate bindings, and every
// object under its prefix in the store. It leaves a row in project_purges.
// It returns how many stored objects it deleted.
//
// The tables are found in the catalogue rather than listed here, so a table
// added later is purged without anyone remembering to add it; they are
// deleted in rounds until the foreign keys between them allow every one.
func (p *Purger) PurgeProject(ctx context.Context, projectID uuid.UUID, reason string) (int, error) {
	err := db.WithoutPrincipal(ctx, p.pool, func(tx pgx.Tx) error {
		var held bool
		var path, name string
		var requestedBy *uuid.UUID
		var requestedAt *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT p.legal_hold, n.slug || '/' || p.slug, p.name, p.deletion_requested_by, p.deletion_requested_at
			 FROM projects p JOIN namespaces n ON n.id = p.namespace_id WHERE p.id = $1 FOR UPDATE OF p`, projectID,
		).Scan(&held, &path, &name, &requestedBy, &requestedAt); err != nil {
			return fmt.Errorf("read project: %w", err)
		}
		if held {
			return ErrHeld
		}
		tables, err := projectTables(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM skill_bindings WHERE scope = 'project' AND scope_ref = $1`, projectID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM gate_bindings WHERE scope = 'project' AND scope_ref = $1`, projectID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE skill_versions SET published_in_project = NULL WHERE published_in_project = $1`, projectID); err != nil {
			return err
		}
		if err := deleteInRounds(ctx, tx, tables, projectID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
			return fmt.Errorf("delete project: %w", err)
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO project_purges (project_id, path, name, requested_by, requested_at, reason) VALUES ($1, $2, $3, $4, $5, $6)`,
			projectID, path, name, requestedBy, requestedAt, reason)
		return err
	})
	if err != nil {
		if errors.Is(err, ErrHeld) {
			return 0, err
		}
		return 0, fmt.Errorf("retention: purge project %s: %w", projectID, err)
	}
	// Everything under the prefix: every version, and any orphan an
	// interrupted write left.
	objects, err := p.store.List(ctx, fmt.Sprintf("projects/%s/", projectID))
	if err != nil {
		return 0, fmt.Errorf("retention: list objects of %s: %w", projectID, err)
	}
	for _, o := range objects {
		if err := p.store.Delete(ctx, o.Key); err != nil {
			return 0, fmt.Errorf("retention: delete object %s: %w", o.Key, err)
		}
	}
	return len(objects), nil
}

func projectTables(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT c.table_name FROM information_schema.columns c
		 JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		 WHERE c.table_schema = 'public' AND c.column_name = 'project_id' AND t.table_type = 'BASE TABLE'
		 ORDER BY c.table_name`)
	if err != nil {
		return nil, fmt.Errorf("list project tables: %w", err)
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	var out []string
	for _, t := range all {
		if !keptTables[t] {
			out = append(out, t)
		}
	}
	return out, nil
}

// deleteInRounds deletes the project's rows from every table, retrying a
// table whose delete a foreign key from a not-yet-emptied table refused,
// until a round makes no progress.
func deleteInRounds(ctx context.Context, tx pgx.Tx, tables []string, projectID uuid.UUID) error {
	left := tables
	for len(left) > 0 {
		var next []string
		var lastErr error
		for _, t := range left {
			if _, err := tx.Exec(ctx, `SAVEPOINT purge_table`); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE project_id = $1`, pgx.Identifier{t}.Sanitize()), projectID)
			var pgErr *pgconn.PgError
			switch {
			case err == nil:
				if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT purge_table`); err != nil {
					return err
				}
			case errors.As(err, &pgErr) && pgErr.Code == "23503": // foreign_key_violation
				if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT purge_table`); err != nil {
					return err
				}
				next, lastErr = append(next, t), err
			default:
				return fmt.Errorf("delete from %s: %w", t, err)
			}
		}
		if len(next) == len(left) {
			return fmt.Errorf("delete stuck on %v: %w", next, lastErr)
		}
		left = next
	}
	return nil
}

// SetLegalHold places or lifts a hold. Placing one cancels a scheduled
// deletion: a hold means nothing of the project may go.
func SetLegalHold(ctx context.Context, pool *db.Pool, projectID uuid.UUID, hold bool, reason string) error {
	return db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var tag pgconn.CommandTag
		var err error
		if hold {
			tag, err = tx.Exec(ctx,
				`UPDATE projects SET legal_hold = true, legal_hold_reason = $2,
				        deletion_requested_at = NULL, deletion_requested_by = NULL, purge_after = NULL
				 WHERE id = $1`, projectID, reason)
		} else {
			tag, err = tx.Exec(ctx, `UPDATE projects SET legal_hold = false, legal_hold_reason = NULL WHERE id = $1`, projectID)
		}
		if err != nil {
			return fmt.Errorf("retention: legal hold: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return errors.New("retention: no such project")
		}
		return nil
	})
}
