package mcp

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/handoffs"
)

type handoffResult struct {
	ID              string `json:"id"`
	SourceSessionID string `json:"source_session_id,omitempty"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
}

func toHandoffResult(h handoffs.Handoff) handoffResult {
	r := handoffResult{ID: h.ID.String(), Status: h.Status, CreatedAt: h.CreatedAt.Format(time.RFC3339)}
	if h.SourceSessionID != nil {
		r.SourceSessionID = h.SourceSessionID.String()
	}
	return r
}

func registerHandoffTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
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

	type createArgs struct {
		Project             string   `json:"project" jsonschema:"project path \"namespace/slug\""`
		SourceSessionID     string   `json:"source_session_id,omitempty" jsonschema:"optional history external_session UUID this handoff originated from"`
		Summary             string   `json:"summary" jsonschema:"short summary of the work so far"`
		CompletedWork       string   `json:"completed_work" jsonschema:"what has been completed"`
		NextActions         string   `json:"next_actions" jsonschema:"what the next agent should do"`
		ReferencedArtifacts []string `json:"referenced_artifacts,omitempty" jsonschema:"artifact IDs relevant to this handoff"`
		ReferencedTasks     []string `json:"referenced_tasks,omitempty" jsonschema:"task IDs relevant to this handoff"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "handoff.create",
		Description: "Leave structured hand-off state for another agent to continue from — summary, " +
			"completed work, and next actions, without sharing vendor-specific conversation history.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args createArgs) (*sdkmcp.CallToolResult, handoffResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, handoffResult{}, err
		}
		var sourceSessionID *uuid.UUID
		if args.SourceSessionID != "" {
			id, err := uuid.Parse(args.SourceSessionID)
			if err != nil {
				return nil, handoffResult{}, err
			}
			sourceSessionID = &id
		}
		referencedArtifacts, err := parseUUIDs(args.ReferencedArtifacts)
		if err != nil {
			return nil, handoffResult{}, err
		}
		referencedTasks, err := parseUUIDs(args.ReferencedTasks)
		if err != nil {
			return nil, handoffResult{}, err
		}
		h, err := deps.Handoffs.Create(ctx, principalID, &clientID, projectID, sourceSessionID, handoffs.Body{
			Summary:             args.Summary,
			CompletedWork:       args.CompletedWork,
			NextActions:         args.NextActions,
			ReferencedArtifacts: referencedArtifacts,
			ReferencedTasks:     referencedTasks,
		})
		if err != nil {
			return nil, handoffResult{}, err
		}
		return nil, toHandoffResult(h), nil
	})

	type getArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		ID      string `json:"id" jsonschema:"handoff ID"`
	}
	type handoffDetailResult struct {
		handoffResult
		Summary             string   `json:"summary"`
		CompletedWork       string   `json:"completed_work"`
		NextActions         string   `json:"next_actions"`
		ReferencedArtifacts []string `json:"referenced_artifacts,omitempty"`
		ReferencedTasks     []string `json:"referenced_tasks,omitempty"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "handoff.get",
		Description: "Read one handoff's full body — summary, completed work, next actions, references.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args getArgs) (*sdkmcp.CallToolResult, handoffDetailResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, handoffDetailResult{}, err
		}
		handoffID, err := uuid.Parse(args.ID)
		if err != nil {
			return nil, handoffDetailResult{}, err
		}
		h, body, err := deps.Handoffs.Get(ctx, principalID, projectID, handoffID)
		if err != nil {
			return nil, handoffDetailResult{}, err
		}
		return nil, handoffDetailResult{
			handoffResult:       toHandoffResult(h),
			Summary:             body.Summary,
			CompletedWork:       body.CompletedWork,
			NextActions:         body.NextActions,
			ReferencedArtifacts: formatUUIDs(body.ReferencedArtifacts),
			ReferencedTasks:     formatUUIDs(body.ReferencedTasks),
		}, nil
	})

	type listArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Status  string `json:"status,omitempty" jsonschema:"filter by status: open or closed"`
		Limit   int    `json:"limit,omitempty" jsonschema:"max handoffs to return (default 100, max 500)"`
		Cursor  string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call with the SAME status filter, to continue into older handoffs"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "handoff.list",
		Description: "List handoffs in a project, most recent first. Returns one page plus next_cursor when more remain.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, pagedResult[handoffResult], error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, pagedResult[handoffResult]{}, err
		}
		var statusFilter *string
		if args.Status != "" {
			statusFilter = &args.Status
		}
		page, err := deps.Handoffs.List(ctx, principalID, projectID, statusFilter, pagingOptions(args.Limit, args.Cursor))
		if err != nil {
			return nil, pagedResult[handoffResult]{}, err
		}
		out := make([]handoffResult, len(page.Items))
		for i, h := range page.Items {
			out[i] = toHandoffResult(h)
		}
		return nil, paged(out, page.NextCursor), nil
	})
}

func parseUUIDs(ss []string) ([]uuid.UUID, error) {
	if len(ss) == 0 {
		return nil, nil
	}
	out := make([]uuid.UUID, len(ss))
	for i, s := range ss {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}

func formatUUIDs(ids []uuid.UUID) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}
