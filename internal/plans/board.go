package plans

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
)

// project is everything a project's board is made of, decrypted. Read in one
// transaction so the counts, the titles and the presence agree.
type project struct {
	phases []Phase
	plans  []Plan // with items, text filled in
}

func (s *Service) loadProject(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) (project, error) {
	var out project

	type phaseRow struct {
		Phase
		e envelope
	}
	rows, err := tx.Query(ctx,
		`SELECT id, project_id, position, status, version, ciphertext, nonce, wrapped_dek, project_key_version, created_by_principal, created_at, updated_at
		 FROM phases WHERE project_id = $1 ORDER BY position`, projectID)
	if err != nil {
		return project{}, err
	}
	var phaseRows []phaseRow
	for rows.Next() {
		var r phaseRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Position, &r.Status, &r.Version, &r.e.ciphertext, &r.e.nonce, &r.e.wrappedDEK, &r.e.keyVersion,
			&r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
			rows.Close()
			return project{}, err
		}
		phaseRows = append(phaseRows, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return project{}, err
	}
	// Decrypted only after the rows are closed: pgx refuses a second query
	// on the transaction while one is open, and Decrypt needs one.
	for _, r := range phaseRows {
		var b phaseBlob
		if err := s.open(ctx, tx, projectID, "phase", r.ID, r.Version, r.e, &b); err != nil {
			return project{}, err
		}
		ph := r.Phase
		ph.Title, ph.Goal = b.Title, b.Goal
		out.phases = append(out.phases, ph)
	}

	rows, err = tx.Query(ctx, `SELECT `+planColumns+` FROM plans WHERE project_id = $1 ORDER BY updated_at DESC`, projectID)
	if err != nil {
		return project{}, err
	}
	var planRows []planRow
	for rows.Next() {
		var r planRow
		if err := scanPlan(rows, &r); err != nil {
			rows.Close()
			return project{}, err
		}
		planRows = append(planRows, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return project{}, err
	}

	items, err := loadItems(ctx, tx, `WHERE i.project_id = $1`, projectID)
	if err != nil {
		return project{}, err
	}
	byPlan := map[uuid.UUID][]Item{}
	for _, it := range items {
		byPlan[it.PlanID] = append(byPlan[it.PlanID], it)
	}

	for _, r := range planRows {
		var b planBlob
		if err := s.open(ctx, tx, projectID, "plan", r.ID, r.Version, r.e, &b); err != nil {
			return project{}, err
		}
		p := r.Plan
		p.Title, p.Body = b.Title, b.Body
		p.Items = byPlan[p.ID]
		for i := range p.Items {
			t := b.Items[p.Items[i].ID]
			p.Items[i].Text, p.Items[i].Note = t.Text, t.Note
		}
		out.plans = append(out.plans, p)
	}
	return out, nil
}

// Board is the state of a project's work: phases with their progress,
// plans, what is in progress or blocked and by whom, and where every client
// is. Abandoned plans are left out of the list and out of the phase counts.
func (s *Service) Board(ctx context.Context, principalID, projectID uuid.UUID) (Board, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return Board{}, err
	}
	var b Board
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		proj, err := s.loadProject(ctx, tx, projectID)
		if err != nil {
			return err
		}
		people := map[uuid.UUID]bool{}
		phaseIndex := map[uuid.UUID]int{}
		for i, ph := range proj.phases {
			b.Phases = append(b.Phases, PhaseSummary{Phase: ph})
			phaseIndex[ph.ID] = i
		}
		titles := map[uuid.UUID]Plan{}
		for _, p := range proj.plans {
			titles[p.ID] = p
			if p.Status == PlanAbandoned {
				continue
			}
			sum := PlanSummary{ID: p.ID, PhaseID: p.PhaseID, Status: p.Status, Title: p.Title, CreatedBy: p.CreatedBy, UpdatedAt: p.UpdatedAt}
			people[p.CreatedBy] = true
			for _, it := range p.Items {
				sum.Counts.add(it.Status)
				sum.Effort.add(it.Effort)
				if it.Assignee != nil {
					people[*it.Assignee] = true
				}
				switch it.Status {
				case ItemInProgress:
					b.InProgress = append(b.InProgress, ActiveItem{PlanID: p.ID, PlanTitle: p.Title, Item: it})
				case ItemBlocked:
					b.Blocked = append(b.Blocked, ActiveItem{PlanID: p.ID, PlanTitle: p.Title, Item: it})
				}
			}
			if p.PhaseID != nil {
				if i, ok := phaseIndex[*p.PhaseID]; ok {
					ph := &b.Phases[i]
					ph.Counts.Open += sum.Counts.Open
					ph.Counts.InProgress += sum.Counts.InProgress
					ph.Counts.Done += sum.Counts.Done
					ph.Counts.Blocked += sum.Counts.Blocked
					ph.Counts.Skipped += sum.Counts.Skipped
					ph.Effort.add(sum.Effort)
				}
			}
			b.Plans = append(b.Plans, sum)
		}

		rows, err := loadPresence(ctx, tx, &projectID)
		if err != nil {
			return err
		}
		for _, r := range rows {
			p, err := s.openPresence(ctx, tx, r)
			if err != nil {
				return err
			}
			if p.PlanID != nil {
				plan := titles[*p.PlanID]
				p.PlanTitle = plan.Title
				if p.ItemID != nil {
					for _, it := range plan.Items {
						if it.ID == *p.ItemID {
							p.ItemText = it.Text
						}
					}
				}
			}
			people[p.PrincipalID] = true
			b.Presence = append(b.Presence, p)
		}

		b.People, err = resolvePeople(ctx, tx, people)
		return err
	})
	if err != nil {
		return Board{}, fmt.Errorf("plans: board: %w", err)
	}
	return b, nil
}

// MaxSearchHits bounds one search's answer.
const MaxSearchHits = 100

// Search finds plans and items whose text contains query, case-insensitively.
// The text is encrypted (docs/adr/0011), so this decrypts the project's plans
// and matches in Go; a project's plans are hundreds, not millions.
// statusFilter, when set, keeps only items in that status.
func (s *Service) Search(ctx context.Context, principalID, projectID uuid.UUID, query, statusFilter string) ([]SearchHit, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" && statusFilter == "" {
		return nil, invalid("search needs a query or a status")
	}
	if statusFilter != "" && !validItemStatus(statusFilter) {
		return nil, invalid("item status %q is not one of open, in_progress, done, blocked, skipped", statusFilter)
	}
	match := func(texts ...string) bool {
		if q == "" {
			return true
		}
		for _, t := range texts {
			if strings.Contains(strings.ToLower(t), q) {
				return true
			}
		}
		return false
	}

	var hits []SearchHit
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		proj, err := s.loadProject(ctx, tx, projectID)
		if err != nil {
			return err
		}
		for _, p := range proj.plans {
			if statusFilter == "" && match(p.Title, p.Body) {
				hits = append(hits, SearchHit{PlanID: p.ID, PlanTitle: p.Title, PlanStatus: p.Status, Text: p.Title, Status: p.Status})
			}
			for _, it := range p.Items {
				if statusFilter != "" && it.Status != statusFilter {
					continue
				}
				if match(it.Text, it.Note) {
					id := it.ID
					hits = append(hits, SearchHit{PlanID: p.ID, PlanTitle: p.Title, PlanStatus: p.Status, ItemID: &id, Text: it.Text, Status: it.Status})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("plans: search: %w", err)
	}
	// Live work first: a hit in an active plan is more likely the one wanted
	// than the same words in a plan finished months ago.
	rank := map[string]int{PlanActive: 0, PlanDraft: 1, PlanDone: 2, PlanAbandoned: 3}
	sort.SliceStable(hits, func(i, j int) bool { return rank[hits[i].PlanStatus] < rank[hits[j].PlanStatus] })
	if len(hits) > MaxSearchHits {
		hits = hits[:MaxSearchHits]
	}
	return hits, nil
}
