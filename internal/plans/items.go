package plans

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/quota"
)

// AddItems appends items to a plan. Merged on the server under the plan's
// row lock, so two agents appending at once both land, in arrival order.
func (s *Service) AddItems(ctx context.Context, principalID, projectID, planID uuid.UUID, items []NewItem) (Plan, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Plan{}, err
	}
	if len(items) == 0 {
		return Plan{}, invalid("no items to add")
	}
	if err := validateNewItems(items); err != nil {
		return Plan{}, err
	}

	var p Plan
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var blob planBlob
		var err error
		p, blob, err = s.loadPlan(ctx, tx, projectID, planID, true)
		if err != nil {
			return err
		}
		if len(p.Items)+len(items) > MaxItemsPerPlan {
			return invalid("a plan holds at most %d items", MaxItemsPerPlan)
		}
		if blob.Items == nil {
			blob.Items = map[uuid.UUID]itemBlob{}
		}
		next := 1
		if n := len(p.Items); n > 0 {
			next = p.Items[n-1].Position + 1
		}
		for i, it := range items {
			kind := it.Kind
			if kind == "" {
				kind = KindTask
			}
			item := Item{ID: uuid.New(), PlanID: p.ID, Position: next + i, Kind: kind, Status: ItemOpen, Text: strings.TrimSpace(it.Text)}
			if err := insertItem(ctx, tx, projectID, &item); err != nil {
				return err
			}
			blob.Items[item.ID] = itemBlob{Text: item.Text}
			p.Items = append(p.Items, item)
		}
		return s.rewritePlan(ctx, tx, &p, blob)
	})
	if err != nil {
		return Plan{}, fmt.Errorf("plans: add items: %w", quota.Translate(err))
	}
	return p, nil
}

// UpdateItem changes one item. A status change is recorded (the last
// HistoryPerItem per item are kept); setting the status an item already has
// records nothing. Moving an unassigned item to in_progress assigns it to
// whoever moved it, because "someone is on this" without a someone is the
// one state the board cannot use.
func (s *Service) UpdateItem(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, itemID uuid.UUID, u ItemUpdate) (Item, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Item{}, err
	}
	if u.Status != nil && !validItemStatus(*u.Status) {
		return Item{}, invalid("item status %q is not one of open, in_progress, done, blocked, skipped", *u.Status)
	}
	if u.Text != nil {
		if strings.TrimSpace(*u.Text) == "" {
			return Item{}, invalid("an item needs text")
		}
		if err := checkLen("item text", *u.Text, MaxItemText); err != nil {
			return Item{}, err
		}
	}
	if u.Note != nil {
		if err := checkLen("note", *u.Note, MaxItemNote); err != nil {
			return Item{}, err
		}
	}

	var out Item
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var planID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT plan_id FROM plan_items WHERE id = $1 AND project_id = $2`, itemID, projectID).Scan(&planID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		// The plan row is locked only when its text changes: a status tick
		// must not queue behind someone else's edit of another item.
		textChange := u.Text != nil || u.Note != nil
		p, blob, err := s.loadPlan(ctx, tx, projectID, planID, textChange)
		if err != nil {
			return err
		}
		var it *Item
		for i := range p.Items {
			if p.Items[i].ID == itemID {
				it = &p.Items[i]
			}
		}
		if it == nil {
			return ErrNotFound
		}

		if u.AssigneeSet && u.Assignee != nil {
			var member bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM project_members WHERE project_id = $1 AND principal_id = $2)`,
				projectID, *u.Assignee).Scan(&member); err != nil {
				return err
			}
			if !member {
				return invalid("the assignee is not a member of this project")
			}
		}

		if textChange {
			t := blob.Items[itemID]
			if u.Text != nil {
				t.Text = strings.TrimSpace(*u.Text)
			}
			if u.Note != nil {
				t.Note = *u.Note
			}
			blob.Items[itemID] = t
			it.Text, it.Note = t.Text, t.Note
			if err := s.rewritePlan(ctx, tx, &p, blob); err != nil {
				return err
			}
		}

		from := it.Status
		if u.Status != nil {
			it.Status = *u.Status
		}
		if u.AssigneeSet {
			it.Assignee = u.Assignee
		} else if it.Status == ItemInProgress && it.Assignee == nil {
			me := principalID
			it.Assignee = &me
		}
		if err := tx.QueryRow(ctx,
			`UPDATE plan_items SET status = $2, assignee_principal = $3, updated_by_principal = $4, updated_at = now(),
			        completed_at = CASE WHEN $2 = 'done' THEN COALESCE(completed_at, now()) ELSE NULL END
			 WHERE id = $1 RETURNING updated_at, completed_at, updated_by_principal`,
			it.ID, it.Status, it.Assignee, principalID,
		).Scan(&it.UpdatedAt, &it.CompletedAt, &it.UpdatedBy); err != nil {
			return err
		}

		if it.Status != from {
			if _, err := tx.Exec(ctx,
				`INSERT INTO plan_item_transitions (project_id, item_id, from_status, to_status, principal_id, client_id)
				 VALUES ($1, $2, $3, $4, $5, $6)`,
				projectID, itemID, from, it.Status, principalID, clientID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`DELETE FROM plan_item_transitions WHERE item_id = $1 AND id NOT IN (
				   SELECT id FROM plan_item_transitions WHERE item_id = $1 ORDER BY id DESC LIMIT $2)`,
				itemID, HistoryPerItem); err != nil {
				return err
			}
		}
		out = *it
		return nil
	})
	if err != nil {
		return Item{}, fmt.Errorf("plans: update item: %w", quota.Translate(err))
	}
	return out, nil
}

// Transitions is an item's recorded status changes, newest first.
func (s *Service) Transitions(ctx context.Context, principalID, projectID, itemID uuid.UUID) ([]Transition, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return nil, err
	}
	var out []Transition
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT from_status, to_status, principal_id, client_id, at FROM plan_item_transitions
			 WHERE item_id = $1 AND project_id = $2 ORDER BY id DESC`, itemID, projectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t Transition
			if err := rows.Scan(&t.From, &t.To, &t.PrincipalID, &t.ClientID, &t.At); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("plans: transitions: %w", err)
	}
	return out, nil
}
