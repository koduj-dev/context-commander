package mcp

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/plans"
)

// Plans, phases and presence (docs/adr/0022). The tools an agent uses to put
// the plan it already makes where its team can see it, tick it off as it
// goes, and say where it is.

type effortResult struct {
	Seconds          int64 `json:"seconds_tracked,omitempty"`
	TokensIn         int64 `json:"tokens_in_reported,omitempty"`
	TokensOut        int64 `json:"tokens_out_reported,omitempty"`
	CacheReadTokens  int64 `json:"cache_read_tokens_reported,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens_reported,omitempty"`
	// ByModel splits the tokens by the model the client named; "" is
	// "not stated".
	ByModel []modelTokensResult `json:"by_model,omitempty"`
	// CostEstimate is what the tokens would have cost at the operator's
	// prices, when the server has a price table. Never what was spent.
	CostEstimate *estimateResult `json:"cost_estimate,omitempty"`
}

type modelTokensResult struct {
	Model            string `json:"model"`
	TokensIn         int64  `json:"tokens_in,omitempty"`
	TokensOut        int64  `json:"tokens_out,omitempty"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
	// EstimatedCost is absent when the model has no price.
	EstimatedCost *float64 `json:"estimated_cost,omitempty"`
}

type estimateResult struct {
	Currency string  `json:"currency"`
	AsOf     string  `json:"prices_as_of,omitempty"`
	Amount   float64 `json:"amount"`
	// Unpriced are the models whose tokens the amount leaves out.
	Unpriced []string `json:"unpriced_models,omitempty"`
}

func toEffort(e plans.Effort, prices *plans.Prices) effortResult {
	r := effortResult{
		Seconds: e.Seconds, TokensIn: e.TokensIn, TokensOut: e.TokensOut,
		CacheReadTokens: e.CacheReadTokens, CacheWriteTokens: e.CacheWriteTokens,
	}
	for _, m := range e.Models {
		mr := modelTokensResult{
			Model: m.Model, TokensIn: m.TokensIn, TokensOut: m.TokensOut,
			CacheReadTokens: m.CacheReadTokens, CacheWriteTokens: m.CacheWriteTokens,
		}
		if cost, ok := prices.Cost(m); ok {
			mr.EstimatedCost = &cost
		}
		r.ByModel = append(r.ByModel, mr)
	}
	if est := prices.Estimate(e); est != nil {
		r.CostEstimate = &estimateResult{Currency: est.Currency, AsOf: est.AsOf, Amount: est.Amount, Unpriced: est.Unpriced}
	}
	return r
}

type itemResult struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Text     string `json:"text"`
	Note     string `json:"note,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	// AssignedToYou is what an agent should branch on: the principal id
	// alone does not say whether this session is the one meant.
	AssignedToYou bool         `json:"assigned_to_you,omitempty"`
	CompletedAt   string       `json:"completed_at,omitempty"`
	Effort        effortResult `json:"effort"`
}

type planResult struct {
	ID      string       `json:"id"`
	PhaseID string       `json:"phase_id,omitempty"`
	Status  string       `json:"status"`
	Version int          `json:"version"`
	Title   string       `json:"title"`
	Body    string       `json:"body,omitempty"`
	Items   []itemResult `json:"items"`
}

func toItem(it plans.Item, principalID uuid.UUID, prices *plans.Prices) itemResult {
	r := itemResult{
		ID: it.ID.String(), Position: it.Position, Kind: it.Kind, Status: it.Status,
		Text: it.Text, Note: it.Note, Effort: toEffort(it.Effort, prices),
	}
	if it.Assignee != nil {
		r.Assignee = it.Assignee.String()
		r.AssignedToYou = *it.Assignee == principalID
	}
	if it.CompletedAt != nil {
		r.CompletedAt = it.CompletedAt.Format(time.RFC3339)
	}
	return r
}

func toPlan(p plans.Plan, principalID uuid.UUID, prices *plans.Prices) planResult {
	r := planResult{ID: p.ID.String(), Status: p.Status, Version: p.Version, Title: p.Title, Body: p.Body, Items: []itemResult{}}
	if p.PhaseID != nil {
		r.PhaseID = p.PhaseID.String()
	}
	for _, it := range p.Items {
		r.Items = append(r.Items, toItem(it, principalID, prices))
	}
	return r
}

type countsResult struct {
	Open       int `json:"open"`
	InProgress int `json:"in_progress"`
	Done       int `json:"done"`
	Blocked    int `json:"blocked"`
	Skipped    int `json:"skipped"`
}

func toCounts(c plans.Counts) countsResult {
	return countsResult{Open: c.Open, InProgress: c.InProgress, Done: c.Done, Blocked: c.Blocked, Skipped: c.Skipped}
}

type phaseResult struct {
	ID       string       `json:"id"`
	Position int          `json:"position"`
	Status   string       `json:"status"`
	Title    string       `json:"title"`
	Goal     string       `json:"goal,omitempty"`
	Counts   countsResult `json:"items"`
	Effort   effortResult `json:"effort"`
}

type planSummaryResult struct {
	ID      string       `json:"id"`
	PhaseID string       `json:"phase_id,omitempty"`
	Status  string       `json:"status"`
	Title   string       `json:"title"`
	Counts  countsResult `json:"items"`
	Updated string       `json:"updated_at"`
}

type activeItemResult struct {
	PlanID    string `json:"plan_id"`
	PlanTitle string `json:"plan_title"`
	ItemID    string `json:"item_id"`
	Text      string `json:"text"`
	Note      string `json:"note,omitempty"`
	Who       string `json:"who,omitempty"`
}

type presenceResult struct {
	Who        string `json:"who"`
	WhoKind    string `json:"who_kind"`
	ClientType string `json:"client_type"`
	You        bool   `json:"you,omitempty"`
	Project    string `json:"project,omitempty"`
	PlanID     string `json:"plan_id,omitempty"`
	PlanTitle  string `json:"plan_title,omitempty"`
	ItemID     string `json:"item_id,omitempty"`
	ItemText   string `json:"item_text,omitempty"`
	Activity   string `json:"activity,omitempty"`
	Since      string `json:"since"`
	LastSeen   string `json:"last_seen"`
	Idle       bool   `json:"idle"`
}

func toPresence(p plans.Presence, people map[uuid.UUID]plans.Person, clientID uuid.UUID, now time.Time) presenceResult {
	person := people[p.PrincipalID]
	r := presenceResult{
		Who: person.Name, WhoKind: person.Kind, ClientType: p.ClientType, You: p.ClientID == clientID,
		Project: p.ProjectPath, PlanTitle: p.PlanTitle, ItemText: p.ItemText, Activity: p.Activity,
		Since: p.Since.Format(time.RFC3339), LastSeen: p.LastBeat.Format(time.RFC3339), Idle: p.Idle(now),
	}
	if p.PlanID != nil {
		r.PlanID = p.PlanID.String()
	}
	if p.ItemID != nil {
		r.ItemID = p.ItemID.String()
	}
	return r
}

type boardResult struct {
	Phases     []phaseResult       `json:"phases"`
	Plans      []planSummaryResult `json:"plans"`
	InProgress []activeItemResult  `json:"in_progress"`
	Blocked    []activeItemResult  `json:"blocked"`
	Presence   []presenceResult    `json:"presence"`
}

func toBoard(b plans.Board, clientID uuid.UUID, prices *plans.Prices) boardResult {
	out := boardResult{Phases: []phaseResult{}, Plans: []planSummaryResult{}, InProgress: []activeItemResult{}, Blocked: []activeItemResult{}, Presence: []presenceResult{}}
	for _, ph := range b.Phases {
		out.Phases = append(out.Phases, phaseResult{
			ID: ph.ID.String(), Position: ph.Position, Status: ph.Status, Title: ph.Title, Goal: ph.Goal,
			Counts: toCounts(ph.Counts), Effort: toEffort(ph.Effort, prices),
		})
	}
	for _, p := range b.Plans {
		r := planSummaryResult{ID: p.ID.String(), Status: p.Status, Title: p.Title, Counts: toCounts(p.Counts), Updated: p.UpdatedAt.Format(time.RFC3339)}
		if p.PhaseID != nil {
			r.PhaseID = p.PhaseID.String()
		}
		out.Plans = append(out.Plans, r)
	}
	active := func(a plans.ActiveItem) activeItemResult {
		r := activeItemResult{PlanID: a.PlanID.String(), PlanTitle: a.PlanTitle, ItemID: a.Item.ID.String(), Text: a.Item.Text, Note: a.Item.Note}
		if a.Item.Assignee != nil {
			r.Who = b.People[*a.Item.Assignee].Name
		}
		return r
	}
	for _, a := range b.InProgress {
		out.InProgress = append(out.InProgress, active(a))
	}
	for _, a := range b.Blocked {
		out.Blocked = append(out.Blocked, active(a))
	}
	now := time.Now()
	for _, p := range b.Presence {
		out.Presence = append(out.Presence, toPresence(p, b.People, clientID, now))
	}
	return out
}

func parseOptionalUUID(s string) (*uuid.UUID, error) {
	if s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// projectResolver turns a "namespace/slug" into a project id, as the caller
// sees it: a project they are not a member of does not resolve.
func projectResolver(deps Deps, principalID uuid.UUID) func(context.Context, string) (uuid.UUID, error) {
	return func(ctx context.Context, projectPath string) (uuid.UUID, error) {
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
}

func registerPlanTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	resolve := projectResolver(deps, principalID)

	type projectArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "plan.board",
		Description: "The state of the project's work in one read: phases with progress, plans with item counts, " +
			"what is in progress or blocked and by whom, and where every client currently is. Read it before " +
			"planning, so you do not redo or collide with what a colleague or another agent is already doing.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectArgs) (*sdkmcp.CallToolResult, boardResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, boardResult{}, err
		}
		b, err := deps.Plans.Board(ctx, principalID, projectID)
		if err != nil {
			return nil, boardResult{}, err
		}
		return nil, toBoard(b, clientID, deps.Plans.Prices()), nil
	})

	type newItem struct {
		Text string `json:"text" jsonschema:"one step, as a person would read it"`
		Kind string `json:"kind,omitempty" jsonschema:"task (default) or check — check is something to verify rather than produce"`
	}
	toNew := func(items []newItem) []plans.NewItem {
		out := make([]plans.NewItem, len(items))
		for i, it := range items {
			out[i] = plans.NewItem{Text: it.Text, Kind: it.Kind}
		}
		return out
	}

	type createArgs struct {
		Project string    `json:"project" jsonschema:"project path \"namespace/slug\""`
		PhaseID string    `json:"phase_id,omitempty" jsonschema:"optional phase this plan belongs to"`
		Title   string    `json:"title" jsonschema:"what this plan achieves"`
		Body    string    `json:"body,omitempty" jsonschema:"approach, context, constraints — free text"`
		Items   []newItem `json:"items,omitempty" jsonschema:"the steps, in order"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "plan.create",
		Description: "Record the execution plan you made for a task, so the team can see it and follow it. " +
			"Items are the steps; mark each one as you go with plan.update_item.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args createArgs) (*sdkmcp.CallToolResult, planResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, planResult{}, err
		}
		phaseID, err := parseOptionalUUID(args.PhaseID)
		if err != nil {
			return nil, planResult{}, err
		}
		p, err := deps.Plans.CreatePlan(ctx, principalID, &clientID, projectID, phaseID, args.Title, args.Body, toNew(args.Items))
		if err != nil {
			return nil, planResult{}, err
		}
		return nil, toPlan(p, principalID, deps.Plans.Prices()), nil
	})

	type planArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		PlanID  string `json:"plan_id" jsonschema:"plan ID"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "plan.get",
		Description: "A plan with all of its items, their status, assignee, notes, and the time and tokens tracked on each.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args planArgs) (*sdkmcp.CallToolResult, planResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, planResult{}, err
		}
		id, err := uuid.Parse(args.PlanID)
		if err != nil {
			return nil, planResult{}, err
		}
		p, err := deps.Plans.GetPlan(ctx, principalID, projectID, id)
		if err != nil {
			return nil, planResult{}, err
		}
		return nil, toPlan(p, principalID, deps.Plans.Prices()), nil
	})

	type addArgs struct {
		Project string    `json:"project" jsonschema:"project path \"namespace/slug\""`
		PlanID  string    `json:"plan_id" jsonschema:"plan ID"`
		Items   []newItem `json:"items" jsonschema:"steps to append, in order"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "plan.add_items",
		Description: "Append steps to a plan. Safe to call while others are editing it: appends are merged, never lost.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args addArgs) (*sdkmcp.CallToolResult, planResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, planResult{}, err
		}
		id, err := uuid.Parse(args.PlanID)
		if err != nil {
			return nil, planResult{}, err
		}
		p, err := deps.Plans.AddItems(ctx, principalID, projectID, id, toNew(args.Items))
		if err != nil {
			return nil, planResult{}, err
		}
		return nil, toPlan(p, principalID, deps.Plans.Prices()), nil
	})

	type updateItemArgs struct {
		Project  string  `json:"project" jsonschema:"project path \"namespace/slug\""`
		ItemID   string  `json:"item_id" jsonschema:"item ID"`
		Status   string  `json:"status,omitempty" jsonschema:"open, in_progress, done, blocked or skipped"`
		Note     *string `json:"note,omitempty" jsonschema:"replace the item's note — why it is blocked, what was found"`
		Text     *string `json:"text,omitempty" jsonschema:"replace the item's text"`
		Assignee string  `json:"assignee,omitempty" jsonschema:"\"me\", \"none\", or a member's principal ID; omitted leaves it, and in_progress on an unassigned item assigns it to you"`
		Position *int    `json:"position,omitempty" jsonschema:"move the step to this place in its plan: 1 is first, past the end is last"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "plan.update_item",
		Description: "Mark a step's progress: in_progress when you start it, done when it is finished, blocked with a " +
			"note saying why. Only real status changes are recorded, so ticking an item twice costs nothing. " +
			"position moves the step within its plan.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args updateItemArgs) (*sdkmcp.CallToolResult, itemResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, itemResult{}, err
		}
		id, err := uuid.Parse(args.ItemID)
		if err != nil {
			return nil, itemResult{}, err
		}
		u := plans.ItemUpdate{Note: args.Note, Text: args.Text, Position: args.Position}
		if args.Status != "" {
			u.Status = &args.Status
		}
		switch args.Assignee {
		case "":
		case "me":
			me := principalID
			u.AssigneeSet, u.Assignee = true, &me
		case "none":
			u.AssigneeSet = true
		default:
			a, err := uuid.Parse(args.Assignee)
			if err != nil {
				return nil, itemResult{}, err
			}
			u.AssigneeSet, u.Assignee = true, &a
		}
		it, err := deps.Plans.UpdateItem(ctx, principalID, &clientID, projectID, id, u)
		if err != nil {
			return nil, itemResult{}, err
		}
		return nil, toItem(it, principalID, deps.Plans.Prices()), nil
	})

	type updatePlanArgs struct {
		Project         string  `json:"project" jsonschema:"project path \"namespace/slug\""`
		PlanID          string  `json:"plan_id" jsonschema:"plan ID"`
		Status          string  `json:"status,omitempty" jsonschema:"draft, active, done or abandoned"`
		PhaseID         *string `json:"phase_id,omitempty" jsonschema:"move the plan into this phase; empty string takes it out of any phase"`
		Title           *string `json:"title,omitempty" jsonschema:"replace the title (needs expected_version)"`
		Body            *string `json:"body,omitempty" jsonschema:"replace the body (needs expected_version)"`
		ExpectedVersion int     `json:"expected_version,omitempty" jsonschema:"the version you read, when replacing title or body"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "plan.update",
		Description: "Change a plan's status or phase, or replace its title and body (which needs the version you read).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args updatePlanArgs) (*sdkmcp.CallToolResult, planResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, planResult{}, err
		}
		id, err := uuid.Parse(args.PlanID)
		if err != nil {
			return nil, planResult{}, err
		}
		u := plans.PlanUpdate{Title: args.Title, Body: args.Body, ExpectedVersion: args.ExpectedVersion}
		if args.Status != "" {
			u.Status = &args.Status
		}
		if args.PhaseID != nil {
			u.PhaseSet = true
			if u.PhaseID, err = parseOptionalUUID(*args.PhaseID); err != nil {
				return nil, planResult{}, err
			}
		}
		p, err := deps.Plans.UpdatePlan(ctx, principalID, projectID, id, u)
		if err != nil {
			return nil, planResult{}, err
		}
		return nil, toPlan(p, principalID, deps.Plans.Prices()), nil
	})

	type searchArgs struct {
		Project string `json:"project,omitempty" jsonschema:"project path \"namespace/slug\"; leave it out to search every project you can read plans in"`
		Query   string `json:"query,omitempty" jsonschema:"words to find in plan titles, bodies, items and notes"`
		Status  string `json:"status,omitempty" jsonschema:"only items in this status"`
	}
	type searchHit struct {
		// Project is set only when the search covered every project.
		Project   string `json:"project,omitempty"`
		PlanID    string `json:"plan_id"`
		PlanTitle string `json:"plan_title"`
		ItemID    string `json:"item_id,omitempty"`
		Text      string `json:"text"`
		Status    string `json:"status"`
	}
	// An object, not a bare array, so a bounded answer can say it was
	// bounded: an agent that sees ten hits must be able to tell "that is
	// all" from "that is all the search looked at".
	type searchResult struct {
		Items     []searchHit `json:"items"`
		Truncated bool        `json:"truncated"`
	}
	toHit := func(h plans.SearchHit, project string) searchHit {
		r := searchHit{Project: project, PlanID: h.PlanID.String(), PlanTitle: h.PlanTitle, Text: h.Text, Status: h.Status}
		if h.ItemID != nil {
			r.ItemID = h.ItemID.String()
		}
		return r
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "plan.search",
		Description: "Find plans and steps by their text, or every step in a status. Live plans come first; at most 100 hits. " +
			"Without a project it searches every project you can read plans in (at most 50, your newest first), and each hit names its project. " +
			"`truncated` true means a bound cut the answer and more may exist: search within one project, or narrow the query.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args searchArgs) (*sdkmcp.CallToolResult, searchResult, error) {
		out := searchResult{Items: []searchHit{}}
		if args.Project == "" {
			res, err := deps.Plans.SearchAll(ctx, principalID, args.Query, args.Status)
			if err != nil {
				return nil, searchResult{}, err
			}
			paths := map[uuid.UUID]string{}
			for _, p := range res.Projects {
				paths[p.ID] = p.Path
			}
			for _, h := range res.Hits {
				out.Items = append(out.Items, toHit(h, paths[h.ProjectID]))
			}
			out.Truncated = res.Truncated
			return nil, out, nil
		}
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, searchResult{}, err
		}
		hits, err := deps.Plans.Search(ctx, principalID, projectID, args.Query, args.Status)
		if err != nil {
			return nil, searchResult{}, err
		}
		for _, h := range hits {
			out.Items = append(out.Items, toHit(h, ""))
		}
		// One project's search is capped at MaxSearchHits too; a full page
		// is the only sign that the cap was reached.
		out.Truncated = len(hits) >= plans.MaxSearchHits
		return nil, out, nil
	})

	type usageArgs struct {
		Project string `json:"project,omitempty" jsonschema:"project path \"namespace/slug\"; omit to sum every project you can read plans in"`
		From    string `json:"from,omitempty" jsonschema:"first UTC day, YYYY-MM-DD (default: 29 days before to)"`
		To      string `json:"to,omitempty" jsonschema:"last UTC day, YYYY-MM-DD, inclusive (default: today)"`
		Group   string `json:"group,omitempty" jsonschema:"comma-separated dimensions to group by: project, day, principal, model, branch, ticket; omit for one total"`
	}
	type usageRow struct {
		Project          string   `json:"project,omitempty"`
		Day              string   `json:"day,omitempty"`
		PrincipalID      string   `json:"principal_id,omitempty"`
		Principal        string   `json:"principal,omitempty"`
		Model            *string  `json:"model,omitempty"`
		Branch           *string  `json:"branch,omitempty"`
		TicketKey        *string  `json:"ticket_key,omitempty"`
		Seconds          int64    `json:"seconds"`
		TokensIn         int64    `json:"tokens_in"`
		TokensOut        int64    `json:"tokens_out"`
		CacheReadTokens  int64    `json:"cache_read_tokens"`
		CacheWriteTokens int64    `json:"cache_write_tokens"`
		Turns            int64    `json:"turns"`
		Cost             *float64 `json:"cost_estimate,omitempty"`
	}
	type usageResult struct {
		From      string     `json:"from"`
		To        string     `json:"to"`
		Rows      []usageRow `json:"rows"`
		Total     usageRow   `json:"total"`
		Currency  string     `json:"currency,omitempty"`
		Unpriced  []string   `json:"unpriced_models,omitempty"`
		Truncated bool       `json:"truncated"`
	}
	toUsageRow := func(r plans.UsageRow) usageRow {
		out := usageRow{
			Project: r.ProjectPath, Principal: r.PrincipalName, Model: r.Model, Branch: r.Branch, TicketKey: r.TicketKey,
			Seconds: r.Seconds, TokensIn: r.Tokens.TokensIn, TokensOut: r.Tokens.TokensOut,
			CacheReadTokens: r.Tokens.CacheReadTokens, CacheWriteTokens: r.Tokens.CacheWriteTokens,
			Turns: r.Turns, Cost: r.Cost,
		}
		if r.Day != nil {
			out.Day = r.Day.Format(time.DateOnly)
		}
		if r.PrincipalID != nil {
			out.PrincipalID = r.PrincipalID.String()
		}
		return out
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "usage.summary",
		Description: "What the work cost: time present and tokens (fresh input, output, cache reads, cache writes) as client hooks " +
			"reported them, summed per UTC day and grouped by project, day, principal, model, branch or ticket. " +
			"cost_estimate is at the operator's price table and only an estimate; it is absent without one. " +
			"Use it to answer questions like \"what did ticket PROJ-12 cost\" or \"who worked on which branch this week\".",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args usageArgs) (*sdkmcp.CallToolResult, usageResult, error) {
		q, err := plans.ParseUsageQuery(args.From, args.To, args.Group, time.Now())
		if err != nil {
			return nil, usageResult{}, err
		}
		if args.Project != "" {
			projectID, err := resolve(ctx, args.Project)
			if err != nil {
				return nil, usageResult{}, err
			}
			q.ProjectID = &projectID
		}
		s, err := deps.Plans.Usage(ctx, principalID, q)
		if err != nil {
			return nil, usageResult{}, err
		}
		out := usageResult{
			From: q.From.Format(time.DateOnly), To: q.To.Format(time.DateOnly),
			Rows: make([]usageRow, len(s.Rows)), Total: toUsageRow(s.Total),
			Currency: s.Currency, Unpriced: s.Unpriced, Truncated: s.Truncated,
		}
		for i, r := range s.Rows {
			out.Rows[i] = toUsageRow(r)
		}
		return nil, out, nil
	})

	type reportArgs struct {
		Project   string `json:"project" jsonschema:"project path \"namespace/slug\""`
		ItemID    string `json:"item_id,omitempty" jsonschema:"the step you are on now"`
		PlanID    string `json:"plan_id,omitempty" jsonschema:"the plan you are on, if not on a particular step"`
		Activity  string `json:"activity,omitempty" jsonschema:"one line: what you are doing right now"`
		Branch    string `json:"branch,omitempty" jsonschema:"the git branch you are working on, if you know it (git branch --show-current)"`
		TicketKey string `json:"ticket_key,omitempty" jsonschema:"the ticket this work is for in the team's ticket system, e.g. PROJ-123, if there is one"`
		TokensIn  int64  `json:"tokens_in,omitempty" jsonschema:"input tokens used since your previous report, if your client can tell you; never estimate"`
		TokensOut int64  `json:"tokens_out,omitempty" jsonschema:"output tokens used since your previous report, if your client can tell you; never estimate"`
		Model     string `json:"model,omitempty" jsonschema:"the model id the tokens were for, e.g. claude-opus-5-5, if you report tokens; letters, digits and . _ - : / @ +"`
	}
	type reportResult struct {
		Since    string `json:"since"`
		LastSeen string `json:"last_seen"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "plan.report",
		Description: "Say where you are, so your team and your user can see it live. Call it when you move to a step " +
			"and every few minutes while working on it: time is tracked only while reports keep coming " +
			"(a gap over 15 minutes counts as idle). Call plan.stop when you finish or stop. " +
			"Leave the token fields out unless your client tells you exact counts: you cannot see your own usage, " +
			"and the reliable way to record it is the client hook in deploy/mcp-clients/README.md " +
			"(\"Token usage\"), which a user can ask you to install.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args reportArgs) (*sdkmcp.CallToolResult, reportResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, reportResult{}, err
		}
		itemID, err := parseOptionalUUID(args.ItemID)
		if err != nil {
			return nil, reportResult{}, err
		}
		planID, err := parseOptionalUUID(args.PlanID)
		if err != nil {
			return nil, reportResult{}, err
		}
		p, err := deps.Plans.Report(ctx, principalID, &clientID, projectID, plans.Report{
			PlanID: planID, ItemID: itemID, Activity: args.Activity, Branch: args.Branch, TicketKey: args.TicketKey,
			Usage: plans.Tokens{Model: args.Model, TokensIn: args.TokensIn, TokensOut: args.TokensOut},
		})
		if err != nil {
			return nil, reportResult{}, err
		}
		return nil, reportResult{Since: p.Since.Format(time.RFC3339), LastSeen: p.LastBeat.Format(time.RFC3339)}, nil
	})

	type stopArgs struct {
		Project   string `json:"project" jsonschema:"project path \"namespace/slug\""`
		TokensIn  int64  `json:"tokens_in,omitempty" jsonschema:"input tokens used since your previous report, if known"`
		TokensOut int64  `json:"tokens_out,omitempty" jsonschema:"output tokens used since your previous report, if known"`
		Model     string `json:"model,omitempty" jsonschema:"the model id the tokens were for, if you report tokens"`
	}
	type stopResult struct {
		Stopped bool `json:"stopped"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "plan.stop",
		Description: "You finished or stopped: the time since your last report is credited and you no longer show as present.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args stopArgs) (*sdkmcp.CallToolResult, stopResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, stopResult{}, err
		}
		if err := deps.Plans.ClearPresence(ctx, principalID, &clientID, projectID, plans.Tokens{Model: args.Model, TokensIn: args.TokensIn, TokensOut: args.TokensOut}); err != nil {
			return nil, stopResult{}, err
		}
		return nil, stopResult{Stopped: true}, nil
	})

	type phaseCreateArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		Title   string `json:"title" jsonschema:"the stretch of work, e.g. \"Migration\" or \"Release 1.2\""`
		Goal    string `json:"goal,omitempty" jsonschema:"what is true when this phase is done"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "phase.create",
		Description: "Add a phase to the project. Phases group plans into stretches of work and show progress across them.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args phaseCreateArgs) (*sdkmcp.CallToolResult, phaseResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, phaseResult{}, err
		}
		ph, err := deps.Plans.CreatePhase(ctx, principalID, projectID, args.Title, args.Goal)
		if err != nil {
			return nil, phaseResult{}, err
		}
		return nil, phaseResult{ID: ph.ID.String(), Position: ph.Position, Status: ph.Status, Title: ph.Title, Goal: ph.Goal}, nil
	})

	type phaseUpdateArgs struct {
		Project  string  `json:"project" jsonschema:"project path \"namespace/slug\""`
		PhaseID  string  `json:"phase_id" jsonschema:"phase ID"`
		Status   string  `json:"status,omitempty" jsonschema:"planned, active or done"`
		Title    *string `json:"title,omitempty" jsonschema:"replace the title"`
		Goal     *string `json:"goal,omitempty" jsonschema:"replace the goal"`
		Position *int    `json:"position,omitempty" jsonschema:"move the phase to this place in the project: 1 is first, past the end is last"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "phase.update",
		Description: "Change a phase's status, title or goal, or move it to another place in the project's order.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args phaseUpdateArgs) (*sdkmcp.CallToolResult, phaseResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, phaseResult{}, err
		}
		id, err := uuid.Parse(args.PhaseID)
		if err != nil {
			return nil, phaseResult{}, err
		}
		u := plans.PhaseUpdate{Title: args.Title, Goal: args.Goal, Position: args.Position}
		if args.Status != "" {
			u.Status = &args.Status
		}
		ph, err := deps.Plans.UpdatePhase(ctx, principalID, projectID, id, u)
		if err != nil {
			return nil, phaseResult{}, err
		}
		return nil, phaseResult{ID: ph.ID.String(), Position: ph.Position, Status: ph.Status, Title: ph.Title, Goal: ph.Goal}, nil
	})
}
