package contextpack

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/paging"
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
	events    *events.Service
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

	referenced := s.fillHandoff(ctx, principalID, projectID, &pack, b)
	if !opts.SkipSkills {
		s.fillSkills(ctx, principalID, projectID, &pack, b)
	}
	if !opts.SkipArtifacts {
		s.fillArtifacts(ctx, principalID, projectID, opts, referenced, &pack, b)
	}
	s.fillEvents(ctx, principalID, projectID, opts, &pack, b)

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
		Summary: body.Summary, CompletedWork: body.CompletedWork, NextActions: body.NextActions,
		ReferencedArtifacts: body.ReferencedArtifacts, ReferencedTasks: body.ReferencedTasks,
	}
	return body.ReferencedArtifacts
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
		v, err := s.skills.GetVersion(ctx, principalID, projectID, binding.SkillVersionID)
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
	paths, err := s.selectArtifactPaths(ctx, principalID, projectID, opts, referenced)
	if err != nil {
		b.note("artifact", "selection", 0, reasonFor(err, "could not list artifacts"))
		return
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
) ([]string, error) {
	if len(opts.ArtifactPaths) > 0 {
		return dedupe(opts.ArtifactPaths), nil
	}

	var paths []string
	if len(referenced) > 0 {
		resolved, err := s.resolveArtifactPaths(ctx, principalID, projectID, referenced)
		if err != nil {
			return nil, err
		}
		paths = append(paths, resolved...)
	}

	// The convention half. One page is deliberate: if a project has more
	// than paging.DefaultLimit artifacts under context/, it has outgrown
	// "everything here is must-read" and needs the Phase 2 manifest, not a
	// bigger pack.
	page, err := s.artifacts.List(ctx, principalID, projectID, paging.Options{})
	if err != nil {
		return nil, err
	}
	for _, a := range page.Items {
		if strings.HasPrefix(a.LogicalPath, ConventionalArtifactPrefix) {
			paths = append(paths, a.LogicalPath)
		}
	}
	return dedupe(paths), nil
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

// fillEvents reports the tail of the event stream and the cursor to
// continue from.
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
