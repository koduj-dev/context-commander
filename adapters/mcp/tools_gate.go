package mcp

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/gates"
)

// ID/ScopeRef are plain strings, not uuid.UUID, and Config is `any`, not
// json.RawMessage — see the comment on skillBindingResult in
// tools_skill.go: the MCP SDK's reflection-based schema disagrees with the
// actual JSON encoding for both uuid.UUID (reflects as [16]byte, an array)
// and json.RawMessage (reflects as []byte, also an array) even though both
// marshal to something else (a string, an object) at runtime.
type gateBindingResult struct {
	ID       string `json:"id"`
	Scope    string `json:"scope"`
	ScopeRef string `json:"scope_ref,omitempty"`
	GateName string `json:"gate_name"`
	Hook     string `json:"hook"`
	Enabled  bool   `json:"enabled"`
	Config   any    `json:"config"`
}

func toGateBindingResult(b gates.Binding) gateBindingResult {
	r := gateBindingResult{
		ID: b.ID.String(), Scope: string(b.Scope),
		GateName: b.GateName, Hook: b.Hook, Enabled: b.Enabled,
	}
	if b.ScopeRef != nil {
		r.ScopeRef = b.ScopeRef.String()
	}
	if len(b.Config) > 0 {
		_ = json.Unmarshal(b.Config, &r.Config)
	}
	return r
}

// registerGateTools exposes gate_bindings management only (bind/unbind/
// list) — never the evaluation itself, which is not a capability-gated
// action a calling agent takes (docs/architecture-cookbook.md §13.3: gate
// enforcement happens server-side, inside the domain services that call
// internal/gates.Engine, not as something a client invokes directly).
func registerGateTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	_ = clientID // kept for signature symmetry with registerArtifactTools/registerSkillTools

	resolve := func(ctx context.Context, projectPath string) (uuid.UUID, error) {
		var id uuid.UUID
		err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			p, err := deps.Projects.Resolve(ctx, tx, projectPath)
			if err != nil {
				return err
			}
			id = p.ID
			return nil
		})
		return id, err
	}

	resolveUserID := func(ctx context.Context) (uuid.UUID, error) {
		var userID uuid.UUID
		err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			var nullableUserID *uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT user_id FROM principals WHERE id = $1`, principalID).Scan(&nullableUserID); err != nil {
				return err
			}
			if nullableUserID != nil {
				userID = *nullableUserID
			}
			return nil
		})
		return userID, err
	}

	type listArgs struct {
		Project        string `json:"project" jsonschema:"project path \"namespace/slug\""`
		OrganizationID string `json:"organization_id,omitempty" jsonschema:"organization UUID, if the project belongs to one"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "gate.list_bindings",
		Description: "List gate_bindings visible to a project — installation + organization + project scope.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, []gateBindingResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, nil, err
		}
		var orgID *uuid.UUID
		if args.OrganizationID != "" {
			id, err := uuid.Parse(args.OrganizationID)
			if err != nil {
				return nil, nil, err
			}
			orgID = &id
		}
		list, err := deps.Gates.ListBindings(ctx, principalID, projectID, orgID)
		if err != nil {
			return nil, nil, err
		}
		out := make([]gateBindingResult, len(list))
		for i, b := range list {
			out[i] = toGateBindingResult(b)
		}
		return nil, out, nil
	})

	type bindArgs struct {
		Project  string         `json:"project" jsonschema:"project path \"namespace/slug\", used to authorize the bind"`
		Scope    string         `json:"scope" jsonschema:"\"organization\" | \"project\" (installation scope is operator-seeded only)"`
		ScopeRef string         `json:"scope_ref,omitempty" jsonschema:"organization or project UUID the binding applies to"`
		GateName string         `json:"gate_name" jsonschema:"registered gate name, e.g. \"secret-scanner\""`
		Hook     string         `json:"hook" jsonschema:"lifecycle hook, e.g. \"artifact.before_write\""`
		Config   map[string]any `json:"config,omitempty" jsonschema:"gate-specific configuration object"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "gate.bind",
		Description: "Bind a registered gate to a lifecycle hook for a scope.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args bindArgs) (*sdkmcp.CallToolResult, gateBindingResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, gateBindingResult{}, err
		}
		userID, err := resolveUserID(ctx)
		if err != nil {
			return nil, gateBindingResult{}, err
		}
		var scopeRef *uuid.UUID
		if args.ScopeRef != "" {
			id, err := uuid.Parse(args.ScopeRef)
			if err != nil {
				return nil, gateBindingResult{}, err
			}
			scopeRef = &id
		}
		var config json.RawMessage
		if args.Config != nil {
			config, err = json.Marshal(args.Config)
			if err != nil {
				return nil, gateBindingResult{}, err
			}
		}
		b, err := deps.Gates.Bind(ctx, principalID, userID, projectID, gates.Scope(args.Scope), scopeRef, args.GateName, args.Hook, config)
		if err != nil {
			return nil, gateBindingResult{}, err
		}
		return nil, toGateBindingResult(b), nil
	})

	type unbindArgs struct {
		Project   string `json:"project" jsonschema:"project path \"namespace/slug\", used to authorize the unbind"`
		BindingID string `json:"binding_id"`
	}
	type unbindResult struct {
		Unbound bool `json:"unbound"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "gate.unbind",
		Description: "Delete a gate binding.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args unbindArgs) (*sdkmcp.CallToolResult, unbindResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, unbindResult{}, err
		}
		userID, err := resolveUserID(ctx)
		if err != nil {
			return nil, unbindResult{}, err
		}
		bindingID, err := uuid.Parse(args.BindingID)
		if err != nil {
			return nil, unbindResult{}, err
		}
		if err := deps.Gates.Unbind(ctx, principalID, userID, projectID, bindingID); err != nil {
			return nil, unbindResult{}, err
		}
		return nil, unbindResult{Unbound: true}, nil
	})
}
