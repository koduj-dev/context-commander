package contextpack

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/skills"
)

var (
	// ErrForbidden is returned when the caller can't read the project at
	// all. Individual sections degrade to a truncation note instead (see
	// Bootstrap) — this is only for the one check without which there is
	// no pack to build.
	ErrForbidden = errors.New("contextpack: forbidden")

	ErrNotFound = errors.New("contextpack: project not found")
)

// Service assembles Packs. It owns no storage of its own: every dependency
// is a domain service that performs its own authorization, so the pack
// cannot contain anything the caller could not have fetched one call at a
// time.
type Service struct {
	pool      *db.Pool
	projects  *projects.Store
	authz     *auth.AuthzService
	artifacts *artifacts.Service
	skills    *skills.Service
	handoffs  *handoffs.Service
	// plans is optional: a pack without it has no Work section, which is
	// what every caller that predates docs/adr/0022 gets.
	plans  *plans.Service
	events *events.Service
}

func NewService(
	pool *db.Pool, projectStore *projects.Store, authz *auth.AuthzService,
	artifactsSvc *artifacts.Service, skillsSvc *skills.Service,
	handoffsSvc *handoffs.Service, eventsSvc *events.Service,
) *Service {
	return &Service{
		pool: pool, projects: projectStore, authz: authz,
		artifacts: artifactsSvc, skills: skillsSvc, handoffs: handoffsSvc, events: eventsSvc,
	}
}

// WithPlans adds the Work section to every pack this service assembles.
func (s *Service) WithPlans(p *plans.Service) *Service {
	s.plans = p
	return s
}

// budget tracks what is left to spend and records every omission, so
// "nothing more exists" and "more exists, you didn't get it" are never the
// same answer.
type budget struct {
	limit     int
	used      int
	truncated []TruncationNote
}

// fits reports whether n more bytes can be included, charging them if so.
// An item that doesn't fit is never partially included — see
// TruncationNote.
func (b *budget) fits(n int) bool {
	if b.used+n > b.limit {
		return false
	}
	b.used += n
	return true
}

func (b *budget) note(kind, ref string, size int64, reason string) {
	b.truncated = append(b.truncated, TruncationNote{Kind: kind, Ref: ref, SizeBytes: size, Reason: reason})
}

// Bootstrap assembles the starting context for principalID on projectID.
//
// Sections are gathered in priority order and charged against one shared
// budget: the handoff first (it is the assignment — a pack without it has
// failed at its job), then effective skills (instructions that shape how
// the work is done), then artifacts (the work itself). Events are
// metadata-only and bounded by count instead, since the cursor matters more
// than the rows.
//
// A section the caller isn't authorized for, or that fails to load, becomes
// a TruncationNote rather than an error: a READER with no CapSecretRead-like
// gap in one domain should still get everything else, and an agent told
// exactly which part is missing can proceed sensibly. The single exception
// is CapProjectRead, checked up front — without it there is nothing to
// assemble and returning a mostly-empty pack would be a worse answer than
// a refusal.
func (s *Service) Bootstrap(ctx context.Context, principalID, projectID uuid.UUID, opts Options) (Pack, error) {
	allowed, err := s.authz.Can(ctx, principalID, projectID, auth.CapProjectRead)
	if err != nil {
		return Pack{}, fmt.Errorf("contextpack: authorization check: %w", err)
	}
	if !allowed {
		return Pack{}, ErrForbidden
	}

	pack := Pack{AssembledAt: time.Now().UTC()}
	b := &budget{limit: clampBudget(opts.Budget)}

	if err := s.fillProjectAndRole(ctx, principalID, projectID, &pack); err != nil {
		return Pack{}, err
	}

	// Events FIRST, before any content is read — the order is load-bearing,
	// not stylistic.
	//
	// Each section below reads in its own transaction, so the pack is not a
	// single consistent snapshot of the project. The continuation cursor
	// therefore has to mark a point that everything in the pack is at least
	// as new as. Taken here, a write that lands while the rest of the pack
	// is being assembled is *after* the cursor: the pack may carry the old
	// version, and event.list from the cursor will still report the change.
	// Taken last (the original order, found in review), the same write
	// lands *before* the cursor while its content is missing from the pack —
	// invisible from both sides, which is the one outcome an agent cannot
	// recover from.
	//
	// The cost of this order is that the cursor may replay events already
	// reflected in the pack. Re-reading something twice is a wasted call;
	// never learning it changed is a wrong answer.
	s.fillEvents(ctx, principalID, projectID, opts, &pack, b)

	referenced := s.fillHandoff(ctx, principalID, projectID, &pack, b)
	s.fillWork(ctx, principalID, projectID, &pack, b)
	if !opts.SkipSkills {
		s.fillSkills(ctx, principalID, projectID, &pack, b)
	}
	if !opts.SkipArtifacts {
		s.fillArtifacts(ctx, principalID, projectID, opts, referenced, &pack, b)
	}

	// A seam for the interleaving test in boundary_internal_test.go: nil in
	// production, so this costs one nil check.
	//
	// It sits here, after the content and where the event read used to be,
	// because this is the window the bug lived in: a write landing at this
	// exact point was missing from the content AND behind a cursor taken
	// after it. Nothing in an assembly publishes an event of its own, so
	// there is no way to reproduce that from outside the package.
	if hookAfterContentRead != nil {
		hookAfterContentRead()
	}

	// Non-nil empty slices: a client iterating a section should not have to
	// special-case null, and "this project has no skills" is a real answer
	// the pack must be able to state.
	if pack.Artifacts == nil {
		pack.Artifacts = []ArtifactSection{}
	}
	if pack.Skills == nil {
		pack.Skills = []SkillSection{}
	}
	if pack.Events.Recent == nil {
		pack.Events.Recent = []EventRef{}
	}
	if b.truncated == nil {
		b.truncated = []TruncationNote{}
	}
	pack.Budget = BudgetReport{LimitBytes: b.limit, UsedBytes: b.used, Truncated: b.truncated}
	return pack, nil
}

func clampBudget(requested int) int {
	if requested <= 0 {
		return DefaultBudget
	}
	if requested > MaxBudget {
		return MaxBudget
	}
	return requested
}

// fillProjectAndRole resolves the project's human-readable path and the
// caller's role. Both are load-bearing identity, not decoration: an agent
// that cannot name the project it is in cannot describe its own state back
// to a human, and one that doesn't know its role guesses at what it may do.
func (s *Service) fillProjectAndRole(ctx context.Context, principalID, projectID uuid.UUID, pack *Pack) error {
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var namespaceSlug, projectSlug, name string
		err := tx.QueryRow(ctx,
			`SELECT n.slug, p.slug, p.name
			 FROM projects p JOIN namespaces n ON n.id = p.namespace_id
			 WHERE p.id = $1`,
			projectID,
		).Scan(&namespaceSlug, &projectSlug, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("contextpack: resolve project: %w", err)
		}
		pack.Project = ProjectRef{ID: projectID, Path: namespaceSlug + "/" + projectSlug, Name: name}

		m, err := s.projects.GetMembership(ctx, tx, projectID, principalID)
		if err != nil {
			return fmt.Errorf("contextpack: resolve membership: %w", err)
		}
		pack.Role = string(m.Role)
		for _, c := range auth.CapabilitiesFor(m.Role) {
			pack.Capabilities = append(pack.Capabilities, string(c))
		}
		return nil
	})
	return err
}

// fillHandoff adds the current handoff and returns the artifact IDs it
// references, so fillArtifacts can include them without a second decision
// about what matters: the outgoing agent already said these do.
func (s *Service) fillHandoff(ctx context.Context, principalID, projectID uuid.UUID, pack *Pack, b *budget) []uuid.UUID {
	h, err := s.handoffs.Current(ctx, principalID, projectID)
	if errors.Is(err, handoffs.ErrNotFound) {
		return nil // No open handoff is a normal state, not an omission worth noting.
	}
	if err != nil {
		b.note("handoff", "current", 0, reasonFor(err, "could not read the current handoff"))
		return nil
	}

	_, body, err := s.handoffs.Get(ctx, principalID, projectID, h.ID)
	if err != nil {
		b.note("handoff", h.ID.String(), 0, reasonFor(err, "could not decrypt the handoff body"))
		return nil
	}

	// The handoff body is charged to the budget but never omitted for it:
	// it is the reason the pack exists, and it is small by construction
	// (three text fields and a few UUIDs). A caller whose budget cannot
	// hold it gets it anyway, and the overrun is visible in UsedBytes.
	size := len(body.Summary) + len(body.CompletedWork) + len(body.NextActions)
	if !b.fits(size) {
		b.used += size
		b.note("handoff", h.ID.String(), int64(size), "included despite exceeding the budget: the handoff is the pack's purpose")
	}

	pack.Handoff = &HandoffSection{
		ID: h.ID, Status: h.Status, CreatedAt: h.CreatedAt, ClaimedAt: h.ClaimedAt,
		ClaimedByPrincipal: h.ClaimedByPrincipal, ClaimedByClient: h.ClaimedByClient,
		Summary: body.Summary, CompletedWork: body.CompletedWork, NextActions: body.NextActions,
		ReferencedArtifacts: body.ReferencedArtifacts,
	}
	return body.ReferencedArtifacts
}

// fillWork summarizes the project's plans. Like the handoff, it is small by
// construction (capped lists of one-line entries) and charged to the budget
// without being omitted for it: knowing who is already on a step is what
// keeps two agents from doing it twice.
func (s *Service) fillWork(ctx context.Context, principalID, projectID uuid.UUID, pack *Pack, b *budget) {
	if s.plans == nil {
		return
	}
	board, err := s.plans.Board(ctx, principalID, projectID)
	if err != nil {
		if !errors.Is(err, plans.ErrForbidden) {
			b.note("work", "board", 0, reasonFor(err, "could not read the project's plans"))
		}
		return
	}
	if len(board.Plans) == 0 && len(board.Presence) == 0 && len(board.Phases) == 0 {
		return
	}

	w := &WorkSection{}
	size := 0
	for _, ph := range board.Phases {
		w.Phases = append(w.Phases, WorkPhase{Title: ph.Title, Status: ph.Status, Done: ph.Counts.Done, Total: ph.Counts.Total()})
		size += len(ph.Title)
	}
	for _, p := range board.Plans {
		if p.Status == plans.PlanActive || p.Status == plans.PlanDraft {
			w.OpenPlans++
		}
	}
	item := func(a plans.ActiveItem) WorkItem {
		it := WorkItem{PlanID: a.PlanID, ItemID: a.Item.ID, Plan: a.PlanTitle, Text: a.Item.Text, Note: a.Item.Note}
		if a.Item.Assignee != nil {
			it.Who = board.People[*a.Item.Assignee].Name
		}
		size += len(it.Plan) + len(it.Text) + len(it.Note)
		return it
	}
	for i, a := range board.InProgress {
		if i == MaxWorkEntries {
			w.Truncated = true
			break
		}
		w.InProgress = append(w.InProgress, item(a))
	}
	for i, a := range board.Blocked {
		if i == MaxWorkEntries {
			w.Truncated = true
			break
		}
		w.Blocked = append(w.Blocked, item(a))
	}
	now := time.Now()
	for i, p := range board.Presence {
		if i == MaxWorkEntries {
			w.Truncated = true
			break
		}
		where := p.PlanTitle
		if p.ItemText != "" {
			where = p.PlanTitle + " → " + p.ItemText
		}
		w.Present = append(w.Present, WorkPresence{Who: board.People[p.PrincipalID].Name, Where: where, Activity: p.Activity, Idle: p.Idle(now)})
		size += len(where) + len(p.Activity)
	}

	if !b.fits(size) {
		b.used += size
		b.note("work", "board", int64(size), "included despite exceeding the budget: who is on what is needed to avoid duplicate work")
	}
	pack.Work = w
}

// fillSkills turns each effective binding into its actual content — the
// step that was impossible before internal/skills.Service.GetVersion
// existed, and the reason ListEffective alone never let an agent act on a
// skill it had "installed".
func (s *Service) fillSkills(ctx context.Context, principalID, projectID uuid.UUID, pack *Pack, b *budget) {
	bindings, err := s.skills.ListEffective(ctx, principalID, projectID)
	if err != nil {
		b.note("skill", "effective", 0, reasonFor(err, "could not list effective skills"))
		return
	}

	for _, binding := range bindings {
		v, err := s.skills.GetVersion(ctx, principalID, binding.SkillVersionID)
		if err != nil {
			b.note("skill", binding.SkillVersionID.String(), 0, reasonFor(err, "could not read the skill's content"))
			continue
		}
		if !b.fits(len(v.Content)) {
			b.note("skill", v.Skill.Name, int64(len(v.Content)),
				"omitted: over the remaining budget — fetch it with skill.get")
			continue
		}
		pack.Skills = append(pack.Skills, SkillSection{
			BindingID: binding.ID, SkillID: binding.SkillID, SkillVersionID: binding.SkillVersionID,
			Name: v.Skill.Name, Version: v.Version.Version, Scope: string(binding.Scope),
			TrustStatus: v.Version.TrustStatus, Content: string(v.Content),
		})
	}
}

// fillArtifacts selects what to read, then reads it. Selection is, in
// order: the caller's explicit ArtifactPaths if given; otherwise the
// artifacts the current handoff referenced, followed by whatever lives
// under ConventionalArtifactPrefix.
func (s *Service) fillArtifacts(
	ctx context.Context, principalID, projectID uuid.UUID,
	opts Options, referenced []uuid.UUID, pack *Pack, b *budget,
) {
	paths, overflow, err := s.selectArtifactPaths(ctx, principalID, projectID, opts, referenced)
	if err != nil {
		b.note("artifact", "selection", 0, reasonFor(err, "could not list artifacts"))
		return
	}
	if overflow {
		b.note("artifact", ConventionalArtifactPrefix, 0,
			"omitted: more than "+strconv.Itoa(MaxConventionalArtifacts)+" artifacts under this prefix — list them with artifact.list and pass artifact_paths explicitly")
	}

	for _, path := range paths {
		version, content, err := s.artifacts.Read(ctx, principalID, projectID, path, nil)
		if err != nil {
			b.note("artifact", path, 0, reasonFor(err, "could not read the artifact"))
			continue
		}
		// Charged after the read: an artifact's size is not known until
		// its current version is resolved, and the current version is what
		// the read returns. A manifest with declared budgets (NEXT.md
		// Phase 2) is what will let this decide before spending the read.
		if !b.fits(len(content)) {
			b.note("artifact", path, version.SizeBytes,
				"omitted: over the remaining budget — fetch it with artifact.read")
			continue
		}
		pack.Artifacts = append(pack.Artifacts, ArtifactSection{
			LogicalPath: path, Version: version.Version, ContentHash: version.ContentHash,
			SizeBytes: version.SizeBytes, UpdatedAt: version.CreatedAt, Content: string(content),
		})
	}
}

// selectArtifactPaths resolves the selection to concrete logical paths,
// de-duplicated and in priority order. Referenced artifacts come first:
// they were named by the agent that did the work, which is a stronger
// signal than a naming convention.
func (s *Service) selectArtifactPaths(
	ctx context.Context, principalID, projectID uuid.UUID, opts Options, referenced []uuid.UUID,
) (selected []string, overflow bool, err error) {
	if len(opts.ArtifactPaths) > 0 {
		return dedupe(opts.ArtifactPaths), false, nil
	}

	var paths []string
	if len(referenced) > 0 {
		resolved, err := s.resolveArtifactPaths(ctx, principalID, projectID, referenced)
		if err != nil {
			return nil, false, err
		}
		paths = append(paths, resolved...)
	}

	// The convention half. The prefix is filtered in the query
	// (ListByPathPrefix), not applied to a page of a general listing: the
	// general listing orders by updated_at across every artifact in the
	// project, so filtering its first page would return only the must-read
	// files that happen to be among the most recently updated overall. A
	// project with one old context/instructions.md and a hundred newer
	// unrelated writes would have silently produced a pack with no
	// instructions in it and nothing in budget.truncated to say so — found
	// in review, and the reason this no longer goes through List at all.
	//
	// One page, bounded by MaxConventionalArtifacts: a project with more
	// must-read files than that has genuinely outgrown "everything under
	// this prefix is required reading" and wants the Phase 2 manifest. That
	// limit now counts only files under the prefix, which is what makes the
	// statement true.
	page, err := s.artifacts.ListByPathPrefix(ctx, principalID, projectID, ConventionalArtifactPrefix,
		paging.Options{Limit: MaxConventionalArtifacts})
	if err != nil {
		return nil, false, err
	}
	for _, a := range page.Items {
		paths = append(paths, a.LogicalPath)
	}
	if page.NextCursor != "" {
		// Reported rather than dropped: "there are more must-read files
		// than a pack carries" is exactly the kind of thing an agent must
		// not have to infer from a suspiciously round number of artifacts.
		overflow = true
	}
	return dedupe(paths), overflow, nil
}

// resolveArtifactPaths maps artifact IDs to logical paths. The handoff
// stores IDs (stable across renames), while every artifact read is by path
// — so something has to bridge the two, and a handoff naming an artifact
// that was since deleted must not take the whole pack down with it.
func (s *Service) resolveArtifactPaths(ctx context.Context, principalID, projectID uuid.UUID, ids []uuid.UUID) ([]string, error) {
	var paths []string
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT logical_path FROM artifacts
			 WHERE project_id = $1 AND id = ANY($2) AND deleted_at IS NULL`,
			projectID, ids,
		)
		if err != nil {
			return fmt.Errorf("contextpack: resolve referenced artifacts: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				return err
			}
			paths = append(paths, p)
		}
		return rows.Err()
	})
	return paths, err
}

// hookAfterContentRead is called once every content section has been read.
// Always nil outside this package's own tests — see the call site in
// Bootstrap for why the seam exists and why it is at that point.
var hookAfterContentRead func()

// fillEvents reports the tail of the event stream and the cursor to
// continue from. Called before any content is read; see Bootstrap.
//
// The stream is oldest-first with a seq cursor (internal/events), so "the
// most recent N" is not a page of it — it is the last page. Rather than
// walk every page to find the end, this reads the newest rows directly and
// reverses them into chronological order, then encodes the newest seq as
// the forward cursor. What the caller gets back is exactly what event.list
// would accept, so following along uses the ordinary paging contract with
// no special case.
func (s *Service) fillEvents(ctx context.Context, principalID, projectID uuid.UUID, opts Options, pack *Pack, b *budget) {
	allowed, err := s.authz.Can(ctx, principalID, projectID, auth.CapEventRead)
	if err != nil || !allowed {
		b.note("events", "recent", 0, "omitted: this role cannot read events")
		return
	}

	limit := opts.EventLimit
	if limit <= 0 {
		limit = DefaultEventLimit
	}
	if limit > paging.MaxLimit {
		limit = paging.MaxLimit
	}

	type row struct {
		seq          int64
		eventType    string
		resourceType string
		resourceID   *uuid.UUID
	}
	var newestFirst []row
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT seq, type, resource_type, resource_id FROM domain_events
			 WHERE project_id = $1 ORDER BY seq DESC LIMIT $2`,
			projectID, limit,
		)
		if err != nil {
			return fmt.Errorf("contextpack: list recent events: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.seq, &r.eventType, &r.resourceType, &r.resourceID); err != nil {
				return err
			}
			newestFirst = append(newestFirst, r)
		}
		return rows.Err()
	})
	if err != nil {
		b.note("events", "recent", 0, reasonFor(err, "could not read recent events"))
		return
	}
	if len(newestFirst) == 0 {
		return
	}

	pack.Events.NextCursor = paging.EncodeSeq(newestFirst[0].seq)
	pack.Events.Recent = make([]EventRef, 0, len(newestFirst))
	for i := len(newestFirst) - 1; i >= 0; i-- {
		r := newestFirst[i]
		pack.Events.Recent = append(pack.Events.Recent, EventRef{
			Type: r.eventType, ResourceType: r.resourceType, ResourceID: r.resourceID,
		})
	}
}

// reasonFor turns a domain error into a note a client can act on, without
// leaking internals. A forbidden section is the one case worth naming
// precisely: "you may not read this" and "this failed to load" call for
// completely different responses from an agent.
func reasonFor(err error, fallback string) string {
	switch {
	case errors.Is(err, handoffs.ErrForbidden), errors.Is(err, skills.ErrForbidden),
		errors.Is(err, artifacts.ErrForbidden), errors.Is(err, events.ErrForbidden):
		return "omitted: this role is not permitted to read it"
	case errors.Is(err, artifacts.ErrNotFound), errors.Is(err, artifacts.ErrDeleted),
		errors.Is(err, handoffs.ErrNotFound), errors.Is(err, skills.ErrNotFound):
		return "omitted: no longer exists"
	default:
		return "omitted: " + fallback
	}
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
