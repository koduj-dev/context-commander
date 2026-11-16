// Package changes answers an agent's "what did the others do while I was
// working": memories recorded, steps blocked, finished or unblocked, and
// handoffs opened on its project since a moment it names (docs/adr/0033).
//
// MCP gives a server no way to speak first, so this is read, not pushed: the
// Claude Code inbox hook asks for it between tool calls and prints what came
// back into the model's context, and any client can call project.changes.
// It owns no storage. It composes the services that own each kind of
// change, so every read goes through their authorization and decryption.
package changes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/memories"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/plans"
)

// Bounds on one read.
const (
	// MaxItems is the most changes one read returns, newest first. An agent
	// needs to know that things moved and where to look, not a full log.
	MaxItems = 20
	// MaxWindow is how far back since may reach. An agent that was away
	// longer gets the last day; context.bootstrap is the way to catch up on
	// more.
	MaxWindow = 24 * time.Hour
)

// Kinds of change.
const (
	KindMemory  = "memory"
	KindItem    = "item"
	KindHandoff = "handoff"
)

// Change is one thing someone else did.
type Change struct {
	Kind string
	ID   uuid.UUID
	// Title is the memory's title, the step's text, or empty for a handoff,
	// whose summary is read with handoff.get.
	Title string
	// Detail is the memory's kind (decision, gotcha…), the step's move
	// ("blocked", "done", "unblocked") or the handoff's status.
	Detail string
	// Where is the step's plan title; empty otherwise.
	Where  string
	PlanID uuid.UUID // a step's plan; uuid.Nil otherwise
	ByName string
	By     uuid.UUID
	At     time.Time
}

// Service composes the domain services a change can come from.
type Service struct {
	pool     *db.Pool
	authz    *auth.AuthzService
	memories *memories.Service
	plans    *plans.Service
}

func NewService(pool *db.Pool, authz *auth.AuthzService, mem *memories.Service, pl *plans.Service) *Service {
	return &Service{pool: pool, authz: authz, memories: mem, plans: pl}
}

// Since returns what anyone but principalID did on projectID after since,
// newest first, at most MaxItems. since is clamped to MaxWindow before now.
// A kind the caller's role cannot read is left out rather than refused, so
// a READER still hears about memories.
func (s *Service) Since(ctx context.Context, principalID, projectID uuid.UUID, since, now time.Time) ([]Change, error) {
	if floor := now.Add(-MaxWindow); since.Before(floor) {
		since = floor
	}
	var out []Change

	page, err := s.memories.List(ctx, principalID, projectID, memories.Filter{CreatedAfter: &since}, paging.Options{Limit: MaxItems})
	switch {
	case errors.Is(err, memories.ErrForbidden):
	case err != nil:
		return nil, fmt.Errorf("changes: memories: %w", err)
	default:
		var others []memories.Memory
		for _, m := range page.Items {
			if m.CreatedBy != principalID {
				others = append(others, m)
			}
		}
		people, err := s.memories.People(ctx, principalID, others)
		if err != nil {
			return nil, fmt.Errorf("changes: memory authors: %w", err)
		}
		for _, m := range others {
			out = append(out, Change{Kind: KindMemory, ID: m.ID, Title: m.Title, Detail: m.Kind,
				By: m.CreatedBy, ByName: people[m.CreatedBy].Name, At: m.CreatedAt})
		}
	}

	moved, err := s.plans.ItemChanges(ctx, principalID, projectID, since, MaxItems)
	switch {
	case errors.Is(err, plans.ErrForbidden):
	case err != nil:
		return nil, fmt.Errorf("changes: plans: %w", err)
	default:
		for _, c := range moved {
			detail := c.To
			if c.From == plans.ItemBlocked && c.To != plans.ItemDone {
				detail = "unblocked" // back to open or in progress
			}
			out = append(out, Change{Kind: KindItem, ID: c.ItemID, Title: c.Text, Detail: detail, Where: c.PlanTitle,
				PlanID: c.PlanID, By: c.By.PrincipalID, ByName: c.By.Name, At: c.At})
		}
	}

	handoffs, err := s.handoffs(ctx, principalID, projectID, since)
	if err != nil {
		return nil, err
	}
	out = append(out, handoffs...)

	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > MaxItems {
		out = out[:MaxItems]
	}
	return out, nil
}

// handoffs are read here rather than through internal/handoffs: the list it
// offers pages by cursor and has no "since", and all a change needs is the
// plaintext id, status and author.
func (s *Service) handoffs(ctx context.Context, principalID, projectID uuid.UUID, since time.Time) ([]Change, error) {
	allowed, err := s.authz.Can(ctx, principalID, projectID, auth.CapHandoffRead)
	if err != nil {
		return nil, fmt.Errorf("changes: authorization check: %w", err)
	}
	if !allowed {
		return nil, nil
	}
	var out []Change
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT h.id, h.status, h.created_by_principal, h.created_at,
			        COALESCE(NULLIF(u.display_name, ''), u.email, sp.name, '')
			 FROM handoffs h
			 JOIN principals p ON p.id = h.created_by_principal
			 LEFT JOIN users u ON u.id = p.user_id
			 LEFT JOIN service_principals sp ON sp.id = p.service_principal_id
			 WHERE h.project_id = $1 AND h.created_at > $2 AND h.created_by_principal <> $3
			 ORDER BY h.created_at DESC LIMIT $4`,
			projectID, since, principalID, MaxItems)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Change, error) {
			c := Change{Kind: KindHandoff}
			err := r.Scan(&c.ID, &c.Detail, &c.By, &c.At, &c.ByName)
			return c, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("changes: handoffs: %w", err)
	}
	return out, nil
}
