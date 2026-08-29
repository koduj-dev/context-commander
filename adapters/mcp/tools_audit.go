package mcp

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/db"
)

type auditRecordResult struct {
	ID           int64  `json:"id"`
	Timestamp    string `json:"timestamp"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id,omitempty"`
	Result       string `json:"result"`
}

func registerAuditTools(server *sdkmcp.Server, deps Deps, principalID, _ uuid.UUID) {
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
		Project      string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Action       string `json:"action,omitempty" jsonschema:"exact-match action filter"`
		ResourceType string `json:"resource_type,omitempty" jsonschema:"exact-match resource type filter"`
		Limit        int    `json:"limit,omitempty" jsonschema:"max records to return (default: 100, max: 500)"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "audit.list",
		Description: "Query the project's audit trail. Requires ADMIN or OWNER on the project.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, []auditRecordResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, nil, err
		}
		list, err := deps.Audit.Query(ctx, principalID, projectID, audit.QueryOptions{
			Action: args.Action, ResourceType: args.ResourceType, Limit: args.Limit,
		})
		if err != nil {
			return nil, nil, err
		}
		out := make([]auditRecordResult, len(list))
		for i, rec := range list {
			out[i] = auditRecordResult{ID: rec.ID, Timestamp: rec.Timestamp.Format(time.RFC3339), Action: rec.Action, ResourceType: rec.ResourceType, Result: string(rec.Result)}
			if rec.ResourceID != nil {
				out[i].ResourceID = rec.ResourceID.String()
			}
		}
		return nil, out, nil
	})
}
