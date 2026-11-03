package plans

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

// Reordering (docs/adr/0022, "Reordering"). A phase moves within its
// project and an item within its plan, to a 1-based place: 1 is first, and
// anything past the end is last.
//
// A move renumbers the whole list 1..n in one statement, under a lock that
// every other writer of that list's positions also takes: the project's
// phase-order advisory lock for phases, the plan's row lock for items. Two
// moves at once are therefore serialized, each one sees the order the other
// left, and no position is ever handed out twice.

// moveTo returns ids with id taken out and put back at the 1-based place
// to, clamped to the list. ok is false when id is not in the list.
func moveTo(ids []uuid.UUID, id uuid.UUID, to int) (out []uuid.UUID, place int, ok bool) {
	from := -1
	for i, v := range ids {
		if v == id {
			from = i
			break
		}
	}
	if from < 0 {
		return nil, 0, false
	}
	rest := make([]uuid.UUID, 0, len(ids))
	rest = append(rest, ids[:from]...)
	rest = append(rest, ids[from+1:]...)
	at := min(max(to, 1), len(ids)) - 1
	out = make([]uuid.UUID, 0, len(ids))
	out = append(out, rest[:at]...)
	out = append(out, id)
	out = append(out, rest[at:]...)
	return out, at + 1, true
}

// lockPhaseOrder serializes everything that assigns a phase position in
// one project: creating a phase and moving one.
func lockPhaseOrder(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	return db.AdvisoryXactLock(ctx, tx, db.LockClassPhaseOrder, projectID.String())
}

// renumber writes positions 1..n in the order of ids. table is a fixed name
// from this package, never caller input. Rows already in place are not
// touched, so moving an item one step rewrites two rows, not the plan.
func renumber(ctx context.Context, tx pgx.Tx, table string, ids []uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE `+table+` t SET position = o.n::int
		 FROM unnest($1::uuid[]) WITH ORDINALITY AS o(id, n)
		 WHERE t.id = o.id AND t.position <> o.n`, ids)
	return err
}

// movePhase puts a phase at a place in its project. The caller holds
// lockPhaseOrder.
func movePhase(ctx context.Context, tx pgx.Tx, projectID, phaseID uuid.UUID, to int) (int, error) {
	ids, err := orderedIDs(ctx, tx, `SELECT id FROM phases WHERE project_id = $1 ORDER BY position, id`, projectID)
	if err != nil {
		return 0, err
	}
	order, place, ok := moveTo(ids, phaseID, to)
	if !ok {
		return 0, ErrNotFound
	}
	return place, renumber(ctx, tx, "phases", order)
}

// moveItem puts an item at a place in its plan. The caller holds the plan's
// row lock.
func moveItem(ctx context.Context, tx pgx.Tx, planID, itemID uuid.UUID, to int) (int, error) {
	ids, err := orderedIDs(ctx, tx, `SELECT id FROM plan_items WHERE plan_id = $1 ORDER BY position, id`, planID)
	if err != nil {
		return 0, err
	}
	order, place, ok := moveTo(ids, itemID, to)
	if !ok {
		return 0, ErrNotFound
	}
	return place, renumber(ctx, tx, "plan_items", order)
}

func orderedIDs(ctx context.Context, tx pgx.Tx, query string, arg uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
