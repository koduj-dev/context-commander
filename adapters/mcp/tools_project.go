package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

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

// myProjectResult names the project the way a caller will use it — one
// "namespace/slug" string, since that is what every other tool takes as its
// `project` argument. Handing back the two halves separately would make the
// obvious next call a string concatenation the caller has to get right.
type myProjectResult struct {
	Project    string `json:"project"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	Role       string `json:"role"`
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

	// project.list closes the gap that mattered most on this surface: an
	// agent holding a token had no way to find out which projects it could
	// act in. Every other project tool takes a "namespace/slug" the agent
	// had to be told out of band — which is exactly the kind of thing a
	// shared context layer exists to stop happening.
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "project.list",
		Description: "List every project you are a member of, with your role in each. Start here when you do not already know a project's \"namespace/slug\".",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, []myProjectResult, error) {
		var out []myProjectResult
		err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			memberships, err := deps.Projects.ListForPrincipal(ctx, tx, principalID)
			if err != nil {
				return err
			}
			out = make([]myProjectResult, len(memberships))
			for i, m := range memberships {
				out[i] = myProjectResult{
					Project:    m.NamespaceSlug + "/" + m.Project.Slug,
					Name:       m.Project.Name,
					Visibility: string(m.Project.Visibility),
					Role:       string(m.Role),
				}
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		return nil, out, nil
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
		Description: "Add a principal to a project you're already a member of, with the given role. Requires ADMIN or OWNER; granting the OWNER role requires OWNER.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args addMemberArgs) (*sdkmcp.CallToolResult, addMemberResult, error) {
		targetPrincipalID, err := uuid.Parse(args.PrincipalID)
		if err != nil {
			return nil, addMemberResult{}, errors.New("principal_id must be a UUID")
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

		// Role validation, the member.manage check and the OWNER-grants-
		// OWNER rule all live in internal/members.Service (docs/adr/0014).
		if err := deps.Members.Add(ctx, principalID, project.ID, targetPrincipalID, projects.Role(args.Role)); err != nil {
			return nil, addMemberResult{}, err
		}
		return nil, addMemberResult{Added: true}, nil
	})

	type listMembersArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
	}
	type memberResult struct {
		PrincipalID string `json:"principal_id"`
		Role        string `json:"role"`
		CreatedAt   string `json:"created_at"`
		Email       string `json:"email,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
	}
	type listMembersResult struct {
		Members []memberResult `json:"members"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "project.list_members",
		Description: "List every member of a project you're already a member of, with their role.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listMembersArgs) (*sdkmcp.CallToolResult, listMembersResult, error) {
		var project projects.Project
		err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			var err error
			project, err = deps.Projects.Resolve(ctx, tx, args.Project)
			return err
		})
		if err != nil {
			return nil, listMembersResult{}, err
		}

		members, err := deps.Members.List(ctx, principalID, project.ID)
		if err != nil {
			return nil, listMembersResult{}, err
		}

		principalIDs := make([]uuid.UUID, len(members))
		for i, m := range members {
			principalIDs[i] = m.PrincipalID
		}
		var users map[uuid.UUID]identity.User
		if len(principalIDs) > 0 {
			err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
				var err error
				users, err = identity.NewStore().GetUsersForPrincipals(ctx, tx, principalIDs)
				return err
			})
			if err != nil {
				return nil, listMembersResult{}, err
			}
		}

		out := make([]memberResult, len(members))
		for i, m := range members {
			out[i] = memberResult{PrincipalID: m.PrincipalID.String(), Role: string(m.Role), CreatedAt: m.CreatedAt.Format(time.RFC3339)}
			if u, ok := users[m.PrincipalID]; ok {
				out[i].Email = u.Email
				out[i].DisplayName = u.DisplayName
			}
		}
		return nil, listMembersResult{Members: out}, nil
	})
}
