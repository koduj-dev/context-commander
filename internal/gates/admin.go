package gates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/organizations"
)

// Scope is where a gate_bindings row applies — mirrors internal/skills.Scope
// (kept as its own type rather than imported: gates and skills bindings are
// independent tables, and nothing requires their scope enums to be the same
// Go type, only the same three string values the "installation" |
// "organization" | "project" CHECK constraint allows).
type Scope string

const (
	ScopeInstallation Scope = "installation"
	ScopeOrganization Scope = "organization"
	ScopeProject      Scope = "project"
)

var (
	ErrNotFound  = errors.New("gates: not found")
	ErrForbidden = errors.New("gates: forbidden")

	// ErrUnsupportedScope is returned by AdminService.Bind for
	// scope == ScopeInstallation — same reasoning as
	// internal/skills.ErrUnsupportedScope: v1 has no installation-admin
	// capability model, so installation-scope bindings are operator-seeded
	// directly in the database, never created through this service.
	ErrUnsupportedScope = errors.New("gates: installation-scope bindings are operator-seeded only in v1, not creatable through AdminService")

	// ErrScopeRefRequired is returned by AdminService.Bind when scope is
	// "organization" or "project" but no scope_ref was given — a client
	// input mistake, not a server fault, so it must map to 400 rather than
	// the default 500 an unwrapped fmt.Errorf would fall through to.
	ErrScopeRefRequired = errors.New("gates: organization/project scope requires scope_ref")
)

// Binding is one gate_bindings row — data pointing at which registered Gate
// (Register) runs on which hook for which scope, distinct from Engine's
// internal, unauthenticated read of the same table during Evaluate.
type Binding struct {
	ID        uuid.UUID
	Scope     Scope
	ScopeRef  *uuid.UUID
	GateName  string
	Hook      string
	Enabled   bool
	Config    json.RawMessage
	CreatedAt time.Time
}

// AdminService is gate_bindings' authorization chokepoint (docs/adr/0014),
// kept deliberately separate from Engine: Engine.Evaluate runs on every
// artifact write and has no reason to carry an AuthzService dependency,
// while binding management is a rare, capability-gated administrative
// action. Splitting them also means adding this service required no change
// to Engine's constructor or any of its existing call sites.
type AdminService struct {
	pool  *db.Pool
	authz *auth.AuthzService
	orgs  *organizations.Store
}

func NewAdminService(pool *db.Pool, authz *auth.AuthzService, orgs *organizations.Store) *AdminService {
	return &AdminService{pool: pool, authz: authz, orgs: orgs}
}

func (s *AdminService) checkCapability(ctx context.Context, principalID, projectID uuid.UUID) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, auth.CapGateManage)
	if err != nil {
		return fmt.Errorf("gates: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// checkScopeTarget mirrors internal/skills.Service.checkScopeTarget
// exactly: ScopeOrganization requires the caller (identified by userID —
// organization_members is keyed by user, docs/adr/0006) to hold
// CapOrgManageMembers on that organization; ScopeProject is authorized by
// the anchor-project capability check the caller already passed;
// ScopeInstallation is rejected outright.
func (s *AdminService) checkScopeTarget(ctx context.Context, tx pgx.Tx, userID uuid.UUID, scope Scope, scopeRef *uuid.UUID) error {
	switch scope {
	case ScopeInstallation:
		return ErrUnsupportedScope
	case ScopeOrganization:
		if scopeRef == nil {
			return ErrScopeRefRequired
		}
		m, err := s.orgs.GetMembership(ctx, tx, *scopeRef, userID)
		if err != nil {
			return ErrForbidden
		}
		if !organizations.RoleCan(m.Role, organizations.CapOrgManageMembers) {
			return ErrForbidden
		}
		return nil
	case ScopeProject:
		if scopeRef == nil {
			return ErrScopeRefRequired
		}
		return nil
	default:
		return fmt.Errorf("gates: unknown scope %q", scope)
	}
}

// Bind creates a gate_bindings row. projectID anchors the capability check
// regardless of scope, same reasoning as internal/skills.Service.Install.
func (s *AdminService) Bind(
	ctx context.Context, principalID, userID, projectID uuid.UUID,
	scope Scope, scopeRef *uuid.UUID,
	gateName, hook string, config json.RawMessage,
) (Binding, error) {
	if err := s.checkCapability(ctx, principalID, projectID); err != nil {
		return Binding{}, err
	}
	if config == nil {
		config = json.RawMessage("{}")
	}

	b := Binding{
		ID: uuid.New(), Scope: scope, ScopeRef: scopeRef,
		GateName: gateName, Hook: hook, Enabled: true, Config: config,
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := s.checkScopeTarget(ctx, tx, userID, scope, scopeRef); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`INSERT INTO gate_bindings (id, scope, scope_ref, gate_name, hook, enabled, config)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 RETURNING created_at`,
			b.ID, b.Scope, b.ScopeRef, b.GateName, b.Hook, b.Enabled, []byte(b.Config),
		).Scan(&b.CreatedAt)
	})
	if err != nil {
		return Binding{}, fmt.Errorf("gates: bind: %w", err)
	}
	return b, nil
}

// Unbind deletes a gate_bindings row. It first reads the row's own
// scope/scope_ref so an organization-scope binding is authorized against
// that organization (not just the anchor project), matching Bind's rule.
func (s *AdminService) Unbind(ctx context.Context, principalID, userID, projectID, bindingID uuid.UUID) error {
	if err := s.checkCapability(ctx, principalID, projectID); err != nil {
		return err
	}
	return db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var scope Scope
		var scopeRef *uuid.UUID
		err := tx.QueryRow(ctx, `SELECT scope, scope_ref FROM gate_bindings WHERE id = $1`, bindingID).Scan(&scope, &scopeRef)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("gates: look up binding: %w", err)
		}
		if err := s.checkScopeTarget(ctx, tx, userID, scope, scopeRef); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM gate_bindings WHERE id = $1`, bindingID)
		if err != nil {
			return fmt.Errorf("gates: delete binding: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ListBindings returns every binding visible to projectID (and orgID, if
// the project belongs to one) — installation + organization + project
// scope, same composition query as Engine.Evaluate/internal/skills'
// ListEffective, but returned as raw rows rather than composed by GateName
// (an admin view needs to see every binding, not just the winning one).
func (s *AdminService) ListBindings(ctx context.Context, principalID, projectID uuid.UUID, orgID *uuid.UUID) ([]Binding, error) {
	if err := s.checkCapability(ctx, principalID, projectID); err != nil {
		return nil, err
	}

	var bindings []Binding
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, scope, scope_ref, gate_name, hook, enabled, config, created_at
			 FROM gate_bindings
			 WHERE scope = 'installation'
			    OR (scope = 'organization' AND scope_ref = $1)
			    OR (scope = 'project' AND scope_ref = $2)`,
			orgID, projectID,
		)
		if err != nil {
			return fmt.Errorf("query gate_bindings: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var b Binding
			var config []byte
			if err := rows.Scan(&b.ID, &b.Scope, &b.ScopeRef, &b.GateName, &b.Hook, &b.Enabled, &config, &b.CreatedAt); err != nil {
				return fmt.Errorf("scan gate_bindings row: %w", err)
			}
			b.Config = config
			bindings = append(bindings, b)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("gates: list bindings: %w", err)
	}
	return bindings, nil
}
