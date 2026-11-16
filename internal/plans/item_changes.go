package plans

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
)

// ItemChange is one step someone else moved: what it says, where it lives,
// from which status to which, who and when.
type ItemChange struct {
	PlanID    uuid.UUID
	PlanTitle string
	ItemID    uuid.UUID
	Text      string
	From, To  string
	By        Person
	At        time.Time
}

// ItemChanges returns the status changes on projectID after since, made by
// anyone but principalID, newest first and at most limit. Only moves worth
// interrupting another agent for are returned: to blocked, to done, and out
// of blocked.
//
// It asks the plaintext transition log first and decrypts the project's
// plans only when something matched, so a poll that finds nothing costs one
// indexed query.
func (s *Service) ItemChanges(ctx context.Context, principalID, projectID uuid.UUID, since time.Time, limit int) ([]ItemChange, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return nil, err
	}
	var out []ItemChange
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT item_id, from_status, to_status, principal_id, at FROM plan_item_transitions
			 WHERE project_id = $1 AND at > $2 AND principal_id <> $3
			   AND (to_status IN ($4, $5) OR from_status = $4)
			 ORDER BY at DESC, id DESC LIMIT $6`,
			projectID, since, principalID, ItemBlocked, ItemDone, limit)
		if err != nil {
			return err
		}
		type moved struct {
			item     uuid.UUID
			from, to string
			by       uuid.UUID
			at       time.Time
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (moved, error) {
			var m moved
			err := r.Scan(&m.item, &m.from, &m.to, &m.by, &m.at)
			return m, err
		})
		if err != nil || len(list) == 0 {
			return err
		}

		proj, err := s.loadProject(ctx, tx, projectID)
		if err != nil {
			return err
		}
		type where struct {
			plan Plan
			text string
		}
		items := map[uuid.UUID]where{}
		for _, p := range proj.plans {
			for _, it := range p.Items {
				items[it.ID] = where{plan: p, text: it.Text}
			}
		}
		ids := map[uuid.UUID]bool{}
		for _, m := range list {
			ids[m.by] = true
		}
		people, err := resolvePeople(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, m := range list {
			w, ok := items[m.item]
			if !ok {
				continue // the item was deleted since
			}
			out = append(out, ItemChange{
				PlanID: w.plan.ID, PlanTitle: w.plan.Title, ItemID: m.item, Text: w.text,
				From: m.from, To: m.to, By: people[m.by], At: m.at,
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("plans: item changes: %w", err)
	}
	return out, nil
}

// Snapshot returns every phase and every plan of projectID with its items,
// abandoned plans included, decrypted: what a project export carries.
func (s *Service) Snapshot(ctx context.Context, principalID, projectID uuid.UUID) ([]Phase, []Plan, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return nil, nil, err
	}
	var proj project
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		proj, err = s.loadProject(ctx, tx, projectID)
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("plans: snapshot: %w", err)
	}
	return proj.phases, proj.plans, nil
}
