package mcp

import (
	"context"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/ideas"
	"github.com/koduj-dev/context-commander/internal/paging"
)

// The idea stack (docs/adr/0026). The tools a planning agent pulls ideas
// with and answers them through, and the one anybody's agent drops an idea
// with. Accepting and rejecting a proposal are deliberately absent: they are
// a person's decision, made in the interface (ideas.ErrNeedsPerson).

// planningLoop is said once and repeated in every tool a planning agent
// uses, because a model reads the description of the tool it is about to
// call, not of the one it called three steps ago.
const planningLoop = "The planning-agent loop: idea.claim_next → read the project's context (context.bootstrap, plan.board, " +
	"the artifacts it names) → analyse the idea → idea.propose with your proposal and a draft plan → stop. " +
	"Never implement the solution yourself: a person accepts or rejects the proposal, and the draft plan becomes " +
	"the work only when they accept it."

type ideaResult struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	Position int    `json:"position"`
	Title    string `json:"title"`
	// Body, proposal and rejection come with idea.get and claims, not in
	// lists.
	Body      string `json:"body,omitempty"`
	Proposal  string `json:"proposal,omitempty"`
	Rejection string `json:"rejection,omitempty"`
	Branch    string `json:"branch,omitempty"`
	TicketKey string `json:"ticket_key,omitempty"`
	TicketURL string `json:"ticket_url,omitempty"`

	CreatedBy      string `json:"created_by"`
	ClaimedBy      string `json:"claimed_by,omitempty"`
	ClaimedAt      string `json:"claimed_at,omitempty"`
	ClaimExpiresAt string `json:"claim_expires_at,omitempty"`
	// ClaimedByYou is what an agent should branch on: the principal id does
	// not say whether this session is the client meant.
	ClaimedByYou bool `json:"claimed_by_you"`
	// ClaimLapsed is a claim whose lease ran out; the next claim takes it.
	ClaimLapsed     bool   `json:"claim_lapsed,omitempty"`
	ResultingPlanID string `json:"resulting_plan_id,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func optTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func optID(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func toIdea(i ideas.Idea, clientID uuid.UUID, now time.Time) ideaResult {
	return ideaResult{
		ID: i.ID.String(), Status: i.Status, Priority: i.Priority, Position: i.Position,
		Title: i.Title, Body: i.Body, Proposal: i.Proposal, Rejection: i.Rejection,
		Branch: i.Branch, TicketKey: i.TicketKey, TicketURL: i.TicketURL,
		CreatedBy: i.CreatedBy.String(), ClaimedBy: optID(i.ClaimedBy),
		ClaimedAt: optTime(i.ClaimedAt), ClaimExpiresAt: optTime(i.ClaimExpiresAt),
		ClaimedByYou: i.Status == ideas.StatusClaimed && i.HeldBy(&clientID), ClaimLapsed: i.Lapsed(now),
		ResultingPlanID: optID(i.ResultingPlanID),
		CreatedAt:       i.CreatedAt.Format(time.RFC3339), UpdatedAt: i.UpdatedAt.Format(time.RFC3339),
	}
}

func registerIdeaTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	resolve := projectResolver(deps, principalID)

	type addArgs struct {
		Project   string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Title     string `json:"title" jsonschema:"the idea or task, in one line"`
		Body      string `json:"body,omitempty" jsonschema:"what you know about it: context, constraints, why it matters"`
		Priority  string `json:"priority,omitempty" jsonschema:"low, normal (default) or high"`
		Branch    string `json:"branch,omitempty" jsonschema:"the git branch it concerns, if any"`
		TicketKey string `json:"ticket_key,omitempty" jsonschema:"the issue tracker key, e.g. PROJ-123"`
		TicketURL string `json:"ticket_url,omitempty" jsonschema:"the ticket's http(s) URL"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "idea.add",
		Description: "Drop an idea or task on the project's idea stack for the planning agent to analyse and propose a " +
			"solution for. Use it for something worth doing that is not yours to do now — you noticed it while working, " +
			"or your user asked you to note it. It does not start any work by itself.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args addArgs) (*sdkmcp.CallToolResult, ideaResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, ideaResult{}, err
		}
		i, err := deps.Ideas.Create(ctx, principalID, &clientID, projectID, ideas.NewIdea{
			Title: args.Title, Body: args.Body, Priority: args.Priority,
			Branch: args.Branch, TicketKey: args.TicketKey, TicketURL: args.TicketURL,
		})
		if err != nil {
			return nil, ideaResult{}, err
		}
		return nil, toIdea(i, clientID, deps.Ideas.Now()), nil
	})

	type listArgs struct {
		Project string   `json:"project" jsonschema:"project path \"namespace/slug\""`
		Status  []string `json:"status,omitempty" jsonschema:"only these statuses: open, claimed, proposed, accepted, rejected, withdrawn; default open and claimed"`
		Limit   int      `json:"limit,omitempty" jsonschema:"page size (default 100, max 500)"`
		Cursor  string   `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
	}
	type listResult struct {
		Items      []ideaResult `json:"items"`
		NextCursor string       `json:"next_cursor,omitempty"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "idea.list",
		Description: "The project's idea stack, titles and status only. Open and claimed ideas come in queue order " +
			"(priority, then the team's manual order, then age) — the order idea.claim_next takes them in; " +
			"other statuses come most recently changed first. Use idea.get for an idea's body and proposal.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, listResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, listResult{}, err
		}
		statuses := args.Status
		if len(statuses) == 0 {
			statuses = []string{ideas.StatusOpen, ideas.StatusClaimed}
		}
		page, err := deps.Ideas.List(ctx, principalID, projectID, statuses, paging.Options{Limit: args.Limit, Cursor: args.Cursor})
		if err != nil {
			return nil, listResult{}, err
		}
		out := listResult{Items: []ideaResult{}, NextCursor: page.NextCursor}
		now := deps.Ideas.Now()
		for _, i := range page.Items {
			out.Items = append(out.Items, toIdea(i, clientID, now))
		}
		return nil, out, nil
	})

	type ideaArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		IdeaID  string `json:"idea_id" jsonschema:"idea ID"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "idea.get",
		Description: "One idea in full: its body, the proposal made for it, and — if a person rejected an earlier " +
			"proposal and sent the idea back — the reason, which the next proposal has to answer.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args ideaArgs) (*sdkmcp.CallToolResult, ideaResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, ideaResult{}, err
		}
		id, err := uuid.Parse(args.IdeaID)
		if err != nil {
			return nil, ideaResult{}, err
		}
		i, err := deps.Ideas.Get(ctx, principalID, projectID, id)
		if err != nil {
			return nil, ideaResult{}, err
		}
		return nil, toIdea(i, clientID, deps.Ideas.Now()), nil
	})

	type claimNextArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
	}
	type claimNextResult struct {
		// Claimed false means the stack has nothing waiting: stop.
		Claimed bool `json:"claimed"`
		// Resumed means you already held this idea — an earlier claim_next
		// whose reply you did not get. Carry on with it.
		Resumed bool        `json:"resumed,omitempty"`
		Idea    *ideaResult `json:"idea,omitempty"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "idea.claim_next",
		Description: "If you are the planning agent: take the next idea off the stack — the highest-priority open one, " +
			"or one whose previous claim lapsed. No other agent gets it while your claim holds. The claim is a lease of " +
			"two hours (claim_expires_at); call idea.claim on the same idea to renew it if your analysis runs long. " +
			"claimed=false means nothing is waiting: stop, do not poll. If you already hold a claim, you get that idea " +
			"back (resumed=true) instead of a second one. " + planningLoop,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args claimNextArgs) (*sdkmcp.CallToolResult, claimNextResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, claimNextResult{}, err
		}
		i, found, resumed, err := deps.Ideas.ClaimNext(ctx, principalID, &clientID, projectID)
		if err != nil {
			return nil, claimNextResult{}, err
		}
		if !found {
			return nil, claimNextResult{}, nil
		}
		r := toIdea(i, clientID, deps.Ideas.Now())
		return nil, claimNextResult{Claimed: true, Resumed: resumed, Idea: &r}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "idea.claim",
		Description: "Claim one particular idea for analysis, or renew the lease on one you already hold (another two " +
			"hours from now). Refused as already claimed when another client holds a live claim: then it is theirs, " +
			"leave it. Prefer idea.claim_next, which takes the idea the team put first. " + planningLoop,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args ideaArgs) (*sdkmcp.CallToolResult, ideaResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, ideaResult{}, err
		}
		id, err := uuid.Parse(args.IdeaID)
		if err != nil {
			return nil, ideaResult{}, err
		}
		i, err := deps.Ideas.Claim(ctx, principalID, &clientID, projectID, id)
		if err != nil {
			return nil, ideaResult{}, err
		}
		return nil, toIdea(i, clientID, deps.Ideas.Now()), nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "idea.release",
		Description: "Give an idea you claimed back to the stack, in its old place, without proposing anything — when " +
			"you cannot analyse it (missing access, missing context) or must stop. Say why in a handoff or to your user; " +
			"the next planning agent will take it.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args ideaArgs) (*sdkmcp.CallToolResult, ideaResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, ideaResult{}, err
		}
		id, err := uuid.Parse(args.IdeaID)
		if err != nil {
			return nil, ideaResult{}, err
		}
		i, err := deps.Ideas.Release(ctx, principalID, &clientID, projectID, id)
		if err != nil {
			return nil, ideaResult{}, err
		}
		return nil, toIdea(i, clientID, deps.Ideas.Now()), nil
	})

	type planItem struct {
		Text string `json:"text" jsonschema:"one step, as a person would read it"`
		Kind string `json:"kind,omitempty" jsonschema:"task (default) or check — check is something to verify rather than produce"`
	}
	type draftPlan struct {
		Title   string     `json:"title,omitempty" jsonschema:"what the plan achieves; defaults to the idea's title"`
		Body    string     `json:"body,omitempty" jsonschema:"approach, risks, constraints"`
		PhaseID string     `json:"phase_id,omitempty" jsonschema:"phase the plan belongs to, from plan.board"`
		Items   []planItem `json:"items,omitempty" jsonschema:"the steps, in order"`
	}
	type proposeArgs struct {
		Project  string     `json:"project" jsonschema:"project path \"namespace/slug\""`
		IdeaID   string     `json:"idea_id" jsonschema:"the idea you claimed"`
		Proposal string     `json:"proposal" jsonschema:"your analysis and proposed solution, in markdown: what you found, the options, what you recommend and why, risks"`
		Plan     *draftPlan `json:"plan,omitempty" jsonschema:"the plan that would carry the proposal out; created as a draft and activated only if a person accepts"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "idea.propose",
		Description: "Answer the idea you claimed with your proposed solution and, ideally, a draft plan of the steps. " +
			"This ends your part: the idea waits for a person to accept or reject it, and you stop — do not start the " +
			"work, do not claim the next idea unless your instructions say to. If the idea came back with a rejection " +
			"reason (idea.get), your proposal must answer it. Refused as not the claimant when your lease lapsed and " +
			"another agent took the idea. " + planningLoop,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args proposeArgs) (*sdkmcp.CallToolResult, ideaResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, ideaResult{}, err
		}
		id, err := uuid.Parse(args.IdeaID)
		if err != nil {
			return nil, ideaResult{}, err
		}
		var plan *ideas.DraftPlan
		if args.Plan != nil {
			phaseID, err := parseOptionalUUID(args.Plan.PhaseID)
			if err != nil {
				return nil, ideaResult{}, err
			}
			plan = &ideas.DraftPlan{PhaseID: phaseID, Title: args.Plan.Title, Body: args.Plan.Body}
			for _, it := range args.Plan.Items {
				plan.Items = append(plan.Items, ideas.PlanItem{Text: it.Text, Kind: it.Kind})
			}
		}
		i, err := deps.Ideas.Propose(ctx, principalID, &clientID, projectID, id, args.Proposal, plan)
		if err != nil {
			return nil, ideaResult{}, err
		}
		return nil, toIdea(i, clientID, deps.Ideas.Now()), nil
	})
}
