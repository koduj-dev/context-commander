package mcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
)

type ideaToolResult struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	Title           string `json:"title"`
	Rejection       string `json:"rejection"`
	ClaimedByYou    bool   `json:"claimed_by_you"`
	ClaimExpiresAt  string `json:"claim_expires_at"`
	ResultingPlanID string `json:"resulting_plan_id"`
}

type claimNextToolResult struct {
	Claimed bool            `json:"claimed"`
	Resumed bool            `json:"resumed"`
	Idea    *ideaToolResult `json:"idea"`
}

// The planning-agent loop as an agent sees it: one client drops an idea,
// the planning agent claims it, a second planning agent finds nothing to
// take, the first proposes with a draft plan, and the bootstrap pack counts
// what waits.
func TestThePlanningAgentLoopOverMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	token := f.newUserWithToken("ideas-a@example.test")
	dev, err := f.connect(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dev.Close() }()
	const project = "ideas-a/work"
	callTool[projectResult](t, ctx, dev, "project.create", map[string]any{"project": project, "name": "Work"})

	added := callTool[ideaToolResult](t, ctx, dev, "idea.add", map[string]any{
		"project": project, "title": "Retry failed webhooks", "body": "They are dropped today", "priority": "high",
	})
	if added.Status != "open" {
		t.Fatalf("added %+v", added)
	}

	pack := callTool[struct {
		Ideas *struct {
			Open      int `json:"open"`
			Claimable int `json:"claimable"`
		} `json:"ideas"`
	}](t, ctx, dev, "context.bootstrap", map[string]any{"project": project})
	if pack.Ideas == nil || pack.Ideas.Open != 1 || pack.Ideas.Claimable != 1 {
		t.Errorf("bootstrap ideas = %+v, want one open and claimable", pack.Ideas)
	}

	// The planning agent is a second client of the same person: another
	// token, another session.
	planner, err := f.connect(ctx, f.newTokenFor("ideas-a@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = planner.Close() }()

	got := callTool[claimNextToolResult](t, ctx, planner, "idea.claim_next", map[string]any{"project": project})
	if !got.Claimed || got.Idea == nil || got.Idea.ID != added.ID || !got.Idea.ClaimedByYou || got.Idea.ClaimExpiresAt == "" {
		t.Fatalf("claim_next = %+v", got)
	}
	// Seen from the other client, the idea is claimed, not by it, and
	// there is nothing left for it to take.
	if none := callTool[claimNextToolResult](t, ctx, dev, "idea.claim_next", map[string]any{"project": project}); none.Claimed {
		t.Errorf("a second client took a claimed idea: %+v", none)
	}
	res, err := dev.CallTool(ctx, &sdkmcp.CallToolParams{Name: "idea.claim", Arguments: map[string]any{"project": project, "idea_id": added.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(textOf(res), "already claimed") {
		t.Errorf("claiming another client's idea: error=%v %q", res.IsError, textOf(res))
	}

	proposed := callTool[ideaToolResult](t, ctx, planner, "idea.propose", map[string]any{
		"project": project, "idea_id": added.ID, "proposal": "Add an outbox with exponential backoff.",
		"plan": map[string]any{"items": []map[string]any{{"text": "Add the outbox table"}, {"text": "Retries back off", "kind": "check"}}},
	})
	if proposed.Status != "proposed" || proposed.ResultingPlanID == "" {
		t.Fatalf("proposed %+v", proposed)
	}
	plan := callTool[planToolResult](t, ctx, dev, "plan.get", map[string]any{"project": project, "plan_id": proposed.ResultingPlanID})
	if len(plan.Items) != 2 {
		t.Errorf("draft plan %+v", plan)
	}
	listed := callTool[struct {
		Items []ideaToolResult `json:"items"`
	}](t, ctx, dev, "idea.list", map[string]any{"project": project, "status": []string{"proposed"}})
	if len(listed.Items) != 1 || listed.Items[0].ID != added.ID {
		t.Errorf("proposed list %+v", listed.Items)
	}
}

// newTokenFor issues a second credential to an existing user: a second
// client of the same person, as a planning agent beside their coding agent is.
func (f *fixture) newTokenFor(email string) string {
	f.t.Helper()
	ctx := context.Background()
	store := identity.NewStore()
	var token string
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		u, err := store.GetUserByEmail(ctx, tx, email)
		if err != nil {
			return err
		}
		p, err := store.PrincipalForUser(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		token, _, err = store.IssueCredential(ctx, tx, p.ID, "planner", nil)
		return err
	})
	if err != nil {
		f.t.Fatalf("newTokenFor: %v", err)
	}
	return token
}

func textOf(r *sdkmcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if t, ok := c.(*sdkmcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}
