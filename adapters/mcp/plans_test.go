package mcp_test

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type planToolItem struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Text          string `json:"text"`
	AssignedToYou bool   `json:"assigned_to_you"`
}

type planToolResult struct {
	ID    string         `json:"id"`
	Title string         `json:"title"`
	Items []planToolItem `json:"items"`
}

type boardToolResult struct {
	InProgress []struct {
		Text string `json:"text"`
		Who  string `json:"who"`
	} `json:"in_progress"`
	Presence []struct {
		Who      string `json:"who"`
		You      bool   `json:"you"`
		ItemText string `json:"item_text"`
		Activity string `json:"activity"`
		Idle     bool   `json:"idle"`
	} `json:"presence"`
}

// One agent records its plan, starts a step and says where it is; a
// colleague's client reads the board and sees exactly that.
func TestAnAgentsPlanAndPositionAreVisibleToItsColleagues(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	tokenA := f.newUserWithToken("planner-a@example.test")
	a, err := f.connect(ctx, tokenA)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	const project = "planner-a/work"
	callTool[projectResult](t, ctx, a, "project.create", map[string]any{"project": project, "name": "Work"})

	plan := callTool[planToolResult](t, ctx, a, "plan.create", map[string]any{
		"project": project, "title": "Ship the importer",
		"items": []map[string]any{{"text": "Write the parser"}, {"text": "Import runs clean on staging", "kind": "check"}},
	})
	if len(plan.Items) != 2 || plan.Items[1].Kind != "check" {
		t.Fatalf("created %+v", plan)
	}
	started := callTool[planToolItem](t, ctx, a, "plan.update_item", map[string]any{
		"project": project, "item_id": plan.Items[0].ID, "status": "in_progress",
	})
	if !started.AssignedToYou {
		t.Error("starting an unassigned step did not assign it to the agent that started it")
	}
	callTool[map[string]any](t, ctx, a, "plan.report", map[string]any{
		"project": project, "item_id": plan.Items[0].ID, "activity": "handling quoted fields", "tokens_in": 1200,
	})

	tokenB := f.newUserWithToken("planner-b@example.test")
	callTool[addMemberToolResult](t, ctx, a, "project.add_member", map[string]any{
		"project": project, "principal_id": f.principalIDOf(ctx, tokenB), "role": "READER",
	})
	b, err := f.connect(ctx, tokenB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()

	board := callTool[boardToolResult](t, ctx, b, "plan.board", map[string]any{"project": project})
	if len(board.InProgress) != 1 || board.InProgress[0].Text != "Write the parser" {
		t.Fatalf("in progress %+v", board.InProgress)
	}
	if len(board.Presence) != 1 {
		t.Fatalf("presence %+v", board.Presence)
	}
	p := board.Presence[0]
	if p.You || p.ItemText != "Write the parser" || p.Activity != "handling quoted fields" || p.Idle {
		t.Errorf("B sees %+v", p)
	}

	// An agent arriving cold learns the same from context.bootstrap, before
	// it has planned anything.
	type packWork struct {
		Work struct {
			OpenPlans  int `json:"open_plans"`
			InProgress []struct {
				Text string `json:"text"`
			} `json:"in_progress"`
			Present []struct {
				Where    string `json:"where"`
				Activity string `json:"activity"`
			} `json:"present"`
		} `json:"work"`
	}
	pack := callTool[packWork](t, ctx, b, "context.bootstrap", map[string]any{"project": project})
	if pack.Work.OpenPlans != 1 || len(pack.Work.InProgress) != 1 || len(pack.Work.Present) != 1 ||
		pack.Work.Present[0].Where != "Ship the importer → Write the parser" {
		t.Errorf("bootstrap work section = %+v", pack.Work)
	}

	// A reader sees all of it and changes none of it.
	res, err := b.CallTool(ctx, &sdkmcp.CallToolParams{Name: "plan.update_item", Arguments: map[string]any{
		"project": project, "item_id": plan.Items[1].ID, "status": "done",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("a reader ticked a step")
	}

	callTool[map[string]any](t, ctx, a, "plan.stop", map[string]any{"project": project})
	if board = callTool[boardToolResult](t, ctx, b, "plan.board", map[string]any{"project": project}); len(board.Presence) != 0 {
		t.Errorf("a stopped agent is still present: %+v", board.Presence)
	}
}
