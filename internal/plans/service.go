package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/quota"
)

// Service is the authorization chokepoint for plans (docs/adr/0014): every
// method checks CapPlanRead or CapPlanWrite before touching data.
type Service struct {
	pool   *db.Pool
	crypto *crypto.Provider
	authz  *auth.AuthzService
	// now is replaceable so a test can move time without sleeping.
	now func() time.Time
}

func NewService(pool *db.Pool, cryptoProvider *crypto.Provider, authz *auth.AuthzService) *Service {
	return &Service{pool: pool, crypto: cryptoProvider, authz: authz, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the service's clock. For tests: effort is time between
// heartbeats, and a test that slept fifteen minutes would not be run.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

func (s *Service) checkCapability(ctx context.Context, principalID, projectID uuid.UUID, cap auth.Capability) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, cap)
	if err != nil {
		return fmt.Errorf("plans: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func checkLen(field, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return invalid("%s is longer than %d characters", field, max)
	}
	return nil
}

// ---------------------------------------------------------------- blobs

// planBlob is everything a plan says: its own text and every item's.
type planBlob struct {
	Title string                 `json:"title"`
	Body  string                 `json:"body,omitempty"`
	Items map[uuid.UUID]itemBlob `json:"items,omitempty"`
}

type itemBlob struct {
	Text string `json:"text"`
	Note string `json:"note,omitempty"`
}

type phaseBlob struct {
	Title string `json:"title"`
	Goal  string `json:"goal,omitempty"`
}

type envelope struct {
	ciphertext, nonce, wrappedDEK []byte
	keyVersion                    int
}

func (s *Service) seal(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, resource string, id uuid.UUID, version int, v any) (envelope, error) {
	plaintext, err := json.Marshal(v)
	if err != nil {
		return envelope{}, fmt.Errorf("plans: marshal %s: %w", resource, err)
	}
	ec := crypto.EncryptionContext{
		ProjectID: projectID, ResourceType: resource,
		ResourceID: id, ResourceVersion: uint64(version), //nolint:gosec // a small positive counter
	}
	var e envelope
	e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion, err = s.crypto.Encrypt(ctx, tx, ec, plaintext)
	if err != nil {
		return envelope{}, fmt.Errorf("plans: encrypt %s: %w", resource, err)
	}
	return e, nil
}

func (s *Service) open(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, resource string, id uuid.UUID, version int, e envelope, v any) error {
	ec := crypto.EncryptionContext{
		ProjectID: projectID, ResourceType: resource,
		ResourceID: id, ResourceVersion: uint64(version), //nolint:gosec // a small positive counter
	}
	plaintext, err := s.crypto.Decrypt(ctx, tx, ec, e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion)
	if err != nil {
		return fmt.Errorf("plans: decrypt %s: %w", resource, err)
	}
	return json.Unmarshal(plaintext, v)
}

// ---------------------------------------------------------------- phases

func (s *Service) CreatePhase(ctx context.Context, principalID, projectID uuid.UUID, title, goal string) (Phase, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Phase{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return Phase{}, invalid("a phase needs a title")
	}
	if err := errors.Join(checkLen("title", title, MaxTitle), checkLen("goal", goal, MaxPlanBody)); err != nil {
		return Phase{}, err
	}

	ph := Phase{ID: uuid.New(), ProjectID: projectID, Status: PhasePlanned, Version: 1, Title: title, Goal: goal, CreatedBy: principalID}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(position), 0) + 1 FROM phases WHERE project_id = $1`, projectID).Scan(&ph.Position); err != nil {
			return err
		}
		e, err := s.seal(ctx, tx, projectID, "phase", ph.ID, ph.Version, phaseBlob{Title: title, Goal: goal})
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO phases (id, project_id, position, status, version, ciphertext, nonce, wrapped_dek, project_key_version, created_by_principal)
			 VALUES ($1, $2, $3, $4, 1, $5, $6, $7, $8, $9) RETURNING created_at, updated_at`,
			ph.ID, projectID, ph.Position, ph.Status, e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion, principalID,
		).Scan(&ph.CreatedAt, &ph.UpdatedAt); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID,
			Action: "phase.create", ResourceType: "phase", ResourceID: &ph.ID, Result: audit.ResultAllow,
		})
	})
	if err != nil {
		return Phase{}, fmt.Errorf("plans: create phase: %w", quota.Translate(err))
	}
	return ph, nil
}

func (s *Service) UpdatePhase(ctx context.Context, principalID, projectID, phaseID uuid.UUID, u PhaseUpdate) (Phase, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Phase{}, err
	}
	if u.Status != nil && !validPhaseStatus(*u.Status) {
		return Phase{}, invalid("phase status %q is not one of planned, active, done", *u.Status)
	}
	if u.Title != nil && strings.TrimSpace(*u.Title) == "" {
		return Phase{}, invalid("a phase needs a title")
	}
	if u.Title != nil {
		if err := checkLen("title", *u.Title, MaxTitle); err != nil {
			return Phase{}, err
		}
	}
	if u.Goal != nil {
		if err := checkLen("goal", *u.Goal, MaxPlanBody); err != nil {
			return Phase{}, err
		}
	}

	var ph Phase
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var e envelope
		err := tx.QueryRow(ctx,
			`SELECT id, project_id, position, status, version, ciphertext, nonce, wrapped_dek, project_key_version, created_by_principal, created_at, updated_at
			 FROM phases WHERE id = $1 AND project_id = $2 FOR UPDATE`, phaseID, projectID,
		).Scan(&ph.ID, &ph.ProjectID, &ph.Position, &ph.Status, &ph.Version, &e.ciphertext, &e.nonce, &e.wrappedDEK, &e.keyVersion,
			&ph.CreatedBy, &ph.CreatedAt, &ph.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var blob phaseBlob
		if err := s.open(ctx, tx, projectID, "phase", ph.ID, ph.Version, e, &blob); err != nil {
			return err
		}
		if u.Status != nil {
			ph.Status = *u.Status
		}
		if u.Title != nil || u.Goal != nil {
			if u.Title != nil {
				blob.Title = strings.TrimSpace(*u.Title)
			}
			if u.Goal != nil {
				blob.Goal = *u.Goal
			}
			ph.Version++
			if e, err = s.seal(ctx, tx, projectID, "phase", ph.ID, ph.Version, blob); err != nil {
				return err
			}
		}
		ph.Title, ph.Goal = blob.Title, blob.Goal
		return tx.QueryRow(ctx,
			`UPDATE phases SET status = $2, version = $3, ciphertext = $4, nonce = $5, wrapped_dek = $6, project_key_version = $7, updated_at = now()
			 WHERE id = $1 RETURNING updated_at`,
			ph.ID, ph.Status, ph.Version, e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion,
		).Scan(&ph.UpdatedAt)
	})
	if err != nil {
		return Phase{}, fmt.Errorf("plans: update phase: %w", quota.Translate(err))
	}
	return ph, nil
}

// DeletePhase removes a phase. Its plans stay, outside any phase: deleting
// how work was grouped is not deleting the work.
func (s *Service) DeletePhase(ctx context.Context, principalID, projectID, phaseID uuid.UUID) error {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return err
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM phases WHERE id = $1 AND project_id = $2`, phaseID, projectID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID,
			Action: "phase.delete", ResourceType: "phase", ResourceID: &phaseID, Result: audit.ResultAllow,
		})
	})
	if err != nil {
		return fmt.Errorf("plans: delete phase: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------- plans

func validateNewItems(items []NewItem) error {
	if len(items) > MaxItemsPerPlan {
		return invalid("a plan holds at most %d items", MaxItemsPerPlan)
	}
	for i, it := range items {
		if strings.TrimSpace(it.Text) == "" {
			return invalid("item %d has no text", i+1)
		}
		if err := checkLen("item text", it.Text, MaxItemText); err != nil {
			return err
		}
		if it.Kind != "" && !validKind(it.Kind) {
			return invalid("item kind %q is not task or check", it.Kind)
		}
	}
	return nil
}

func (s *Service) checkPhase(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, phaseID *uuid.UUID) error {
	if phaseID == nil {
		return nil
	}
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM phases WHERE id = $1 AND project_id = $2)`, *phaseID, projectID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return invalid("phase %s is not in this project", *phaseID)
	}
	return nil
}

// CreatePlan writes a plan and its items in one transaction.
func (s *Service) CreatePlan(
	ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID uuid.UUID,
	phaseID *uuid.UUID, title, body string, items []NewItem,
) (Plan, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Plan{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return Plan{}, invalid("a plan needs a title")
	}
	if err := errors.Join(checkLen("title", title, MaxTitle), checkLen("body", body, MaxPlanBody), validateNewItems(items)); err != nil {
		return Plan{}, err
	}

	p := Plan{
		ID: uuid.New(), ProjectID: projectID, PhaseID: phaseID, Status: PlanActive, Version: 1,
		Title: title, Body: body, CreatedBy: principalID, CreatedByClient: clientID,
	}
	blob := planBlob{Title: title, Body: body, Items: map[uuid.UUID]itemBlob{}}
	for i, it := range items {
		kind := it.Kind
		if kind == "" {
			kind = KindTask
		}
		item := Item{ID: uuid.New(), PlanID: p.ID, Position: i + 1, Kind: kind, Status: ItemOpen, Text: strings.TrimSpace(it.Text)}
		blob.Items[item.ID] = itemBlob{Text: item.Text}
		p.Items = append(p.Items, item)
	}

	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := s.checkPhase(ctx, tx, projectID, phaseID); err != nil {
			return err
		}
		e, err := s.seal(ctx, tx, projectID, "plan", p.ID, p.Version, blob)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO plans (id, project_id, phase_id, status, version, ciphertext, nonce, wrapped_dek, project_key_version, created_by_principal, created_by_client)
			 VALUES ($1, $2, $3, $4, 1, $5, $6, $7, $8, $9, $10) RETURNING created_at, updated_at`,
			p.ID, projectID, phaseID, p.Status, e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion, principalID, clientID,
		).Scan(&p.CreatedAt, &p.UpdatedAt); err != nil {
			return err
		}
		for i := range p.Items {
			if err := insertItem(ctx, tx, projectID, &p.Items[i]); err != nil {
				return err
			}
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID, ClientID: clientID,
			Action: "plan.create", ResourceType: "plan", ResourceID: &p.ID, Result: audit.ResultAllow,
		})
	})
	if err != nil {
		return Plan{}, fmt.Errorf("plans: create plan: %w", quota.Translate(err))
	}
	return p, nil
}

func insertItem(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, it *Item) error {
	return tx.QueryRow(ctx,
		`INSERT INTO plan_items (id, project_id, plan_id, position, kind, status) VALUES ($1, $2, $3, $4, $5, $6) RETURNING updated_at`,
		it.ID, projectID, it.PlanID, it.Position, it.Kind, it.Status,
	).Scan(&it.UpdatedAt)
}

// planRow is a plan as stored, before its blob is opened.
type planRow struct {
	Plan
	e envelope
}

const planColumns = `id, project_id, phase_id, status, version, ciphertext, nonce, wrapped_dek, project_key_version,
	created_by_principal, created_by_client, created_at, updated_at`

func scanPlan(row pgx.Row, r *planRow) error {
	return row.Scan(&r.ID, &r.ProjectID, &r.PhaseID, &r.Status, &r.Version, &r.e.ciphertext, &r.e.nonce, &r.e.wrappedDEK, &r.e.keyVersion,
		&r.CreatedBy, &r.CreatedByClient, &r.CreatedAt, &r.UpdatedAt)
}

// loadPlan reads one plan with its items and their effort, decrypted.
// forUpdate locks the plan row, which is what serializes text edits.
func (s *Service) loadPlan(ctx context.Context, tx pgx.Tx, projectID, planID uuid.UUID, forUpdate bool) (Plan, planBlob, error) {
	q := `SELECT ` + planColumns + ` FROM plans WHERE id = $1 AND project_id = $2`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	var r planRow
	if err := scanPlan(tx.QueryRow(ctx, q, planID, projectID), &r); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Plan{}, planBlob{}, ErrNotFound
		}
		return Plan{}, planBlob{}, err
	}
	var blob planBlob
	if err := s.open(ctx, tx, projectID, "plan", r.ID, r.Version, r.e, &blob); err != nil {
		return Plan{}, planBlob{}, err
	}
	p := r.Plan
	p.Title, p.Body = blob.Title, blob.Body

	items, err := loadItems(ctx, tx, `WHERE i.plan_id = $1`, planID)
	if err != nil {
		return Plan{}, planBlob{}, err
	}
	for i := range items {
		t := blob.Items[items[i].ID]
		items[i].Text, items[i].Note = t.Text, t.Note
	}
	p.Items = items
	return p, blob, nil
}

// loadItems reads item metadata with each item's total effort. where is a
// fixed clause from this package, never caller input.
func loadItems(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]Item, error) {
	rows, err := tx.Query(ctx,
		`SELECT i.id, i.plan_id, i.position, i.kind, i.status, i.assignee_principal, i.updated_by_principal, i.updated_at, i.completed_at,
		        COALESCE(e.seconds, 0), COALESCE(e.tokens_in, 0), COALESCE(e.tokens_out, 0)
		 FROM plan_items i
		 LEFT JOIN (SELECT item_id, sum(seconds)::bigint AS seconds, sum(tokens_in)::bigint AS tokens_in, sum(tokens_out)::bigint AS tokens_out
		            FROM plan_item_effort GROUP BY item_id) e ON e.item_id = i.id
		 `+where+` ORDER BY i.plan_id, i.position`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.PlanID, &it.Position, &it.Kind, &it.Status, &it.Assignee, &it.UpdatedBy, &it.UpdatedAt, &it.CompletedAt,
			&it.Effort.Seconds, &it.Effort.TokensIn, &it.Effort.TokensOut); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Service) rewritePlan(ctx context.Context, tx pgx.Tx, p *Plan, blob planBlob) error {
	p.Version++
	e, err := s.seal(ctx, tx, p.ProjectID, "plan", p.ID, p.Version, blob)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx,
		`UPDATE plans SET version = $2, ciphertext = $3, nonce = $4, wrapped_dek = $5, project_key_version = $6, updated_at = now()
		 WHERE id = $1 RETURNING updated_at`,
		p.ID, p.Version, e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion,
	).Scan(&p.UpdatedAt)
}

func (s *Service) GetPlan(ctx context.Context, principalID, projectID, planID uuid.UUID) (Plan, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return Plan{}, err
	}
	var p Plan
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		p, _, err = s.loadPlan(ctx, tx, projectID, planID, false)
		return err
	})
	if err != nil {
		return Plan{}, fmt.Errorf("plans: get plan: %w", err)
	}
	return p, nil
}

// UpdatePlan changes a plan's status, its phase, or rewrites its own text.
func (s *Service) UpdatePlan(ctx context.Context, principalID, projectID, planID uuid.UUID, u PlanUpdate) (Plan, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Plan{}, err
	}
	if u.Status != nil && !validPlanStatus(*u.Status) {
		return Plan{}, invalid("plan status %q is not one of draft, active, done, abandoned", *u.Status)
	}
	if u.Title != nil {
		if strings.TrimSpace(*u.Title) == "" {
			return Plan{}, invalid("a plan needs a title")
		}
		if err := checkLen("title", *u.Title, MaxTitle); err != nil {
			return Plan{}, err
		}
	}
	if u.Body != nil {
		if err := checkLen("body", *u.Body, MaxPlanBody); err != nil {
			return Plan{}, err
		}
	}

	var p Plan
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var blob planBlob
		var err error
		p, blob, err = s.loadPlan(ctx, tx, projectID, planID, true)
		if err != nil {
			return err
		}
		if u.Title != nil || u.Body != nil {
			if u.ExpectedVersion != p.Version {
				return ErrConflict
			}
			if u.Title != nil {
				blob.Title = strings.TrimSpace(*u.Title)
			}
			if u.Body != nil {
				blob.Body = *u.Body
			}
			if err := s.rewritePlan(ctx, tx, &p, blob); err != nil {
				return err
			}
			p.Title, p.Body = blob.Title, blob.Body
		}
		if u.PhaseSet {
			if err := s.checkPhase(ctx, tx, projectID, u.PhaseID); err != nil {
				return err
			}
			p.PhaseID = u.PhaseID
		}
		if u.Status != nil {
			p.Status = *u.Status
		}
		return tx.QueryRow(ctx,
			`UPDATE plans SET status = $2, phase_id = $3, updated_at = now() WHERE id = $1 RETURNING updated_at`,
			p.ID, p.Status, p.PhaseID,
		).Scan(&p.UpdatedAt)
	})
	if err != nil {
		return Plan{}, fmt.Errorf("plans: update plan: %w", quota.Translate(err))
	}
	return p, nil
}

func (s *Service) DeletePlan(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, planID uuid.UUID) error {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return err
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM plans WHERE id = $1 AND project_id = $2`, planID, projectID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID, ClientID: clientID,
			Action: "plan.delete", ResourceType: "plan", ResourceID: &planID, Result: audit.ResultAllow,
		})
	})
	if err != nil {
		return fmt.Errorf("plans: delete plan: %w", err)
	}
	return nil
}
