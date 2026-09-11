package mcp

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
)

// This file deliberately exposes only secret.create/list/delete — no
// secret.resolve tool. Resolve returns plaintext, and handing that to any
// MCP-connected agent by default is a separate decision the plan leaves
// open; until it's made, plaintext stays reachable only through
// adapters/cli (a trusted, same-machine operator surface).

type secretReferenceResult struct {
	Reference string `json:"reference"`
	Name      string `json:"name"`
}

func registerSecretTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	_ = clientID // secrets aren't attributed to a client credential, unlike artifacts

	resolveProject := func(ctx context.Context, projectPath string) (uuid.UUID, error) {
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

	type createArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Name    string `json:"name" jsonschema:"secret name, e.g. github-token"`
		Value   string `json:"value" jsonschema:"the secret value to store"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "secret.create",
		Description: "Store (or rotate) a secret value for a project. The value is never returned by any tool in this list.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args createArgs) (*sdkmcp.CallToolResult, secretReferenceResult, error) {
		projectID, err := resolveProject(ctx, args.Project)
		if err != nil {
			return nil, secretReferenceResult{}, err
		}
		ref, err := deps.Secrets.Create(ctx, principalID, projectID, args.Name, []byte(args.Value))
		if err != nil {
			return nil, secretReferenceResult{}, err
		}
		return nil, secretReferenceResult{Reference: ref.String(), Name: ref.Name}, nil
	})

	type listArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Limit   int    `json:"limit,omitempty" jsonschema:"max references to return (default 100, max 500)"`
		Cursor  string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call, to continue past the last name returned"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "secret.list",
		Description: "List a project's secret references in name order — names only, never values. Returns one page plus next_cursor when more remain.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, pagedResult[secretReferenceResult], error) {
		projectID, err := resolveProject(ctx, args.Project)
		if err != nil {
			return nil, pagedResult[secretReferenceResult]{}, err
		}
		page, err := deps.Secrets.List(ctx, principalID, projectID, pagingOptions(args.Limit, args.Cursor))
		if err != nil {
			return nil, pagedResult[secretReferenceResult]{}, err
		}
		out := make([]secretReferenceResult, len(page.Items))
		for i, ref := range page.Items {
			out[i] = secretReferenceResult{Reference: ref.String(), Name: ref.Name}
		}
		return nil, paged(out, page.NextCursor), nil
	})

	type deleteArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Name    string `json:"name" jsonschema:"secret name to delete"`
	}
	type deleteResult struct {
		Deleted bool `json:"deleted"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "secret.delete",
		Description: "Delete a secret value from a project.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args deleteArgs) (*sdkmcp.CallToolResult, deleteResult, error) {
		projectID, err := resolveProject(ctx, args.Project)
		if err != nil {
			return nil, deleteResult{}, err
		}
		if err := deps.Secrets.Delete(ctx, principalID, projectID, args.Name); err != nil {
			return nil, deleteResult{}, err
		}
		return nil, deleteResult{Deleted: true}, nil
	})
}
