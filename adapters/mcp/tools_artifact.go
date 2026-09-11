package mcp

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
)

type artifactResult struct {
	LogicalPath    string `json:"logical_path"`
	ContentType    string `json:"content_type"`
	CurrentVersion int    `json:"current_version"`
}

type versionResult struct {
	Version     int    `json:"version"`
	ContentHash string `json:"content_hash"`
	SizeBytes   int64  `json:"size_bytes"`
}

func registerArtifactTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	// resolve is shared by every artifact tool below: they all take a
	// "project" path argument rather than a bare UUID, matching
	// project.resolve and every other adapter's addressing scheme.
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

	type listArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Limit   int    `json:"limit,omitempty" jsonschema:"max artifacts to return (default 100, max 500)"`
		Cursor  string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call, to continue past the last artifact returned"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "artifact.list",
		Description: "List non-deleted artifacts in a project, most recently updated first. Returns one page plus next_cursor when more remain.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, pagedResult[artifactResult], error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, pagedResult[artifactResult]{}, err
		}
		page, err := deps.Artifacts.List(ctx, principalID, projectID, pagingOptions(args.Limit, args.Cursor))
		if err != nil {
			return nil, pagedResult[artifactResult]{}, err
		}
		out := make([]artifactResult, len(page.Items))
		for i, a := range page.Items {
			out[i] = artifactResult{LogicalPath: a.LogicalPath, ContentType: a.ContentType, CurrentVersion: a.CurrentVersion}
		}
		return nil, paged(out, page.NextCursor), nil
	})

	type pathArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Path    string `json:"path" jsonschema:"logical artifact path, e.g. notes/architecture.md"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "artifact.stat",
		Description: "Get metadata for one artifact without fetching its content.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args pathArgs) (*sdkmcp.CallToolResult, artifactResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, artifactResult{}, err
		}
		a, err := deps.Artifacts.Stat(ctx, principalID, projectID, args.Path)
		if err != nil {
			return nil, artifactResult{}, err
		}
		return nil, artifactResult{LogicalPath: a.LogicalPath, ContentType: a.ContentType, CurrentVersion: a.CurrentVersion}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "artifact.history",
		Description: "List every version of one artifact, oldest first.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args pathArgs) (*sdkmcp.CallToolResult, []versionResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, nil, err
		}
		versions, err := deps.Artifacts.History(ctx, principalID, projectID, args.Path)
		if err != nil {
			return nil, nil, err
		}
		out := make([]versionResult, len(versions))
		for i, v := range versions {
			out[i] = versionResult{Version: v.Version, ContentHash: v.ContentHash, SizeBytes: v.SizeBytes}
		}
		return nil, out, nil
	})

	type readArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Path    string `json:"path" jsonschema:"logical artifact path"`
		Version int    `json:"version,omitempty" jsonschema:"specific version to read (default: latest)"`
	}
	type readResult struct {
		Version     int    `json:"version"`
		ContentHash string `json:"content_hash"`
		Content     string `json:"content"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "artifact.read",
		Description: "Read an artifact's text content — the latest version, or a specific one.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args readArgs) (*sdkmcp.CallToolResult, readResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, readResult{}, err
		}
		var version *int
		if args.Version > 0 {
			version = &args.Version
		}
		v, content, err := deps.Artifacts.Read(ctx, principalID, projectID, args.Path, version)
		if err != nil {
			return nil, readResult{}, err
		}
		return nil, readResult{Version: v.Version, ContentHash: v.ContentHash, Content: string(content)}, nil
	})

	type writeArgs struct {
		Project         string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Path            string `json:"path" jsonschema:"logical artifact path, e.g. notes/architecture.md"`
		ContentType     string `json:"content_type,omitempty" jsonschema:"MIME content type (default: text/plain)"`
		Content         string `json:"content" jsonschema:"the text content to write"`
		ExpectedVersion int    `json:"expected_version" jsonschema:"current version you expect (0 for a brand-new artifact) — optimistic concurrency, see docs/adr/0010"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "artifact.write",
		Description: "Write a new artifact version. expected_version must match the artifact's actual " +
			"current version (0 if it doesn't exist yet) or the write is rejected as a conflict — " +
			"call artifact.stat first if you're not sure.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args writeArgs) (*sdkmcp.CallToolResult, versionResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, versionResult{}, err
		}
		contentType := args.ContentType
		if contentType == "" {
			contentType = "text/plain"
		}
		v, err := deps.Artifacts.Write(ctx, principalID, &clientID, projectID, args.Path, contentType, []byte(args.Content), args.ExpectedVersion)
		if err != nil {
			return nil, versionResult{}, err
		}
		return nil, versionResult{Version: v.Version, ContentHash: v.ContentHash, SizeBytes: v.SizeBytes}, nil
	})

	type deleteResult struct {
		Deleted bool `json:"deleted"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "artifact.delete",
		Description: "Soft-delete an artifact. Version history is kept for audit; this doesn't erase it.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args pathArgs) (*sdkmcp.CallToolResult, deleteResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, deleteResult{}, err
		}
		if err := deps.Artifacts.Delete(ctx, principalID, &clientID, projectID, args.Path); err != nil {
			return nil, deleteResult{}, err
		}
		return nil, deleteResult{Deleted: true}, nil
	})
}
