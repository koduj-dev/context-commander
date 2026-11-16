package plans

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// Overview window bounds, in days.
const (
	DefaultOverviewDays = 14
	MaxOverviewDays     = 90
)

// ProjectOverview is one project's state of work in numbers: how its items
// stand, how many were finished on each of the last days, and when anything
// last moved. Statuses and timestamps are plaintext, so it is read without
// decrypting a single title.
type ProjectOverview struct {
	ProjectID uuid.UUID
	Path      string // "namespace/slug"
	Name      string
	Role      string
	// Items of every plan that is not abandoned, as the board counts them.
	Counts Counts
	// Items finished per UTC day, oldest first; the last entry is today.
	DonePerDay []int
	// The latest change to a plan or an item, or a heartbeat; nil when the
	// project has none of these.
	LastActivity *time.Time
}

// Overview is ProjectOverview for every project whose role may read plans, or
// for the one named by onlyProject (uuid.Nil for all). One query per kind of
// number however many projects there are: the home screen asks on every load.
// The window starts on first (a UTC date) and ends today.
func (s *Service) Overview(ctx context.Context, principalID, onlyProject uuid.UUID, days int) (first time.Time, list []ProjectOverview, err error) {
	if days < 1 || days > MaxOverviewDays {
		return time.Time{}, nil, fmt.Errorf("%w: days must be between 1 and %d", ErrInvalid, MaxOverviewDays)
	}
	if onlyProject != uuid.Nil {
		if err := s.checkCapability(ctx, principalID, onlyProject, auth.CapPlanRead); err != nil {
			return time.Time{}, nil, err
		}
	}
	today := s.now().UTC().Truncate(24 * time.Hour)
	first = today.AddDate(0, 0, -(days - 1))

	out := []ProjectOverview{}
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		memberships, err := projects.NewStore().ListForPrincipal(ctx, tx, principalID)
		if err != nil {
			return err
		}
		index := map[uuid.UUID]int{}
		var ids []uuid.UUID
		for _, ms := range memberships {
			if !auth.RoleCan(ms.Role, auth.CapPlanRead) || (onlyProject != uuid.Nil && ms.Project.ID != onlyProject) {
				continue
			}
			index[ms.Project.ID] = len(out)
			ids = append(ids, ms.Project.ID)
			out = append(out, ProjectOverview{
				ProjectID: ms.Project.ID, Path: ms.NamespaceSlug + "/" + ms.Project.Slug, Name: ms.Project.Name,
				Role: string(ms.Role), DonePerDay: make([]int, days),
			})
		}
		if len(ids) == 0 {
			return nil
		}
		if err := overviewCounts(ctx, tx, ids, index, out); err != nil {
			return err
		}
		if err := overviewDone(ctx, tx, ids, first, index, out); err != nil {
			return err
		}
		return overviewLast(ctx, tx, ids, index, out)
	})
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("plans: overview: %w", err)
	}
	sort.SliceStable(out, func(i, j int) bool { return later(out[i].LastActivity, out[j].LastActivity) })
	return first, out, nil
}

func overviewCounts(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, index map[uuid.UUID]int, out []ProjectOverview) error {
	rows, err := tx.Query(ctx,
		`SELECT i.project_id, i.status, count(*)
		 FROM plan_items i JOIN plans p ON p.id = i.plan_id
		 WHERE i.project_id = ANY($1) AND p.status <> 'abandoned'
		 GROUP BY i.project_id, i.status`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var status string
		var n int
		if err := rows.Scan(&id, &status, &n); err != nil {
			return err
		}
		c := &out[index[id]].Counts
		for range n {
			c.add(status)
		}
	}
	return rows.Err()
}

func overviewDone(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, first time.Time, index map[uuid.UUID]int, out []ProjectOverview) error {
	rows, err := tx.Query(ctx,
		`SELECT i.project_id, (i.completed_at AT TIME ZONE 'UTC')::date, count(*)
		 FROM plan_items i JOIN plans p ON p.id = i.plan_id
		 WHERE i.project_id = ANY($1) AND i.status = 'done' AND i.completed_at >= $2 AND p.status <> 'abandoned'
		 GROUP BY 1, 2`, ids, first)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var day time.Time
		var n int
		if err := rows.Scan(&id, &day, &n); err != nil {
			return err
		}
		perDay := out[index[id]].DonePerDay
		// A clock behind the database's lands an item "tomorrow"; it counts
		// as today rather than vanishing.
		k := min(int(day.Sub(first).Hours()/24), len(perDay)-1)
		if k >= 0 {
			perDay[k] += n
		}
	}
	return rows.Err()
}

func overviewLast(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, index map[uuid.UUID]int, out []ProjectOverview) error {
	rows, err := tx.Query(ctx,
		`SELECT project_id, max(at) FROM (
		   SELECT project_id, updated_at AS at FROM plans WHERE project_id = ANY($1)
		   UNION ALL SELECT project_id, updated_at FROM plan_items WHERE project_id = ANY($1)
		   UNION ALL SELECT project_id, last_beat FROM work_presence WHERE project_id = ANY($1)
		 ) t GROUP BY project_id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return err
		}
		out[index[id]].LastActivity = &at
	}
	return rows.Err()
}

// later orders the most recent first and "never" last.
func later(a, b *time.Time) bool {
	switch {
	case a == nil:
		return false
	case b == nil:
		return true
	default:
		return a.After(*b)
	}
}
