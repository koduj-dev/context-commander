package mcp_test

import (
	"context"
	"testing"
)

type changesToolResult struct {
	Now   string `json:"now"`
	Items []struct {
		Kind   string `json:"kind"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	} `json:"items"`
}

// project.changes over MCP: a starting point first, then a colleague's
// memory, and never the caller's own.
func TestProjectChangesTool(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tokenA, tokenB := f.newUserWithToken("chg-a@example.test"), f.newUserWithToken("chg-b@example.test")
	a, err := f.connect(ctx, tokenA)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	b, err := f.connect(ctx, tokenB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	const project = "chg-a/work"
	callTool[projectResult](t, ctx, a, "project.create", map[string]any{"project": project, "name": "Work"})
	callTool[addMemberToolResult](t, ctx, a, "project.add_member", map[string]any{
		"project": project, "principal_id": f.principalIDOf(ctx, tokenB), "role": "CONTRIBUTOR",
	})

	start := callTool[changesToolResult](t, ctx, b, "project.changes", map[string]any{"project": project})
	if start.Now == "" || len(start.Items) != 0 {
		t.Fatalf("start %+v", start)
	}
	callTool[memoryToolResult](t, ctx, a, "memory.create", map[string]any{"project": project, "kind": "decision", "title": "Vectors stay encrypted"})
	callTool[memoryToolResult](t, ctx, b, "memory.create", map[string]any{"project": project, "title": "My own"})

	got := callTool[changesToolResult](t, ctx, b, "project.changes", map[string]any{"project": project, "since": start.Now})
	if len(got.Items) != 1 || got.Items[0].Title != "Vectors stay encrypted" || got.Items[0].Detail != "decision" {
		t.Errorf("changes %+v", got.Items)
	}
}
