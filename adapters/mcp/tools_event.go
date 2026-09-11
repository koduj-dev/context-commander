package mcp

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
)

type eventResult struct {
	Seq          int64  `json:"seq"`
	EventID      string `json:"event_id"`
	Type         string `json:"type"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id,omitempty"`
	PayloadKind  string `json:"payload_kind"`
}

func registerEventTools(server *sdkmcp.Server, deps Deps, principalID, _ uuid.UUID) {
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
		Limit   int    `json:"limit,omitempty" jsonschema:"max events to return (default 100, max 500)"`
		Cursor  string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call, to continue after the last event returned"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "event.list",
		Description: "List project events (the transactional outbox), oldest first. Returns one page plus next_cursor when more remain — echo it back to follow the feed forward.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, pagedResult[eventResult], error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, pagedResult[eventResult]{}, err
		}
		page, err := deps.Events.List(ctx, principalID, projectID, pagingOptions(args.Limit, args.Cursor))
		if err != nil {
			return nil, pagedResult[eventResult]{}, err
		}
		out := make([]eventResult, len(page.Items))
		for i, e := range page.Items {
			out[i] = eventResult{Seq: e.Seq, EventID: e.EventID.String(), Type: e.Type, ResourceType: e.ResourceType, PayloadKind: string(e.PayloadKind)}
			if e.ResourceID != nil {
				out[i].ResourceID = e.ResourceID.String()
			}
		}
		return nil, paged(out, page.NextCursor), nil
	})
}
