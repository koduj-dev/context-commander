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
		Project  string `json:"project" jsonschema:"project path \"namespace/slug\""`
		AfterSeq int64  `json:"after_seq,omitempty" jsonschema:"only return events with seq greater than this (default: 0)"`
		Limit    int    `json:"limit,omitempty" jsonschema:"max events to return (default: 100)"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "event.list",
		Description: "List project events (the transactional outbox) after a given sequence number.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, []eventResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, nil, err
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 100
		}
		list, err := deps.Events.List(ctx, principalID, projectID, args.AfterSeq, limit)
		if err != nil {
			return nil, nil, err
		}
		out := make([]eventResult, len(list))
		for i, e := range list {
			out[i] = eventResult{Seq: e.Seq, EventID: e.EventID.String(), Type: e.Type, ResourceType: e.ResourceType, PayloadKind: string(e.PayloadKind)}
			if e.ResourceID != nil {
				out[i].ResourceID = e.ResourceID.String()
			}
		}
		return nil, out, nil
	})
}
