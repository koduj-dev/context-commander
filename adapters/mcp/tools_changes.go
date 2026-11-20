package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type changeResult struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail"`
	Where  string `json:"where,omitempty"`
	PlanID string `json:"plan_id,omitempty"`
	By     string `json:"by"`
	ByName string `json:"by_name"`
	At     string `json:"at"`
}

type changesResult struct {
	Now   string         `json:"now"`
	Items []changeResult `json:"items"`
}

func registerChangeTools(server *sdkmcp.Server, deps Deps, principalID, _ uuid.UUID) {
	resolve := projectResolver(deps, principalID)

	type args struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Since   string `json:"since,omitempty" jsonschema:"RFC 3339 time: the now of your previous call; omit on the first call to get a starting point"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "project.changes",
		Description: "What everyone else did on the project since a moment: memories recorded, plan steps blocked, " +
			"finished or unblocked, handoffs opened, artifacts written, and ideas added, proposed or decided, " +
			"newest first, at most 20, reaching back at most a day. " +
			"Call it when you resume work or between larger steps, passing the now of your previous call as since, " +
			"and read whatever touches your task (memory.get, plan.get, handoff.get, artifact.read with the title as path, idea.get). Your own changes are left out.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a args) (*sdkmcp.CallToolResult, changesResult, error) {
		projectID, err := resolve(ctx, a.Project)
		if err != nil {
			return nil, changesResult{}, err
		}
		now := time.Now().UTC()
		since := now
		if a.Since != "" {
			if since, err = time.Parse(time.RFC3339Nano, a.Since); err != nil {
				return nil, changesResult{}, fmt.Errorf("since must be an RFC 3339 time: %w", err)
			}
		}
		list, err := deps.Changes.Since(ctx, principalID, projectID, since, now)
		if err != nil {
			return nil, changesResult{}, err
		}
		out := changesResult{Now: now.Format(time.RFC3339Nano), Items: make([]changeResult, len(list))}
		for i, c := range list {
			out.Items[i] = changeResult{Kind: c.Kind, ID: c.ID.String(), Title: c.Title, Detail: c.Detail, Where: c.Where,
				By: c.By.String(), ByName: c.ByName, At: c.At.Format(time.RFC3339Nano)}
			if c.PlanID != uuid.Nil {
				out.Items[i].PlanID = c.PlanID.String()
			}
		}
		return nil, out, nil
	})
}
