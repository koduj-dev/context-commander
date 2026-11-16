package portability

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/ideas"
	"github.com/koduj-dev/context-commander/internal/memories"
	"github.com/koduj-dev/context-commander/internal/plans"
)

// MaxArchiveBytes bounds what an import unpacks into memory.
const MaxArchiveBytes = 2 << 30

// ErrBadArchive is an archive import refuses: not ours, a version this
// build does not read, incomplete, or a blob that does not match its name.
var ErrBadArchive = errors.New("portability: not a usable project archive")

// Report is what an import wrote.
type Report struct {
	Counts map[string]int
	// Mapped is how many of the archive's people were found in this
	// installation by email; the rest are attributed to the owner.
	Mapped, Unmapped int
}

// Files is an unpacked archive: name → content.
type Files map[string][]byte

func (f Files) decode(name string, v any) error {
	data, ok := f[name]
	if !ok {
		return fmt.Errorf("%w: %s is missing", ErrBadArchive, name)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrBadArchive, name, err)
	}
	return nil
}

// blob returns a blob after checking it is what its name says.
func (f Files) blob(name string) ([]byte, error) {
	data, ok := f[name]
	if !ok || !strings.HasPrefix(name, blobDir) {
		return nil, fmt.Errorf("%w: blob %s is missing", ErrBadArchive, name)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != strings.TrimPrefix(name, blobDir) {
		return nil, fmt.Errorf("%w: blob %s does not match its hash", ErrBadArchive, name)
	}
	return data, nil
}

// ReadArchive unpacks and checks an archive's manifest.
func ReadArchive(r io.Reader) (Files, Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, Manifest{}, fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	tr := tar.NewReader(gz)
	files := Files{}
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, Manifest{}, fmt.Errorf("%w: %v", ErrBadArchive, err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		total += h.Size
		if total > MaxArchiveBytes {
			return nil, Manifest{}, fmt.Errorf("%w: larger than %d bytes unpacked", ErrBadArchive, int64(MaxArchiveBytes))
		}
		data, err := io.ReadAll(io.LimitReader(tr, h.Size))
		if err != nil {
			return nil, Manifest{}, fmt.Errorf("%w: %v", ErrBadArchive, err)
		}
		files[h.Name] = data
	}
	var m Manifest
	if err := files.decode(fileManifest, &m); err != nil {
		return nil, Manifest{}, err
	}
	switch {
	case m.Format != Format:
		return nil, Manifest{}, fmt.Errorf("%w: format %q", ErrBadArchive, m.Format)
	case m.Version != Version:
		return nil, Manifest{}, fmt.Errorf("%w: version %d, this build reads %d", ErrBadArchive, m.Version, Version)
	case !m.Complete:
		return nil, Manifest{}, fmt.Errorf("%w: the export did not finish", ErrBadArchive)
	}
	return files, m, nil
}

// importer carries one import's state: the target, the owner every write is
// made as, and the old-to-new id maps cross-references are rewritten with.
type importer struct {
	s       *Service
	ctx     context.Context
	owner   uuid.UUID
	project uuid.UUID
	files   Files
	people  map[string]uuid.UUID // archive principal id → this installation's
	ids     map[string]uuid.UUID // archive record id → new id
	paths   map[string]uuid.UUID // logical path → new artifact id
	report  Report
	fixups  []fixup
}

// fixup is one statement that restores plaintext metadata (who, when,
// status) after a record was written through its domain package.
type fixup struct {
	sql  string
	args []any
}

func (im *importer) fix(sql string, args ...any) {
	im.fixups = append(im.fixups, fixup{sql: sql, args: args})
}

// who maps an archive principal; unknown and empty go to the owner.
func (im *importer) who(id string) uuid.UUID {
	if p, ok := im.people[id]; ok {
		return p
	}
	return im.owner
}

// whoOpt is who for an optional reference: empty stays empty.
func (im *importer) whoOpt(id string) *uuid.UUID {
	if id == "" {
		return nil
	}
	p := im.who(id)
	return &p
}

// ref is a rewritten cross-reference; one to a record that was not
// imported is dropped.
func (im *importer) ref(id string) *uuid.UUID {
	if n, ok := im.ids[id]; ok {
		return &n
	}
	return nil
}

// Import writes an archive's content into projectID, which must be new and
// owned by ownerID, and restores who wrote what and when.
//
// It must run on a pool connected as cc_owner (`ccd project import`): the
// restoring statements write columns cc_app may not, such as an artifact
// version's author, which is immutable to the application. Records are
// first written through their domain packages as the owner, so content is
// validated and encrypted under the new project's key exactly as a normal
// write would be; only plaintext metadata is restored directly.
func (s *Service) Import(ctx context.Context, ownerID, projectID uuid.UUID, files Files) (Report, error) {
	if err := s.requireOwnerConnection(ctx); err != nil {
		return Report{}, err
	}
	im := &importer{s: s, ctx: ctx, owner: ownerID, project: projectID, files: files,
		ids: map[string]uuid.UUID{}, paths: map[string]uuid.UUID{}, report: Report{Counts: map[string]int{}}}
	var err error
	if im.people, im.report.Mapped, im.report.Unmapped, err = s.mapPeople(ctx, files); err != nil {
		return Report{}, err
	}
	steps := []struct {
		name string
		run  func() (int, error)
	}{
		{"artifacts", im.artifacts},
		{"plans", im.plans},
		{"memories", im.memories},
		{"ideas", im.ideas},
		{"handoffs", im.handoffs},
		{"conversations", im.history},
	}
	for _, step := range steps {
		n, err := step.run()
		if err != nil {
			return im.report, fmt.Errorf("portability: import %s: %w", step.name, err)
		}
		im.report.Counts[step.name] = n
	}
	err = db.WithoutPrincipal(ctx, s.pool, func(tx pgx.Tx) error {
		for _, f := range im.fixups {
			if _, err := tx.Exec(ctx, f.sql, f.args...); err != nil {
				return fmt.Errorf("%s: %w", strings.Fields(f.sql)[1], err)
			}
		}
		return nil
	})
	if err != nil {
		return im.report, fmt.Errorf("portability: restore authorship and status: %w", err)
	}
	return im.report, nil
}

func (s *Service) requireOwnerConnection(ctx context.Context) error {
	var owner bool
	err := db.WithoutPrincipal(ctx, s.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT pg_has_role(current_user, (SELECT tableowner FROM pg_tables WHERE schemaname = 'public' AND tablename = 'projects'), 'MEMBER')`,
		).Scan(&owner)
	})
	if err != nil {
		return fmt.Errorf("portability: check connection role: %w", err)
	}
	if !owner {
		return errors.New("portability: import must connect as cc_owner, the role `ccd migrate` uses")
	}
	return nil
}

// mapPeople finds the archive's people in this installation by email.
func (s *Service) mapPeople(ctx context.Context, files Files) (map[string]uuid.UUID, int, int, error) {
	var list []Person
	if err := files.decode(filePeople, &list); err != nil {
		return nil, 0, 0, err
	}
	out := map[string]uuid.UUID{}
	unmapped := 0
	err := db.WithoutPrincipal(ctx, s.pool, func(tx pgx.Tx) error {
		for _, p := range list {
			if p.Email == "" {
				unmapped++
				continue
			}
			var id uuid.UUID
			err := tx.QueryRow(ctx,
				`SELECT p.id FROM principals p JOIN users u ON u.id = p.user_id WHERE lower(u.email) = lower($1)`, p.Email).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				unmapped++
				continue
			}
			if err != nil {
				return err
			}
			out[p.ID] = id
		}
		return nil
	})
	return out, len(out), unmapped, err
}

func (im *importer) artifacts() (int, error) {
	var list []Artifact
	if err := im.files.decode(fileArtifacts, &list); err != nil {
		return 0, err
	}
	for _, a := range list {
		sort.Slice(a.Versions, func(i, j int) bool { return a.Versions[i].Version < a.Versions[j].Version })
		var artifactID uuid.UUID
		for i, v := range a.Versions {
			content, err := im.files.blob(v.Blob)
			if err != nil {
				return 0, err
			}
			written, err := im.s.artifacts.Write(im.ctx, im.owner, nil, im.project, a.Path, a.ContentType, content, i)
			if err != nil {
				return 0, fmt.Errorf("%s v%d: %w", a.Path, v.Version, err)
			}
			artifactID = written.ArtifactID
			im.fix(`UPDATE artifact_versions SET created_by_principal = $1, created_at = $2 WHERE id = $3`,
				im.who(v.CreatedBy), v.CreatedAt, written.ID)
		}
		if len(a.Versions) == 0 {
			continue
		}
		first, last := a.Versions[0], a.Versions[len(a.Versions)-1]
		im.fix(`UPDATE artifacts SET created_by_principal = $1, created_at = $2, updated_at = $3 WHERE id = $4`,
			im.who(first.CreatedBy), first.CreatedAt, last.CreatedAt, artifactID)
		im.paths[a.Path] = artifactID
	}
	return len(list), nil
}

func (im *importer) plans() (int, error) {
	var in Plans
	if err := im.files.decode(filePlans, &in); err != nil {
		return 0, err
	}
	sort.Slice(in.Phases, func(i, j int) bool { return in.Phases[i].Position < in.Phases[j].Position })
	for _, ph := range in.Phases {
		created, err := im.s.plans.CreatePhase(im.ctx, im.owner, im.project, ph.Title, ph.Goal)
		if err != nil {
			return 0, fmt.Errorf("phase %q: %w", ph.Title, err)
		}
		im.ids[ph.ID] = created.ID
		im.fix(`UPDATE phases SET status = $1, created_by_principal = $2, created_at = $3 WHERE id = $4`,
			ph.Status, im.who(ph.CreatedBy), ph.CreatedAt, created.ID)
	}
	for _, p := range in.Plans {
		sort.Slice(p.Items, func(i, j int) bool { return p.Items[i].Position < p.Items[j].Position })
		items := make([]plans.NewItem, len(p.Items))
		for i, it := range p.Items {
			items[i] = plans.NewItem{Text: it.Text, Kind: it.Kind}
		}
		created, err := im.s.plans.CreatePlan(im.ctx, im.owner, nil, im.project, im.ref(p.PhaseID), p.Title, p.Body, items)
		if err != nil {
			return 0, fmt.Errorf("plan %q: %w", p.Title, err)
		}
		im.ids[p.ID] = created.ID
		im.fix(`UPDATE plans SET status = $1, created_by_principal = $2, created_at = $3 WHERE id = $4`,
			p.Status, im.who(p.CreatedBy), p.CreatedAt, created.ID)
		if len(created.Items) != len(p.Items) {
			return 0, fmt.Errorf("plan %q: wrote %d items of %d", p.Title, len(created.Items), len(p.Items))
		}
		for i, it := range p.Items {
			newItem := created.Items[i]
			im.ids[it.ID] = newItem.ID
			if it.Note != "" {
				note := it.Note
				if _, err := im.s.plans.UpdateItem(im.ctx, im.owner, nil, im.project, newItem.ID, plans.ItemUpdate{Note: &note}); err != nil {
					return 0, fmt.Errorf("plan %q item %d note: %w", p.Title, i+1, err)
				}
			}
			im.fix(`UPDATE plan_items SET status = $1, assignee_principal = $2, updated_by_principal = $3, updated_at = $4, completed_at = $5 WHERE id = $6`,
				it.Status, im.whoOpt(it.Assignee), im.whoOpt(it.UpdatedBy), it.UpdatedAt, it.CompletedAt, newItem.ID)
		}
	}
	return len(in.Plans), nil
}

func (im *importer) memories() (int, error) {
	var list []Memory
	if err := im.files.decode(fileMemories, &list); err != nil {
		return 0, err
	}
	for _, m := range list {
		created, err := im.s.memories.Create(im.ctx, im.owner, nil, im.project, memories.NewMemory{
			Kind: m.Kind, Title: m.Title, Body: m.Body, Branch: m.Branch, TicketKey: m.TicketKey, TicketURL: m.TicketURL,
			Tags: m.Tags, Assignee: im.whoOpt(m.Assignee), PlanID: im.ref(m.PlanID), PlanItemID: im.ref(m.PlanItemID),
		})
		if err != nil {
			return 0, fmt.Errorf("memory %q: %w", m.Title, err)
		}
		im.ids[m.ID] = created.ID
		im.fix(`UPDATE memories SET status = $1, created_by_principal = $2, created_at = $3, updated_at = $4 WHERE id = $5`,
			m.Status, im.who(m.CreatedBy), m.CreatedAt, m.UpdatedAt, created.ID)
	}
	// Superseded-by points forward as often as back, so it is rewritten once
	// every memory has its new id.
	for _, m := range list {
		if by := im.ref(m.SupersededBy); by != nil {
			im.fix(`UPDATE memories SET superseded_by = $1 WHERE id = $2`, *by, im.ids[m.ID])
		}
	}
	return len(list), nil
}

func (im *importer) ideas() (int, error) {
	var list []Idea
	if err := im.files.decode(fileIdeas, &list); err != nil {
		return 0, err
	}
	for _, i := range list {
		created, err := im.s.ideas.Create(im.ctx, im.owner, nil, im.project, ideas.NewIdea{
			Title: i.Title, Body: i.Body, Priority: i.Priority, Branch: i.Branch, TicketKey: i.TicketKey, TicketURL: i.TicketURL,
		})
		if err != nil {
			return 0, fmt.Errorf("idea %q: %w", i.Title, err)
		}
		im.ids[i.ID] = created.ID
		if i.Proposal != "" || i.Rejection != "" {
			if err := im.s.ideas.RestoreText(im.ctx, im.owner, im.project, created.ID, i.Proposal, i.Rejection); err != nil {
				return 0, fmt.Errorf("idea %q: %w", i.Title, err)
			}
		}
		// A claim is a lease held by a client of the old installation; here
		// it means nothing, so a claimed idea comes back open.
		status, claimedBy := i.Status, im.whoOpt(i.ClaimedBy)
		if status == ideas.StatusClaimed {
			status, claimedBy = ideas.StatusOpen, nil
		}
		im.fix(`UPDATE ideas SET status = $1, position = $2, claimed_by_principal = $3, claimed_at = NULL, claim_expires_at = NULL,
		        proposed_at = $4, decided_by_principal = $5, decided_at = $6, resulting_plan_id = $7,
		        created_by_principal = $8, created_at = $9 WHERE id = $10`,
			status, i.Position, claimedBy, i.ProposedAt, im.whoOpt(i.DecidedBy), i.DecidedAt, im.ref(i.ResultingPlanID),
			im.who(i.CreatedBy), i.CreatedAt, created.ID)
	}
	return len(list), nil
}

func (im *importer) handoffs() (int, error) {
	var list []Handoff
	if err := im.files.decode(fileHandoffs, &list); err != nil {
		return 0, err
	}
	// Oldest first, so "the newest open handoff" stays the same one.
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	for _, h := range list {
		body := handoffs.Body{Summary: h.Summary, CompletedWork: h.CompletedWork, NextActions: h.NextActions}
		for _, p := range h.ReferencedArtifacts {
			if id, ok := im.paths[p]; ok {
				body.ReferencedArtifacts = append(body.ReferencedArtifacts, id)
			}
		}
		created, err := im.s.handoffs.Create(im.ctx, im.owner, nil, im.project, nil, body)
		if err != nil {
			return 0, fmt.Errorf("handoff %s: %w", h.ID, err)
		}
		im.ids[h.ID] = created.ID
		var claimedAt, closedAt *time.Time
		var claimedBy, closedBy *uuid.UUID
		// The schema ties the timestamps to the status; an archive that
		// lacks one gets the handoff's creation time rather than a refusal.
		if h.Status == handoffs.StatusClaimed || (h.Status == handoffs.StatusClosed && h.ClaimedAt != nil) {
			claimedAt, claimedBy = orTime(h.ClaimedAt, h.CreatedAt), im.whoOpt(h.ClaimedBy)
			if claimedBy == nil {
				claimedBy = &im.owner
			}
		}
		if h.Status == handoffs.StatusClosed {
			closedAt, closedBy = orTime(h.ClosedAt, h.CreatedAt), im.whoOpt(h.ClosedBy)
			if closedBy == nil {
				closedBy = &im.owner
			}
		}
		im.fix(`UPDATE handoffs SET status = $1, created_by_principal = $2, created_at = $3,
		        claimed_at = $4, claimed_by_principal = $5, closed_at = $6, closed_by_principal = $7 WHERE id = $8`,
			h.Status, im.who(h.CreatedBy), h.CreatedAt, claimedAt, claimedBy, closedAt, closedBy, created.ID)
	}
	return len(list), nil
}

func (im *importer) history() (int, error) {
	var in History
	if err := im.files.decode(fileHistory, &in); err != nil {
		return 0, err
	}
	for _, c := range in.Conversations {
		conv, err := im.s.history.CreateConversation(im.ctx, im.owner, im.project)
		if err != nil {
			return 0, fmt.Errorf("conversation %s: %w", c.ID, err)
		}
		im.ids[c.ID] = conv.ID
		im.fix(`UPDATE conversations SET created_by_principal = $1, created_at = $2 WHERE id = $3`, im.who(c.CreatedBy), c.CreatedAt, conv.ID)
		sort.Slice(c.Sessions, func(i, j int) bool { return c.Sessions[i].StartedAt.Before(c.Sessions[j].StartedAt) })
		for _, es := range c.Sessions {
			created, err := im.s.history.CreateExternalSession(im.ctx, im.owner, im.project, conv.ID,
				es.Provider, es.Client, es.ProviderSessionID, im.ref(es.ParentID), es.Relation, es.StartedAt)
			if err != nil {
				return 0, fmt.Errorf("conversation %s session %s: %w", c.ID, es.ID, err)
			}
			im.ids[es.ID] = created.ID
			if es.EndedAt != nil {
				im.fix(`UPDATE external_sessions SET ended_at = $1 WHERE id = $2`, *es.EndedAt, created.ID)
			}
		}
		// A derived object needs its parent first; creation order is that.
		sort.Slice(c.Objects, func(i, j int) bool { return c.Objects[i].CreatedAt.Before(c.Objects[j].CreatedAt) })
		for _, o := range c.Objects {
			session := im.ref(o.ExternalSessionID)
			if session == nil {
				return 0, fmt.Errorf("%w: object %s names a session the archive does not have", ErrBadArchive, o.ID)
			}
			body, err := im.files.blob(o.Blob)
			if err != nil {
				return 0, err
			}
			created, err := im.s.history.AppendHistoryObject(im.ctx, im.owner, im.project, conv.ID, *session, o.Type, im.ref(o.DerivedFrom), body)
			if err != nil {
				return 0, fmt.Errorf("conversation %s object %s: %w", c.ID, o.ID, err)
			}
			im.ids[o.ID] = created.ID
			im.fix(`UPDATE history_objects SET created_at = $1 WHERE id = $2`, o.CreatedAt, created.ID)
		}
	}
	return len(in.Conversations), nil
}

func orTime(t *time.Time, fallback time.Time) *time.Time {
	if t != nil {
		return t
	}
	return &fallback
}

// Validate decodes every file and checks every blob an archive names, so a
// broken archive is refused before a project is created for it.
func Validate(files Files) error {
	var (
		people []Person
		arts   []Artifact
		mems   []Memory
		pl     Plans
		ids    []Idea
		hs     []Handoff
		hist   History
	)
	for name, v := range map[string]any{filePeople: &people, fileArtifacts: &arts, fileMemories: &mems,
		filePlans: &pl, fileIdeas: &ids, fileHandoffs: &hs, fileHistory: &hist} {
		if err := files.decode(name, v); err != nil {
			return err
		}
	}
	for _, a := range arts {
		for _, v := range a.Versions {
			if _, err := files.blob(v.Blob); err != nil {
				return err
			}
		}
	}
	for _, c := range hist.Conversations {
		for _, o := range c.Objects {
			if _, err := files.blob(o.Blob); err != nil {
				return err
			}
		}
	}
	return nil
}
