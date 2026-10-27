package plans

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
)

type presenceBlob struct {
	Activity string `json:"activity"`
}

// presenceRow is a work_presence row before its activity is opened.
type presenceRow struct {
	Presence
	e *envelope
}

// Report records where a client is (docs/adr/0022). It is a heartbeat:
// call it on arriving at an item, and as often as wanted while working.
//
// Time is credited to the item the client was on before this report, for
// the gap since its previous report — and only when that gap is within
// IdleAfter, so a client that went quiet for an hour adds nothing for it.
// Token deltas go to the same item: they were spent getting here. A client
// that was on nothing gives them to where it now is.
func (s *Service) Report(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID uuid.UUID, r Report) (Presence, error) {
	if clientID == nil {
		return Presence{}, ErrNoClient
	}
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Presence{}, err
	}
	r.Activity = strings.TrimSpace(r.Activity)
	if err := checkLen("activity", r.Activity, MaxActivity); err != nil {
		return Presence{}, err
	}
	if !r.Usage.validTokens() {
		return Presence{}, invalid("token counts are deltas since the previous report, between 0 and %d", MaxTokensPerReport)
	}

	now := s.now()
	out := Presence{ClientID: *clientID, PrincipalID: principalID, ProjectID: projectID, Activity: r.Activity, LastBeat: now}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		planID := r.PlanID
		if r.ItemID != nil {
			var itemPlan uuid.UUID
			err := tx.QueryRow(ctx, `SELECT plan_id FROM plan_items WHERE id = $1 AND project_id = $2`, *r.ItemID, projectID).Scan(&itemPlan)
			if errors.Is(err, pgx.ErrNoRows) {
				return invalid("item %s is not in this project", *r.ItemID)
			}
			if err != nil {
				return err
			}
			if planID != nil && *planID != itemPlan {
				return invalid("item %s is not in plan %s", *r.ItemID, *planID)
			}
			planID = &itemPlan
		} else if planID != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM plans WHERE id = $1 AND project_id = $2)`, *planID, projectID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return invalid("plan %s is not in this project", *planID)
			}
		}
		out.PlanID, out.ItemID = planID, r.ItemID

		since, err := s.creditPrevious(ctx, tx, principalID, *clientID, now, r.ItemID, tokensOnly(r.Usage))
		if err != nil {
			return err
		}
		out.Since = since

		var e envelope
		if r.Activity != "" {
			if e, err = s.seal(ctx, tx, projectID, "work_presence", *clientID, 1, presenceBlob{Activity: r.Activity}); err != nil {
				return err
			}
		}
		var ct, nonce, dek []byte
		var kv *int
		if r.Activity != "" {
			ct, nonce, dek, kv = e.ciphertext, e.nonce, e.wrappedDEK, &e.keyVersion
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO work_presence (client_id, principal_id, project_id, plan_id, item_id, ciphertext, nonce, wrapped_dek, project_key_version, since, last_beat)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			 ON CONFLICT (client_id) DO UPDATE SET principal_id = EXCLUDED.principal_id, project_id = EXCLUDED.project_id,
			   plan_id = EXCLUDED.plan_id, item_id = EXCLUDED.item_id, ciphertext = EXCLUDED.ciphertext, nonce = EXCLUDED.nonce,
			   wrapped_dek = EXCLUDED.wrapped_dek, project_key_version = EXCLUDED.project_key_version,
			   since = EXCLUDED.since, last_beat = EXCLUDED.last_beat`,
			*clientID, principalID, projectID, planID, r.ItemID, ct, nonce, dek, kv, since, now); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `SELECT cc_prune_presence()`)
		return err
	})
	if err != nil {
		return Presence{}, fmt.Errorf("plans: report: %w", err)
	}
	return out, nil
}

// creditPrevious adds the time since the client's previous report, and the
// reported tokens, to the item that report was on. Returns when the client
// arrived where it now is: unchanged if it stayed on the same item.
func (s *Service) creditPrevious(ctx context.Context, tx pgx.Tx, principalID, clientID uuid.UUID, now time.Time, nextItem *uuid.UUID, tokens Effort) (time.Time, error) {
	var prevItem *uuid.UUID
	var prevProject uuid.UUID
	var since, lastBeat time.Time
	err := tx.QueryRow(ctx,
		`SELECT item_id, project_id, since, last_beat FROM work_presence WHERE client_id = $1 FOR UPDATE`, clientID,
	).Scan(&prevItem, &prevProject, &since, &lastBeat)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, err
	}

	target := nextItem
	credit := tokens
	if found && prevItem != nil {
		target = prevItem
		if gap := now.Sub(lastBeat); gap > 0 && gap <= IdleAfter {
			credit.Seconds = int64(gap / time.Second)
		}
	}
	if target != nil && (credit.Seconds > 0 || credit.anyTokens()) {
		if _, err := tx.Exec(ctx,
			`INSERT INTO plan_item_effort (item_id, principal_id, project_id, seconds, tokens_in, tokens_out, cache_read_tokens, cache_write_tokens)
			 SELECT id, $2, project_id, $3, $4, $5, $6, $7 FROM plan_items WHERE id = $1
			 ON CONFLICT (item_id, principal_id) DO UPDATE SET
			   seconds = plan_item_effort.seconds + EXCLUDED.seconds,
			   tokens_in = plan_item_effort.tokens_in + EXCLUDED.tokens_in,
			   tokens_out = plan_item_effort.tokens_out + EXCLUDED.tokens_out,
			   cache_read_tokens = plan_item_effort.cache_read_tokens + EXCLUDED.cache_read_tokens,
			   cache_write_tokens = plan_item_effort.cache_write_tokens + EXCLUDED.cache_write_tokens`,
			*target, principalID, credit.Seconds, credit.TokensIn, credit.TokensOut, credit.CacheReadTokens, credit.CacheWriteTokens); err != nil {
			return time.Time{}, err
		}
	}

	if found && sameItem(prevItem, nextItem) {
		return since, nil
	}
	return now, nil
}

// tokensOnly drops a caller-supplied Seconds: time is measured, never told.
func tokensOnly(e Effort) Effort {
	e.Seconds = 0
	return e
}

// ReportUsage credits token deltas to wherever the client currently is,
// without moving it, and counts as a heartbeat there. It is what a client
// hook calls after every turn (deploy/mcp-clients/README.md): the hook knows
// the tokens and not the step, and the presence row already knows the step.
//
// Returns the presence it credited, or ok=false when the client has none —
// it never reported where it is, so there is nothing to attach the tokens
// to, and they are dropped rather than guessed onto some item.
func (s *Service) ReportUsage(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, usage Effort) (Presence, bool, error) {
	if clientID == nil {
		return Presence{}, false, ErrNoClient
	}
	if !usage.validTokens() {
		return Presence{}, false, invalid("token counts are deltas since the previous report, between 0 and %d", MaxTokensPerReport)
	}
	var projectID uuid.UUID
	var planID, itemID *uuid.UUID
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT project_id, plan_id, item_id FROM work_presence WHERE client_id = $1`, *clientID).
			Scan(&projectID, &planID, &itemID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Presence{}, false, nil
	}
	if err != nil {
		return Presence{}, false, fmt.Errorf("plans: report usage: %w", err)
	}
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Presence{}, false, err
	}

	now := s.now()
	out := Presence{ClientID: *clientID, PrincipalID: principalID, ProjectID: projectID, PlanID: planID, ItemID: itemID, LastBeat: now}
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		since, err := s.creditPrevious(ctx, tx, principalID, *clientID, now, itemID, tokensOnly(usage))
		if err != nil {
			return err
		}
		out.Since = since
		// The activity line is left as the client last wrote it: a hook does
		// not know what the agent is doing, only what it cost.
		_, err = tx.Exec(ctx, `UPDATE work_presence SET last_beat = $2 WHERE client_id = $1`, *clientID, now)
		return err
	})
	if err != nil {
		return Presence{}, false, fmt.Errorf("plans: report usage: %w", err)
	}
	return out, true, nil
}

func sameItem(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// ClearPresence ends a client's presence: it stopped, finished, or signed
// off. The time since its last report is credited first, as for a move.
func (s *Service) ClearPresence(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID uuid.UUID, tokens Effort) error {
	if clientID == nil {
		return ErrNoClient
	}
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return err
	}
	if !tokens.validTokens() {
		return invalid("token counts are deltas since the previous report, between 0 and %d", MaxTokensPerReport)
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if _, err := s.creditPrevious(ctx, tx, principalID, *clientID, s.now(), nil, tokensOnly(tokens)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM work_presence WHERE client_id = $1`, *clientID)
		return err
	})
	if err != nil {
		return fmt.Errorf("plans: clear presence: %w", err)
	}
	return nil
}

// loadPresence reads presence rows visible to the caller. projectID nil reads
// every project the caller is a member of (RLS is what scopes it). A revoked
// credential's row is not shown: it can never report again.
func loadPresence(ctx context.Context, tx pgx.Tx, projectID *uuid.UUID) ([]presenceRow, error) {
	rows, err := tx.Query(ctx,
		`SELECT w.client_id, c.client_type, w.principal_id, w.project_id, n.slug || '/' || p.slug,
		        w.plan_id, w.item_id, w.ciphertext, w.nonce, w.wrapped_dek, w.project_key_version, w.since, w.last_beat
		 FROM work_presence w
		 JOIN client_credentials c ON c.id = w.client_id AND c.revoked_at IS NULL
		 JOIN projects p ON p.id = w.project_id
		 JOIN namespaces n ON n.id = p.namespace_id
		 WHERE $1::uuid IS NULL OR w.project_id = $1
		 ORDER BY w.last_beat DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []presenceRow
	for rows.Next() {
		var r presenceRow
		var ct, nonce, dek []byte
		var kv *int
		if err := rows.Scan(&r.ClientID, &r.ClientType, &r.PrincipalID, &r.ProjectID, &r.ProjectPath,
			&r.PlanID, &r.ItemID, &ct, &nonce, &dek, &kv, &r.Since, &r.LastBeat); err != nil {
			return nil, err
		}
		if ct != nil && kv != nil {
			r.e = &envelope{ciphertext: ct, nonce: nonce, wrappedDEK: dek, keyVersion: *kv}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) openPresence(ctx context.Context, tx pgx.Tx, r presenceRow) (Presence, error) {
	p := r.Presence
	if r.e != nil {
		var b presenceBlob
		if err := s.open(ctx, tx, p.ProjectID, "work_presence", p.ClientID, 1, *r.e, &b); err != nil {
			return Presence{}, err
		}
		p.Activity = b.Activity
	}
	return p, nil
}

// MyPresence is where everyone is, across every project the caller is a
// member of: the screen for watching an agent from anywhere.
func (s *Service) MyPresence(ctx context.Context, principalID uuid.UUID) ([]Presence, map[uuid.UUID]Person, error) {
	var out []Presence
	var people map[uuid.UUID]Person
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := loadPresence(ctx, tx, nil)
		if err != nil {
			return err
		}
		blobs := map[uuid.UUID]planBlob{}
		ids := map[uuid.UUID]bool{}
		for _, r := range rows {
			p, err := s.openPresence(ctx, tx, r)
			if err != nil {
				return err
			}
			if p.PlanID != nil {
				blob, ok := blobs[*p.PlanID]
				if !ok {
					if _, blob, err = s.loadPlan(ctx, tx, p.ProjectID, *p.PlanID, false); err != nil && !errors.Is(err, ErrNotFound) {
						return err
					}
					blobs[*p.PlanID] = blob
				}
				p.PlanTitle = blob.Title
				if p.ItemID != nil {
					p.ItemText = blob.Items[*p.ItemID].Text
				}
			}
			ids[p.PrincipalID] = true
			out = append(out, p)
		}
		people, err = resolvePeople(ctx, tx, ids)
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("plans: my presence: %w", err)
	}
	return out, people, nil
}

// resolvePeople names principals: a user's display name or email, an agent's
// name.
func resolvePeople(ctx context.Context, tx pgx.Tx, ids map[uuid.UUID]bool) (map[uuid.UUID]Person, error) {
	out := map[uuid.UUID]Person{}
	if len(ids) == 0 {
		return out, nil
	}
	list := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	rows, err := tx.Query(ctx,
		`SELECT p.id, p.kind, COALESCE(NULLIF(u.display_name, ''), u.email, sp.name, '')
		 FROM principals p
		 LEFT JOIN users u ON u.id = p.user_id
		 LEFT JOIN service_principals sp ON sp.id = p.service_principal_id
		 WHERE p.id = ANY($1)`, list)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.PrincipalID, &p.Kind, &p.Name); err != nil {
			return nil, err
		}
		out[p.PrincipalID] = p
	}
	return out, rows.Err()
}
