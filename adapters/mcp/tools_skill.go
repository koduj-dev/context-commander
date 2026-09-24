package mcp

import (
	"context"
	"errors"

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

// resolveScopeTarget turns the (scope, scope_ref) pair a caller names into
// the domain's form. scope_ref is accepted as a project PATH for project
// scope, because that is how every other tool in this package names a
// project and an agent should not have to look up a UUID to publish.
func resolveScopeTarget(
	ctx context.Context,
	resolve func(context.Context, string) (uuid.UUID, error),
	scope, scopeRef string,
) (skills.Scope, *uuid.UUID, error) {
	if scope == "" {
		scope = string(skills.ScopeProject)
	}
	switch skills.Scope(scope) {
	case skills.ScopeInstallation:
		return skills.ScopeInstallation, nil, nil
	case skills.ScopeProject:
		if scopeRef == "" {
			return "", nil, errors.New(`scope "project" needs scope_ref: the project path "namespace/slug"`)
		}
		projectID, err := resolve(ctx, scopeRef)
		if err != nil {
			return "", nil, err
		}
		return skills.ScopeProject, &projectID, nil
	case skills.ScopeOrganization:
		if scopeRef == "" {
			return "", nil, errors.New(`scope "organization" needs scope_ref: the organization UUID`)
		}
		orgID, err := uuid.Parse(scopeRef)
		if err != nil {
			return "", nil, err
		}
		return skills.ScopeOrganization, &orgID, nil
	default:
		return "", nil, errors.New(`scope must be "project", "organization" or "installation"`)
	}
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
		Scope                string   `json:"scope,omitempty" jsonschema:"where the skill should live: \"project\" (default), \"organization\" or \"installation\". Installation puts it on the shelf every project here can search."`
		ScopeRef             string   `json:"scope_ref,omitempty" jsonschema:"project path \"namespace/slug\" or organization UUID for the chosen scope; omit for installation scope"`
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
			"published version's content. A new skill is created inside the project you publish from and is " +
			"visible only to that project's members until someone promotes it (skill.promote); it is NOT " +
			"installation-wide by default. Even so, do not put secrets in skill content — use the secret tools " +
			"for anything that must stay confidential.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args publishArgs) (*sdkmcp.CallToolResult, skillVersionResult, error) {
		scope, scopeRef, err := resolveScopeTarget(ctx, resolve, args.Scope, args.ScopeRef)
		if err != nil {
			return nil, skillVersionResult{}, err
		}
		userID, err := resolveUserID(ctx)
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
		v, err := deps.Skills.Publish(ctx, principalID, userID, scope, scopeRef, skillID, args.Name, args.Author, args.Origin, []byte(args.Content), args.DeclaredCapabilities)
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

	type getArgs struct {
		SkillVersionID string `json:"skill_version_id" jsonschema:"skill version UUID — from skill.search (latest_version_id), skill.versions, or skill.list_effective"`
	}
	type skillContentResult struct {
		skillVersionResult
		SkillName   string `json:"skill_name"`
		SkillAuthor string `json:"skill_author,omitempty"`
		SkillOrigin string `json:"skill_origin,omitempty"`
		Content     string `json:"content"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "skill.get",
		Description: "Read a skill version's actual content — the instructions themselves, not just the " +
			"binding. skill.list_effective returns skill_version_id values; this is what turns one into " +
			"something you can load and follow. Content is verified against the checksum recorded when the " +
			"version was published.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args getArgs) (*sdkmcp.CallToolResult, skillContentResult, error) {
		skillVersionID, err := uuid.Parse(args.SkillVersionID)
		if err != nil {
			return nil, skillContentResult{}, err
		}
		v, err := deps.Skills.GetVersion(ctx, principalID, skillVersionID)
		if err != nil {
			return nil, skillContentResult{}, err
		}
		return nil, skillContentResult{
			skillVersionResult: toSkillVersionResult(v.Version),
			SkillName:          v.Skill.Name,
			SkillAuthor:        v.Skill.Author,
			SkillOrigin:        v.Skill.Origin,
			Content:            string(v.Content),
		}, nil
	})

	type searchArgs struct {
		Query  string `json:"query,omitempty" jsonschema:"match skill names containing this text, case-insensitive; omit to list everything visible"`
		Limit  int    `json:"limit,omitempty" jsonschema:"max entries to return (default 100, max 500)"`
		Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call to continue"`
	}
	type catalogEntryResult struct {
		SkillID  string `json:"skill_id"`
		Name     string `json:"name"`
		Author   string `json:"author,omitempty"`
		Origin   string `json:"origin,omitempty"`
		Scope    string `json:"scope" jsonschema:"where the skill lives: installation (visible to everyone here), organization, or project"`
		ScopeRef string `json:"scope_ref,omitempty"`

		LatestVersionID string `json:"latest_version_id" jsonschema:"pass this to skill.get to download the newest content"`
		LatestVersion   int    `json:"latest_version"`
		LatestChecksum  string `json:"latest_checksum" jsonschema:"sha256 of the newest package; remember it and compare later to tell whether the skill changed"`
		TrustStatus     string `json:"trust_status"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "skill.search",
		Description: "Browse the skill registry by name. This is how you FIND a skill you do not already " +
			"have an id for: search by name, then call skill.get with the latest_version_id to download its " +
			"content. Returns what you are allowed to see — skills in your projects, in your organizations, and " +
			"those promoted to the installation-wide shelf. Each entry carries latest_checksum: remember it " +
			"alongside whatever you loaded, and compare on a later search to tell whether the skill changed and " +
			"needs re-downloading, without fetching the content again.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args searchArgs) (*sdkmcp.CallToolResult, pagedResult[catalogEntryResult], error) {
		page, err := deps.Skills.Search(ctx, principalID, args.Query, pagingOptions(args.Limit, args.Cursor))
		if err != nil {
			return nil, pagedResult[catalogEntryResult]{}, err
		}
		out := make([]catalogEntryResult, len(page.Items))
		for i, e := range page.Items {
			out[i] = catalogEntryResult{
				SkillID: e.SkillID.String(), Name: e.Name, Author: e.Author, Origin: e.Origin,
				Scope:           string(e.Scope),
				LatestVersionID: e.LatestVersionID.String(), LatestVersion: e.LatestVersion,
				LatestChecksum: e.LatestChecksum, TrustStatus: e.TrustStatus,
			}
			if e.ScopeRef != nil {
				out[i].ScopeRef = e.ScopeRef.String()
			}
		}
		return nil, paged(out, page.NextCursor), nil
	})

	type infoArgs struct {
		SkillID string `json:"skill_id" jsonschema:"skill UUID — from skill.list_effective, skill.search, or an install"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "skill.info",
		Description: "Look up one skill by id: its name, who published it, where it lives, and its newest " +
			"version. Use it when you already HAVE an id — skill.list_effective returns bindings that name a " +
			"skill_id and nothing else, so this is what turns one into something you can talk about. Searching " +
			"for it by name instead is a scan, and it finds nothing once the name falls past the page you asked " +
			"for.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args infoArgs) (*sdkmcp.CallToolResult, catalogEntryResult, error) {
		skillID, err := uuid.Parse(args.SkillID)
		if err != nil {
			return nil, catalogEntryResult{}, err
		}
		e, err := deps.Skills.Get(ctx, principalID, skillID)
		if err != nil {
			return nil, catalogEntryResult{}, err
		}
		out := catalogEntryResult{
			SkillID: e.SkillID.String(), Name: e.Name, Author: e.Author, Origin: e.Origin,
			Scope:           string(e.Scope),
			LatestVersionID: e.LatestVersionID.String(), LatestVersion: e.LatestVersion,
			LatestChecksum: e.LatestChecksum, TrustStatus: e.TrustStatus,
		}
		if e.ScopeRef != nil {
			out.ScopeRef = e.ScopeRef.String()
		}
		return nil, out, nil
	})

	type versionsArgs struct {
		SkillID string `json:"skill_id" jsonschema:"skill UUID from skill.search"`
		Limit   int    `json:"limit,omitempty"`
		Cursor  string `json:"cursor,omitempty"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "skill.versions",
		Description: "List a skill's published versions, newest first, with each one's checksum. Use it to pin " +
			"an older version deliberately, or to see what changed since the version you are running.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args versionsArgs) (*sdkmcp.CallToolResult, pagedResult[skillVersionResult], error) {
		skillID, err := uuid.Parse(args.SkillID)
		if err != nil {
			return nil, pagedResult[skillVersionResult]{}, err
		}
		page, err := deps.Skills.ListVersions(ctx, principalID, skillID, pagingOptions(args.Limit, args.Cursor))
		if err != nil {
			return nil, pagedResult[skillVersionResult]{}, err
		}
		out := make([]skillVersionResult, len(page.Items))
		for i, v := range page.Items {
			out[i] = toSkillVersionResult(v)
		}
		return nil, paged(out, page.NextCursor), nil
	})

	type promoteArgs struct {
		SkillID  string `json:"skill_id"`
		Scope    string `json:"scope" jsonschema:"target scope: \"organization\" or \"installation\""`
		ScopeRef string `json:"scope_ref,omitempty" jsonschema:"organization UUID (omit for installation scope)"`
	}
	type promoteResult struct {
		SkillID  string `json:"skill_id"`
		Scope    string `json:"scope"`
		ScopeRef string `json:"scope_ref,omitempty"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "skill.promote",
		Description: "Widen where a skill lives, so more people can find it: project -> organization -> " +
			"installation. Promoting to installation puts it on the shelf every project here can search. Only " +
			"ever widens, never narrows — anyone who could already see it may have installed it. Requires the " +
			"rights at the TARGET scope (organization admin, or installation admin).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args promoteArgs) (*sdkmcp.CallToolResult, promoteResult, error) {
		userID, err := resolveUserID(ctx)
		if err != nil {
			return nil, promoteResult{}, err
		}
		skillID, err := uuid.Parse(args.SkillID)
		if err != nil {
			return nil, promoteResult{}, err
		}
		var scopeRef *uuid.UUID
		if args.ScopeRef != "" {
			id, err := uuid.Parse(args.ScopeRef)
			if err != nil {
				return nil, promoteResult{}, err
			}
			scopeRef = &id
		}
		sk, err := deps.Skills.Promote(ctx, principalID, userID, skillID, skills.Scope(args.Scope), scopeRef)
		if err != nil {
			return nil, promoteResult{}, err
		}
		r := promoteResult{SkillID: sk.ID.String(), Scope: string(sk.Scope)}
		if sk.ScopeRef != nil {
			r.ScopeRef = sk.ScopeRef.String()
		}
		return nil, r, nil
	})

	type listEffectiveArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
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
		list, err := deps.Skills.ListEffective(ctx, principalID, projectID)
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
