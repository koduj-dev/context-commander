package gates

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

// Engine resolves the effective gate_bindings for a hook (installation +
// organization + project scope, mirroring
// internal/skills.Service.ListEffective's composition) and runs each bound
// Gate, aggregating verdicts.
type Engine struct {
	pool *db.Pool
}

func NewEngine(pool *db.Pool) *Engine {
	return &Engine{pool: pool}
}

type binding struct {
	gateName string
	config   json.RawMessage
}

// Evaluate runs every enabled gate bound to hook for projectID (and orgID,
// if the project belongs to one) and returns the aggregate Result. A
// binding naming a gate that isn't registered (Register) is skipped rather
// than failing the whole evaluation — a missing/renamed provider is a
// deployment mismatch to fix, not a reason to block every write in the
// meantime.
func (e *Engine) Evaluate(ctx context.Context, principalID, projectID uuid.UUID, orgID *uuid.UUID, hook string, payload []byte) (Result, error) {
	var bindings []binding
	err := db.WithPrincipal(ctx, e.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT gate_name, config FROM gate_bindings
			 WHERE hook = $1 AND enabled = true
			   AND (scope = 'installation'
			        OR (scope = 'organization' AND scope_ref = $2)
			        OR (scope = 'project' AND scope_ref = $3))`,
			hook, orgID, projectID,
		)
		if err != nil {
			return fmt.Errorf("query gate_bindings: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var b binding
			if err := rows.Scan(&b.gateName, &b.config); err != nil {
				return fmt.Errorf("scan gate_bindings row: %w", err)
			}
			bindings = append(bindings, b)
		}
		return rows.Err()
	})
	if err != nil {
		return Result{}, fmt.Errorf("gates: resolve bindings: %w", err)
	}

	result := Result{Verdict: Allow}
	for _, b := range bindings {
		gate, ok := Get(b.gateName)
		if !ok {
			continue
		}
		r, err := gate.Evaluate(ctx, HookContext{Hook: hook, ProjectID: projectID, Payload: payload, Config: b.config})
		if err != nil {
			return Result{}, fmt.Errorf("gates: evaluate %q: %w", b.gateName, err)
		}
		result = combine(result, r)
		if result.Verdict == Deny {
			return result, nil
		}
	}
	return result, nil
}
