package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type projectResult struct {
	ID         string `json:"id"`
	Namespace  string `json:"namespace"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

func toProjectResult(p projects.Project, namespaceSlug string) projectResult {
	return projectResult{ID: p.ID.String(), Namespace: namespaceSlug, Slug: p.Slug, Name: p.Name, Visibility: string(p.Visibility)}
}

func registerProjectTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	_ = clientID // not yet needed by any project tool; kept for symmetry with registerArtifactTools

	type resolveArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\", e.g. filip/es-migration"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "project.resolve",
		Description: "Resolve a project by its \"namespace/slug\" path. Fails if it doesn't exist or you're not a member — the two look identical from the outside, by design.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args resolveArgs) (*sdkmcp.CallToolResult, projectResult, error) {
		namespaceSlug, _, _ := strings.Cut(args.Project, "/")
		var p projects.Project
		err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			var err error
			p, err = deps.Projects.Resolve(ctx, tx, args.Project)
			return err
		})
		if err != nil {
			return nil, projectResult{}, err
		}
		return nil, toProjectResult(p, namespaceSlug), nil
	})

	type createArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\" to create"`
		Name    string `json:"name" jsonschema:"display name"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "project.create",
		Description: "Create a project at \"namespace/slug\" and become its OWNER. Creates a personal namespace for you first if needed.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args createArgs) (*sdkmcp.CallToolResult, projectResult, error) {
		namespaceSlug, _, _ := strings.Cut(args.Project, "/")
		identityStore := identity.NewStore()
		var p projects.Project
		err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			principal, err := identityStore.GetPrincipal(ctx, tx, principalID)
			if err != nil {
				return err
			}
			if principal.UserID == nil {
				return errors.New("only a User principal can create a personal namespace this way")
			}
			p, err = deps.Projects.CreateForOwner(ctx, tx, *principal.UserID, principalID, args.Project, args.Name)
			return err
		})
		if err != nil {
			return nil, projectResult{}, err
		}
		return nil, toProjectResult(p, namespaceSlug), nil
	})

	type addMemberArgs struct {
		Project     string `json:"project" jsonschema:"project path \"namespace/slug\""`
		PrincipalID string `json:"principal_id" jsonschema:"UUID of the principal to add"`
		Role        string `json:"role" jsonschema:"OWNER, ADMIN, CONTRIBUTOR, or READER"`
	}
	type addMemberResult struct {
		Added bool `json:"added"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "project.add_member",
		Description: "Add a principal to a project you're already a member of, with the given role.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args addMemberArgs) (*sdkmcp.CallToolResult, addMemberResult, error) {
		targetPrincipalID, err := uuid.Parse(args.PrincipalID)
		if err != nil {
			return nil, addMemberResult{}, errors.New("principal_id must be a UUID")
		}
		role := projects.Role(args.Role)
		switch role {
		case projects.RoleOwner, projects.RoleAdmin, projects.RoleContributor, projects.RoleReader:
		default:
			return nil, addMemberResult{}, errors.New("role must be one of OWNER, ADMIN, CONTRIBUTOR, READER")
		}

		var project projects.Project
		err = db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			var err error
			project, err = deps.Projects.Resolve(ctx, tx, args.Project)
			return err
		})
		if err != nil {
			return nil, addMemberResult{}, err
		}

		allowed, err := deps.Authz.Can(ctx, principalID, project.ID, auth.CapMemberManage)
		if err != nil {
			return nil, addMemberResult{}, err
		}
		if !allowed {
			return nil, addMemberResult{}, errors.New("you do not have permission to manage members on this project")
		}

		err = db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			_, err := deps.Projects.AddMember(ctx, tx, project.ID, targetPrincipalID, role)
			return err
		})
		if err != nil {
			return nil, addMemberResult{}, err
		}
		return nil, addMemberResult{Added: true}, nil
	})
}
