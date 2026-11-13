package mcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	ccmcp "github.com/koduj-dev/context-commander/adapters/mcp"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/messages"
)

type inboxToolResult struct {
	Items []struct {
		ID          string `json:"id"`
		Project     string `json:"project"`
		From        string `json:"from"`
		FromName    string `json:"from_name"`
		To          string `json:"to"`
		Body        string `json:"body"`
		Redelivered bool   `json:"redelivered"`
	} `json:"items"`
	NextCursor string `json:"next_cursor"`
}

func noticeIn(result *sdkmcp.CallToolResult) string {
	for _, c := range result.Content {
		if text, ok := c.(*sdkmcp.TextContent); ok && strings.HasPrefix(text.Text, "[Context Commander]") {
			return text.Text
		}
	}
	return ""
}

// A person sends a message to an agent's session from the interface; the
// agent's next tool call — any tool — carries a notice without disturbing
// that tool's own result; inbox.next hands the message over, and inbox.ack
// closes it.
func TestAMessageReachesTheAgentAtItsNextToolCall(t *testing.T) {
	var svc *messages.Service
	f := newFixtureWith(t, func(d *ccmcp.Deps) { svc = d.Messages })
	ctx := context.Background()

	token := f.newUserWithToken("inbox@example.test")
	agent, err := f.connect(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agent.Close() }()
	const project = "inbox/work"
	created := callTool[projectResult](t, ctx, agent, "project.create", map[string]any{"project": project, "name": "Work"})

	// Nothing waiting: no notice.
	result, err := agent.CallTool(ctx, &sdkmcp.CallToolParams{Name: "plan.board", Arguments: map[string]any{"project": project}})
	if err != nil || result.IsError {
		t.Fatalf("plan.board: %v %v", err, result)
	}
	if n := noticeIn(result); n != "" {
		t.Fatalf("a notice with an empty inbox: %q", n)
	}

	// The same person, signed in to the interface, sends to this session.
	principalID := uuid.MustParse(f.principalIDOf(ctx, token))
	agentClient, _, err := identity.ParseToken(token)
	if err != nil {
		t.Fatal(err)
	}
	browser := uuid.New()
	if err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO client_credentials (id, principal_id, client_type, token_hash) VALUES ($1, $2, 'session', 'unused')`, browser, principalID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Send(ctx, principalID, &browser, uuid.MustParse(created.ID), messages.NewMessage{
		Body: "Use the staging database, not production.", RecipientClient: &agentClient,
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	result, err = agent.CallTool(ctx, &sdkmcp.CallToolParams{Name: "plan.board", Arguments: map[string]any{"project": project}})
	if err != nil || result.IsError {
		t.Fatalf("plan.board: %v %v", err, result)
	}
	notice := noticeIn(result)
	if !strings.Contains(notice, "1 unread message") || !strings.Contains(notice, "inbox.next") {
		t.Fatalf("notice %q", notice)
	}
	// The tool's own result is untouched: structured content, and the first
	// text block still the tool's JSON, with the notice after it.
	if result.StructuredContent == nil || len(result.Content) < 2 {
		t.Fatalf("the notice displaced the result: %+v", result.Content)
	}
	if first, ok := result.Content[0].(*sdkmcp.TextContent); !ok || !strings.HasPrefix(first.Text, "{") {
		t.Errorf("the first block is no longer the tool's JSON: %+v", result.Content[0])
	}

	inbox := callTool[inboxToolResult](t, ctx, agent, "inbox.next", map[string]any{})
	if len(inbox.Items) != 1 || inbox.Items[0].Body != "Use the staging database, not production." ||
		inbox.Items[0].To != "you" || inbox.Items[0].Project != project || inbox.Items[0].From != principalID.String() {
		t.Fatalf("inbox.next %+v", inbox)
	}

	// Delivered: the notice stops, though the message waits for its ack.
	result, err = agent.CallTool(ctx, &sdkmcp.CallToolParams{Name: "plan.board", Arguments: map[string]any{"project": project}})
	if err != nil || result.IsError {
		t.Fatalf("plan.board: %v %v", err, result)
	}
	if n := noticeIn(result); n != "" {
		t.Errorf("a notice after delivery: %q", n)
	}
	again := callTool[inboxToolResult](t, ctx, agent, "inbox.next", map[string]any{})
	if len(again.Items) != 1 || !again.Items[0].Redelivered {
		t.Fatalf("an unacknowledged message was not offered again: %+v", again)
	}
	ack := callTool[struct {
		Acknowledged int `json:"acknowledged"`
	}](t, ctx, agent, "inbox.ack", map[string]any{"message_ids": []string{again.Items[0].ID}})
	if ack.Acknowledged != 1 {
		t.Errorf("acknowledged %d", ack.Acknowledged)
	}
	if empty := callTool[inboxToolResult](t, ctx, agent, "inbox.next", map[string]any{}); len(empty.Items) != 0 {
		t.Errorf("after ack: %+v", empty)
	}
}
