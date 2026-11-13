package mcp_test

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type memoryToolResult struct {
	ID            string   `json:"id"`
	Project       string   `json:"project"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status"`
	Version       int      `json:"version"`
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Branch        string   `json:"branch"`
	TicketKey     string   `json:"ticket_key"`
	TicketURL     string   `json:"ticket_url"`
	Tags          []string `json:"tags"`
	AssignedToYou bool     `json:"assigned_to_you"`
	AssigneeName  string   `json:"assignee_name"`
	AuthorName    string   `json:"author_name"`
	SupersededBy  string   `json:"superseded_by"`
	Match         string   `json:"match"`
	Snippet       string   `json:"snippet"`
}

type memorySearchToolResult struct {
	Items     []memoryToolResult `json:"items"`
	Truncated bool               `json:"truncated"`
}

// One agent records what it found, with its branch and ticket; a colleague's
// agent arriving later sees that memories exist in context.bootstrap, finds
// this one by its words or its ticket, reads it, and cannot change it as a
// reader.
func TestAMemoryWrittenByOneAgentIsFoundByTheNext(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	tokenA := f.newUserWithToken("memory-a@example.test")
	a, err := f.connect(ctx, tokenA)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	const project = "memory-a/work"
	callTool[projectResult](t, ctx, a, "project.create", map[string]any{"project": project, "name": "Work"})

	created := callTool[memoryToolResult](t, ctx, a, "memory.create", map[string]any{
		"project": project, "kind": "gotcha", "title": "Webhook retries arrive out of order",
		"body": "Sort by `event_seq`, never by arrival time.", "branch": "feature/webhooks",
		"ticket_key": "PROJ-77", "ticket_url": "https://tracker.example.test/PROJ-77", "tags": []string{"Webhooks"}, "assignee": "me",
	})
	if created.Kind != "gotcha" || created.Branch != "feature/webhooks" || len(created.Tags) != 1 || created.Tags[0] != "webhooks" ||
		!created.AssignedToYou || created.Version != 1 {
		t.Fatalf("created %+v", created)
	}

	tokenB := f.newUserWithToken("memory-b@example.test")
	callTool[addMemberToolResult](t, ctx, a, "project.add_member", map[string]any{
		"project": project, "principal_id": f.principalIDOf(ctx, tokenB), "role": "READER",
	})
	b, err := f.connect(ctx, tokenB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()

	type packMemories struct {
		Memories struct {
			Recent []struct {
				ID        string `json:"id"`
				Title     string `json:"title"`
				TicketKey string `json:"ticket_key"`
				Body      string `json:"body"`
			} `json:"recent"`
		} `json:"memories"`
	}
	pack := callTool[packMemories](t, ctx, b, "context.bootstrap", map[string]any{"project": project})
	if len(pack.Memories.Recent) != 1 || pack.Memories.Recent[0].ID != created.ID || pack.Memories.Recent[0].TicketKey != "PROJ-77" {
		t.Fatalf("bootstrap memories = %+v", pack.Memories)
	}

	found := callTool[memorySearchToolResult](t, ctx, b, "memory.search", map[string]any{"project": project, "query": "event_seq"})
	if len(found.Items) != 1 || found.Items[0].Match != "body" || found.Items[0].Body != "" || found.Items[0].Snippet == "" || found.Truncated {
		t.Fatalf("search = %+v", found)
	}
	// Without a project: every project the caller reads, each hit named.
	everywhere := callTool[memorySearchToolResult](t, ctx, b, "memory.search", map[string]any{"query": "webhook", "ticket_key": "PROJ-77"})
	if len(everywhere.Items) != 1 || everywhere.Items[0].Project != project {
		t.Fatalf("search everywhere = %+v", everywhere)
	}
	listed := callToolList[memoryToolResult](t, ctx, b, "memory.list", map[string]any{"project": project, "branch": "feature/webhooks"})
	if len(listed) != 1 || listed[0].AuthorName == "" || listed[0].AssigneeName == "" {
		t.Fatalf("list by branch = %+v", listed)
	}
	got := callTool[memoryToolResult](t, ctx, b, "memory.get", map[string]any{"project": project, "memory_id": created.ID})
	if got.Body != "Sort by `event_seq`, never by arrival time." || got.AssignedToYou {
		t.Errorf("get as B = %+v", got)
	}

	// A reader reads and writes nothing.
	res, err := b.CallTool(ctx, &sdkmcp.CallToolParams{Name: "memory.create", Arguments: map[string]any{"project": project, "title": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("a reader created a memory")
	}

	// The writer corrects it against the version it read, then retires it
	// in favour of a newer one.
	newTitle := "Webhook retries arrive out of order (all providers)"
	edited := callTool[memoryToolResult](t, ctx, a, "memory.update", map[string]any{
		"project": project, "memory_id": created.ID, "expected_version": 1, "title": newTitle,
	})
	if edited.Version != 2 || edited.Title != newTitle || edited.TicketKey != "PROJ-77" {
		t.Errorf("edited %+v", edited)
	}
	stale, err := a.CallTool(ctx, &sdkmcp.CallToolParams{Name: "memory.update", Arguments: map[string]any{
		"project": project, "memory_id": created.ID, "expected_version": 1, "body": "lost",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !stale.IsError {
		t.Error("a stale edit was accepted")
	}
	replacement := callTool[memoryToolResult](t, ctx, a, "memory.create", map[string]any{"project": project, "kind": "decision", "title": "Queue webhooks by event_seq"})
	retired := callTool[memoryToolResult](t, ctx, a, "memory.supersede", map[string]any{
		"project": project, "memory_id": created.ID, "superseded_by": replacement.ID,
	})
	if retired.Status != "superseded" || retired.SupersededBy != replacement.ID {
		t.Errorf("retired %+v", retired)
	}
	if active := callToolList[memoryToolResult](t, ctx, a, "memory.list", map[string]any{"project": project}); len(active) != 1 || active[0].ID != replacement.ID {
		t.Errorf("active after superseding = %+v", active)
	}
}
