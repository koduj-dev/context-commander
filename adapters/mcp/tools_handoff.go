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

	ClaimedAt          string `json:"claimed_at,omitempty"`
	ClaimedByPrincipal string `json:"claimed_by_principal,omitempty"`
	ClaimedByClient    string `json:"claimed_by_client,omitempty"`

	// ClaimedByYou is the field an agent should actually branch on. Status
	// says the handoff is claimed; only this says by whom. It matters
	// because a claim whose response was lost is indistinguishable, from
	// the caller's side, from a claim someone else won: the retry gets
	// "already claimed" either way, and the recommended follow-up read
	// would otherwise show a "claimed" handoff the agent has no way to
	// recognize as its own. A principal id would not settle it either —
	// two clients can act as the same principal — so this compares the
	// client credential this MCP session authenticated with.
	ClaimedByYou bool `json:"claimed_by_you"`

	ClosedAt          string `json:"closed_at,omitempty"`
	ClosedByPrincipal string `json:"closed_by_principal,omitempty"`
	ClosedByClient    string `json:"closed_by_client,omitempty"`
}

// toHandoffResult takes the session's own client id so it can answer
// "is this mine?" rather than leaving every client to compare UUIDs itself.
func toHandoffResult(h handoffs.Handoff, thisClientID uuid.UUID) handoffResult {
	r := handoffResult{ID: h.ID.String(), Status: h.Status, CreatedAt: h.CreatedAt.Format(time.RFC3339)}
	if h.SourceSessionID != nil {
		r.SourceSessionID = h.SourceSessionID.String()
	}
	if h.ClaimedAt != nil {
		r.ClaimedAt = h.ClaimedAt.Format(time.RFC3339)
	}
	if h.ClaimedByPrincipal != nil {
		r.ClaimedByPrincipal = h.ClaimedByPrincipal.String()
	}
	if h.ClaimedByClient != nil {
		r.ClaimedByClient = h.ClaimedByClient.String()
		r.ClaimedByYou = *h.ClaimedByClient == thisClientID
	}
	if h.ClosedAt != nil {
		r.ClosedAt = h.ClosedAt.Format(time.RFC3339)
	}
	if h.ClosedByPrincipal != nil {
		r.ClosedByPrincipal = h.ClosedByPrincipal.String()
	}
	if h.ClosedByClient != nil {
		r.ClosedByClient = h.ClosedByClient.String()
	}
	return r
}

// handoffDetailResult is the full handoff — metadata plus decrypted body.
// Declared at package scope rather than inside registerHandoffTools because
// both handoff.get and handoff.current return it, and the MCP SDK derives
// each tool's output schema by reflecting over this exact type.
type handoffDetailResult struct {
	handoffResult
	Summary             string   `json:"summary"`
	CompletedWork       string   `json:"completed_work"`
	NextActions         string   `json:"next_actions"`
	ReferencedArtifacts []string `json:"referenced_artifacts,omitempty"`
	ReferencedTasks     []string `json:"referenced_tasks,omitempty"`
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
		return nil, toHandoffResult(h, clientID), nil
	})

	type getArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		ID      string `json:"id" jsonschema:"handoff ID"`
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
			handoffResult:       toHandoffResult(h, clientID),
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
			out[i] = toHandoffResult(h, clientID)
		}
		return nil, paged(out, page.NextCursor), nil
	})

	type currentArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "handoff.current",
		Description: "Get the handoff to continue from — the most recent one still open or claimed, with its " +
			"full body. Use this instead of handoff.list when picking up work: a list of IDs does not say " +
			"which one is current. Status \"open\" means nobody has taken it yet (claim it with handoff.claim); " +
			"\"claimed\" means some client has — check claimed_by_you to see whether that client is you (it will " +
			"be, if an earlier claim of yours succeeded but its response never reached you), and if it is not, " +
			"treat the handoff as context rather than as an assignment. " +
			"Returns an error when every handoff is closed — that means there is nothing to take over.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args currentArgs) (*sdkmcp.CallToolResult, handoffDetailResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, handoffDetailResult{}, err
		}
		h, err := deps.Handoffs.Current(ctx, principalID, projectID)
		if err != nil {
			return nil, handoffDetailResult{}, err
		}
		_, body, err := deps.Handoffs.Get(ctx, principalID, projectID, h.ID)
		if err != nil {
			return nil, handoffDetailResult{}, err
		}
		return nil, handoffDetailResult{
			handoffResult:       toHandoffResult(h, clientID),
			Summary:             body.Summary,
			CompletedWork:       body.CompletedWork,
			NextActions:         body.NextActions,
			ReferencedArtifacts: formatUUIDs(body.ReferencedArtifacts),
			ReferencedTasks:     formatUUIDs(body.ReferencedTasks),
		}, nil
	})

	type transitionArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		ID      string `json:"id" jsonschema:"handoff ID"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "handoff.claim",
		Description: "Take responsibility for continuing a handoff's work: open -> claimed, recorded against " +
			"this client. Do this before starting, so another agent picking up the same project can see the " +
			"work is already being continued. Fails if another client claimed it first — that is the " +
			"coordination answer, not an error in your request: re-read it with handoff.current and check " +
			"claimed_by_you — if it is true, your own earlier claim went through and you should carry on; if " +
			"false, another client holds it and the work is not yours to do.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args transitionArgs) (*sdkmcp.CallToolResult, handoffResult, error) {
		projectID, handoffID, err := resolveHandoffTarget(ctx, resolve, args.Project, args.ID)
		if err != nil {
			return nil, handoffResult{}, err
		}
		h, err := deps.Handoffs.Claim(ctx, principalID, &clientID, projectID, handoffID)
		if err != nil {
			return nil, handoffResult{}, err
		}
		return nil, toHandoffResult(h, clientID), nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "handoff.close",
		Description: "Declare a handoff's work finished: open or claimed -> closed. Closing is what stops the " +
			"next agent from picking work up again after it is done — handoff.current only ever returns " +
			"handoffs that are still open or claimed. Create a new handoff first if you are stopping " +
			"mid-task rather than finishing.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args transitionArgs) (*sdkmcp.CallToolResult, handoffResult, error) {
		projectID, handoffID, err := resolveHandoffTarget(ctx, resolve, args.Project, args.ID)
		if err != nil {
			return nil, handoffResult{}, err
		}
		h, err := deps.Handoffs.Close(ctx, principalID, &clientID, projectID, handoffID)
		if err != nil {
			return nil, handoffResult{}, err
		}
		return nil, toHandoffResult(h, clientID), nil
	})
}

// resolveHandoffTarget resolves the project path and parses the handoff ID —
// the identical four lines both transition tools would otherwise open with.
func resolveHandoffTarget(
	ctx context.Context,
	resolve func(context.Context, string) (uuid.UUID, error),
	projectPath, handoffID string,
) (uuid.UUID, uuid.UUID, error) {
	projectID, err := resolve(ctx, projectPath)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	id, err := uuid.Parse(handoffID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return projectID, id, nil
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
