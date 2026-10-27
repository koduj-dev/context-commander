package mcp

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/contextpack"
	"github.com/koduj-dev/context-commander/internal/db"
)

// Result types mirror internal/contextpack's Pack, restated here with
// string IDs for the same reason every other tools_*.go file does: the MCP
// SDK derives a tool's output schema by reflecting over the Go type, and
// uuid.UUID reflects as a [16]byte array despite marshaling as a string —
// the mismatch fails the SDK's own output validation (adapters/mcp/mcp_test.go).
type contextPackResult struct {
	Project      contextProjectResult    `json:"project"`
	Role         string                  `json:"role"`
	Capabilities []string                `json:"capabilities"`
	Handoff      *contextHandoffResult   `json:"handoff,omitempty"`
	Work         *contextWorkResult      `json:"work,omitempty"`
	Artifacts    []contextArtifactResult `json:"artifacts"`
	Skills       []contextSkillResult    `json:"skills"`
	Events       contextEventsResult     `json:"events"`
	Budget       contextBudgetResult     `json:"budget"`
	AssembledAt  string                  `json:"assembled_at"`
}

// contextWorkResult is contextpack.WorkSection with string IDs.
type contextWorkResult struct {
	Phases     []contextpack.WorkPhase    `json:"phases,omitempty"`
	OpenPlans  int                        `json:"open_plans"`
	InProgress []contextWorkItemResult    `json:"in_progress,omitempty"`
	Blocked    []contextWorkItemResult    `json:"blocked,omitempty"`
	Present    []contextpack.WorkPresence `json:"present,omitempty"`
	Truncated  bool                       `json:"truncated,omitempty"`
}

type contextWorkItemResult struct {
	PlanID string `json:"plan_id"`
	ItemID string `json:"item_id"`
	Plan   string `json:"plan"`
	Text   string `json:"text"`
	Note   string `json:"note,omitempty"`
	Who    string `json:"who,omitempty"`
}

func toContextWork(w *contextpack.WorkSection) *contextWorkResult {
	if w == nil {
		return nil
	}
	items := func(in []contextpack.WorkItem) []contextWorkItemResult {
		var out []contextWorkItemResult
		for _, it := range in {
			out = append(out, contextWorkItemResult{
				PlanID: it.PlanID.String(), ItemID: it.ItemID.String(), Plan: it.Plan, Text: it.Text, Note: it.Note, Who: it.Who,
			})
		}
		return out
	}
	return &contextWorkResult{
		Phases: w.Phases, OpenPlans: w.OpenPlans, InProgress: items(w.InProgress), Blocked: items(w.Blocked),
		Present: w.Present, Truncated: w.Truncated,
	}
}

type contextProjectResult struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Name string `json:"name"`
}

type contextHandoffResult struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`

	ClaimedAt          string `json:"claimed_at,omitempty"`
	ClaimedByPrincipal string `json:"claimed_by_principal,omitempty"`
	ClaimedByClient    string `json:"claimed_by_client,omitempty"`
	// See handoffResult.ClaimedByYou — the same question, asked at the
	// moment an agent is deciding whether this handoff is its assignment.
	ClaimedByYou bool `json:"claimed_by_you"`

	Summary             string   `json:"summary"`
	CompletedWork       string   `json:"completed_work"`
	NextActions         string   `json:"next_actions"`
	ReferencedArtifacts []string `json:"referenced_artifacts,omitempty"`
}

type contextArtifactResult struct {
	LogicalPath string `json:"logical_path"`
	Version     int    `json:"version"`
	ContentHash string `json:"content_hash"`
	SizeBytes   int64  `json:"size_bytes"`
	UpdatedAt   string `json:"updated_at"`
	Content     string `json:"content"`
}

type contextSkillResult struct {
	SkillID        string `json:"skill_id"`
	SkillVersionID string `json:"skill_version_id"`
	Name           string `json:"name"`
	Version        int    `json:"version"`
	Scope          string `json:"scope"`
	TrustStatus    string `json:"trust_status"`
	Content        string `json:"content"`
}

type contextEventsResult struct {
	Recent     []contextEventResult `json:"recent"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type contextEventResult struct {
	Type         string `json:"type"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id,omitempty"`
}

type contextBudgetResult struct {
	LimitBytes int                       `json:"limit_bytes"`
	UsedBytes  int                       `json:"used_bytes"`
	Truncated  []contextTruncationResult `json:"truncated"`
}

type contextTruncationResult struct {
	Kind      string `json:"kind"`
	Ref       string `json:"ref"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Reason    string `json:"reason"`
}

// rfc3339 is the one timestamp format every tool result in this package
// uses — an agent parsing two different shapes from the same server is a
// bug waiting to be blamed on the model.
const rfc3339 = time.RFC3339

func toContextPackResult(p contextpack.Pack, thisClientID uuid.UUID) contextPackResult {
	out := contextPackResult{
		Project: contextProjectResult{ID: p.Project.ID.String(), Path: p.Project.Path, Name: p.Project.Name},
		Role:    p.Role, Capabilities: p.Capabilities,
		Artifacts:   make([]contextArtifactResult, 0, len(p.Artifacts)),
		Skills:      make([]contextSkillResult, 0, len(p.Skills)),
		AssembledAt: p.AssembledAt.Format(rfc3339),
		Work:        toContextWork(p.Work),
	}
	if p.Handoff != nil {
		h := contextHandoffResult{
			ID: p.Handoff.ID.String(), Status: p.Handoff.Status,
			CreatedAt: p.Handoff.CreatedAt.Format(rfc3339),
			Summary:   p.Handoff.Summary, CompletedWork: p.Handoff.CompletedWork, NextActions: p.Handoff.NextActions,
			ReferencedArtifacts: formatUUIDs(p.Handoff.ReferencedArtifacts),
		}
		if p.Handoff.ClaimedAt != nil {
			h.ClaimedAt = p.Handoff.ClaimedAt.Format(rfc3339)
		}
		if p.Handoff.ClaimedByPrincipal != nil {
			h.ClaimedByPrincipal = p.Handoff.ClaimedByPrincipal.String()
		}
		if p.Handoff.ClaimedByClient != nil {
			h.ClaimedByClient = p.Handoff.ClaimedByClient.String()
			h.ClaimedByYou = *p.Handoff.ClaimedByClient == thisClientID
		}
		out.Handoff = &h
	}
	for _, a := range p.Artifacts {
		out.Artifacts = append(out.Artifacts, contextArtifactResult{
			LogicalPath: a.LogicalPath, Version: a.Version, ContentHash: a.ContentHash,
			SizeBytes: a.SizeBytes, UpdatedAt: a.UpdatedAt.Format(rfc3339), Content: a.Content,
		})
	}
	for _, sk := range p.Skills {
		out.Skills = append(out.Skills, contextSkillResult{
			SkillID: sk.SkillID.String(), SkillVersionID: sk.SkillVersionID.String(),
			Name: sk.Name, Version: sk.Version, Scope: sk.Scope,
			TrustStatus: sk.TrustStatus, Content: sk.Content,
		})
	}
	out.Events.NextCursor = p.Events.NextCursor
	out.Events.Recent = make([]contextEventResult, 0, len(p.Events.Recent))
	for _, e := range p.Events.Recent {
		r := contextEventResult{Type: e.Type, ResourceType: e.ResourceType}
		if e.ResourceID != nil {
			r.ResourceID = e.ResourceID.String()
		}
		out.Events.Recent = append(out.Events.Recent, r)
	}
	out.Budget = contextBudgetResult{
		LimitBytes: p.Budget.LimitBytes, UsedBytes: p.Budget.UsedBytes,
		Truncated: make([]contextTruncationResult, 0, len(p.Budget.Truncated)),
	}
	for _, t := range p.Budget.Truncated {
		out.Budget.Truncated = append(out.Budget.Truncated, contextTruncationResult{
			Kind: t.Kind, Ref: t.Ref, SizeBytes: t.SizeBytes, Reason: t.Reason,
		})
	}
	return out
}

// registerContextTools exposes the one operation an agent should call
// first. Everything it returns is reachable through the per-domain tools
// as well — this exists so that reaching a useful starting state does not
// require knowing the domain model, which is the difference between a
// server an agent can use and a server an agent needs a tutorial for.
func registerContextTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	// clientID is not attribution here — nothing in a read is attributable —
	// but the pack reports whether the current handoff is held by THIS
	// client, which only this session knows.

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

	type bootstrapArgs struct {
		Project       string   `json:"project" jsonschema:"project path \"namespace/slug\", e.g. \"filip/es-migration\""`
		Budget        int      `json:"budget,omitempty" jsonschema:"max bytes of artifact and skill content to include (default 262144, max 8388608)"`
		ArtifactPaths []string `json:"artifact_paths,omitempty" jsonschema:"read exactly these artifact logical paths instead of the default selection"`
		SkipSkills    bool     `json:"skip_skills,omitempty" jsonschema:"omit skill content — for a client that already loaded it earlier in the session"`
		SkipArtifacts bool     `json:"skip_artifacts,omitempty" jsonschema:"omit artifact content"`
		EventLimit    int      `json:"event_limit,omitempty" jsonschema:"how many recent events to include (default 20)"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "context.bootstrap",
		Description: "Assemble everything needed to start working on a project, in one call: the project and " +
			"your role in it, the handoff to continue from, the content of the project's effective skills, the " +
			"artifacts it says to read, and the tail of its event stream with a cursor to follow along from. " +
			"Call this FIRST when joining a project — the per-domain tools (handoff.*, artifact.*, skill.*, " +
			"event.list) are for everything after that first step. Content is capped by an explicit byte " +
			"budget; whatever did not fit is listed in budget.truncated with the tool that fetches it, so an " +
			"empty truncated list means you have the whole picture. If the handoff comes back already claimed, " +
			"handoff.claimed_by_you says whether the client holding it is you.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args bootstrapArgs) (*sdkmcp.CallToolResult, contextPackResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, contextPackResult{}, err
		}
		pack, err := deps.ContextPack.Bootstrap(ctx, principalID, projectID, contextpack.Options{
			Budget:        args.Budget,
			ArtifactPaths: args.ArtifactPaths,
			SkipSkills:    args.SkipSkills,
			SkipArtifacts: args.SkipArtifacts,
			EventLimit:    args.EventLimit,
		})
		if err != nil {
			return nil, contextPackResult{}, err
		}
		return nil, toContextPackResult(pack, clientID), nil
	})
}
