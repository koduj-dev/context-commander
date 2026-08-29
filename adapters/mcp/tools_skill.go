package mcp

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/skills"
)

// ID fields are plain strings, not uuid.UUID — the MCP SDK derives a tool's
// output JSON Schema by reflecting over the Go type, and uuid.UUID reflects
// as a [16]byte array even though its MarshalJSON produces a string; the
// resulting schema/output mismatch fails the SDK's own output validation
// (caught by adapters/mcp/mcp_test.go). Every other tools_*.go file in this
// package already stores UUIDs as string for this reason — this file and
// tools_gate.go were the two exceptions.
type skillVersionResult struct {
	ID                   string   `json:"id"`
	SkillID              string   `json:"skill_id"`
	Version              int      `json:"version"`
	Checksum             string   `json:"checksum"`
	DeclaredCapabilities []string `json:"declared_capabilities"`
	TrustStatus          string   `json:"trust_status"`
}

type skillBindingResult struct {
	ID             string `json:"id"`
	Scope          string `json:"scope"`
	ScopeRef       string `json:"scope_ref,omitempty"`
	SkillID        string `json:"skill_id"`
	SkillVersionID string `json:"skill_version_id"`
	Pinned         bool   `json:"pinned"`
	Managed        bool   `json:"managed"`
	Overridable    bool   `json:"overridable"`
}

func toSkillVersionResult(v skills.SkillVersion) skillVersionResult {
	return skillVersionResult{
		ID: v.ID.String(), SkillID: v.SkillID.String(), Version: v.Version, Checksum: v.Checksum,
		DeclaredCapabilities: v.DeclaredCapabilities, TrustStatus: v.TrustStatus,
	}
}

func toSkillBindingResult(b skills.SkillBinding) skillBindingResult {
	r := skillBindingResult{
		ID: b.ID.String(), Scope: string(b.Scope),
		SkillID: b.SkillID.String(), SkillVersionID: b.SkillVersionID.String(),
		Pinned: b.Pinned, Managed: b.Managed, Overridable: b.Overridable,
	}
	if b.ScopeRef != nil {
		r.ScopeRef = b.ScopeRef.String()
	}
	return r
}

// registerSkillTools mirrors registerArtifactTools/registerProjectTools:
// every tool resolves a "project" path argument via deps.Projects.Resolve,
// then calls straight into deps.Skills — no business logic here.
func registerSkillTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	_ = clientID // not needed by any skill tool; kept for signature symmetry with registerArtifactTools

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

	// resolveUserID looks up the User principalID belongs to — needed for
	// Install/Override's organization-scope authorization check
	// (organization_members is keyed by user, docs/adr/0006).
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

	type publishArgs struct {
		Project              string   `json:"project" jsonschema:"project path \"namespace/slug\", used to authorize the publish"`
		SkillID              string   `json:"skill_id,omitempty" jsonschema:"existing skill UUID to publish a new version under (omit to create a brand-new skill)"`
		Name                 string   `json:"name,omitempty" jsonschema:"skill name (required when skill_id is omitted)"`
		Author               string   `json:"author,omitempty"`
		Origin               string   `json:"origin,omitempty"`
		Content              string   `json:"content" jsonschema:"the skill's packaged content"`
		DeclaredCapabilities []string `json:"declared_capabilities,omitempty" jsonschema:"e.g. [\"network\", \"filesystem\"] — see docs/architecture-cookbook.md §12.4"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "skill.publish",
		Description: "Publish a new Skill or SkillVersion. Always a pure insert — never mutates an existing " +
			"published version's content (docs/adr/0012).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args publishArgs) (*sdkmcp.CallToolResult, skillVersionResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, skillVersionResult{}, err
		}
		var skillID *uuid.UUID
		if args.SkillID != "" {
			id, err := uuid.Parse(args.SkillID)
			if err != nil {
				return nil, skillVersionResult{}, err
			}
			skillID = &id
		}
		v, err := deps.Skills.Publish(ctx, principalID, projectID, skillID, args.Name, args.Author, args.Origin, []byte(args.Content), args.DeclaredCapabilities)
		if err != nil {
			return nil, skillVersionResult{}, err
		}
		return nil, toSkillVersionResult(v), nil
	})

	type installArgs struct {
		Project        string `json:"project" jsonschema:"project path \"namespace/slug\", used to authorize the install"`
		Scope          string `json:"scope" jsonschema:"\"installation\" | \"organization\" | \"project\""`
		ScopeRef       string `json:"scope_ref,omitempty" jsonschema:"organization or project UUID the binding applies to (omit for installation scope)"`
		SkillID        string `json:"skill_id" jsonschema:"skill UUID"`
		SkillVersionID string `json:"skill_version_id" jsonschema:"skill version UUID to pin to"`
		Managed        bool   `json:"managed,omitempty"`
		Overridable    bool   `json:"overridable,omitempty" jsonschema:"whether a narrower scope may shadow this binding (docs/adr/0012)"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "skill.install",
		Description: "Create a SkillBinding — \"project X uses skill Y\" (or organization/installation).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args installArgs) (*sdkmcp.CallToolResult, skillBindingResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, skillBindingResult{}, err
		}
		userID, err := resolveUserID(ctx)
		if err != nil {
			return nil, skillBindingResult{}, err
		}
		skillID, err := uuid.Parse(args.SkillID)
		if err != nil {
			return nil, skillBindingResult{}, err
		}
		skillVersionID, err := uuid.Parse(args.SkillVersionID)
		if err != nil {
			return nil, skillBindingResult{}, err
		}
		var scopeRef *uuid.UUID
		if args.ScopeRef != "" {
			id, err := uuid.Parse(args.ScopeRef)
			if err != nil {
				return nil, skillBindingResult{}, err
			}
			scopeRef = &id
		}
		b, err := deps.Skills.Install(ctx, principalID, userID, projectID, skills.Scope(args.Scope), scopeRef, skillID, skillVersionID, args.Managed, args.Overridable)
		if err != nil {
			return nil, skillBindingResult{}, err
		}
		return nil, toSkillBindingResult(b), nil
	})

	type overrideArgs struct {
		Project        string `json:"project" jsonschema:"project path \"namespace/slug\" — the override always applies at project scope"`
		SkillID        string `json:"skill_id"`
		SkillVersionID string `json:"skill_version_id"`
		Managed        bool   `json:"managed,omitempty"`
		Overridable    bool   `json:"overridable,omitempty"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "skill.override",
		Description: "Create a project-scope binding that shadows an organization/installation binding of the " +
			"same skill. Fails if that binding has overridable=false (docs/adr/0012).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args overrideArgs) (*sdkmcp.CallToolResult, skillBindingResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, skillBindingResult{}, err
		}
		skillID, err := uuid.Parse(args.SkillID)
		if err != nil {
			return nil, skillBindingResult{}, err
		}
		skillVersionID, err := uuid.Parse(args.SkillVersionID)
		if err != nil {
			return nil, skillBindingResult{}, err
		}
		b, err := deps.Skills.Override(ctx, principalID, projectID, skillID, skillVersionID, args.Managed, args.Overridable)
		if err != nil {
			return nil, skillBindingResult{}, err
		}
		return nil, toSkillBindingResult(b), nil
	})

	type bindingArgs struct {
		Project   string `json:"project" jsonschema:"project path \"namespace/slug\""`
		BindingID string `json:"binding_id"`
	}
	type pinResult struct {
		Pinned bool `json:"pinned"`
	}
	registerPinTool := func(name string, pinned bool) {
		sdkmcp.AddTool(server, &sdkmcp.Tool{
			Name:        name,
			Description: "Set a project-scope SkillBinding's pinned flag.",
		}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args bindingArgs) (*sdkmcp.CallToolResult, pinResult, error) {
			projectID, err := resolve(ctx, args.Project)
			if err != nil {
				return nil, pinResult{}, err
			}
			bindingID, err := uuid.Parse(args.BindingID)
			if err != nil {
				return nil, pinResult{}, err
			}
			if pinned {
				err = deps.Skills.Pin(ctx, principalID, projectID, bindingID)
			} else {
				err = deps.Skills.Unpin(ctx, principalID, projectID, bindingID)
			}
			if err != nil {
				return nil, pinResult{}, err
			}
			return nil, pinResult{Pinned: pinned}, nil
		})
	}
	registerPinTool("skill.pin", true)
	registerPinTool("skill.unpin", false)

	type listEffectiveArgs struct {
		Project        string `json:"project" jsonschema:"project path \"namespace/slug\""`
		OrganizationID string `json:"organization_id,omitempty" jsonschema:"organization UUID, if the project belongs to one"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "skill.list_effective",
		Description: "List the effective skill bindings for a project — installation + organization + project " +
			"scope composed, narrowest scope wins per skill (docs/architecture-cookbook.md §12.1).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listEffectiveArgs) (*sdkmcp.CallToolResult, []skillBindingResult, error) {
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
		list, err := deps.Skills.ListEffective(ctx, principalID, projectID, orgID)
		if err != nil {
			return nil, nil, err
		}
		out := make([]skillBindingResult, len(list))
		for i, b := range list {
			out[i] = toSkillBindingResult(b)
		}
		return nil, out, nil
	})
}
