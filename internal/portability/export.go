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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/ideas"
	"github.com/koduj-dev/context-commander/internal/memories"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/plans"
)

// ErrForbidden is returned when the caller's role may not export.
var ErrForbidden = errors.New("portability: forbidden")

// Service exports and imports projects.
type Service struct {
	pool      *db.Pool
	authz     *auth.AuthzService
	artifacts *artifacts.Service
	memories  *memories.Service
	plans     *plans.Service
	ideas     *ideas.Service
	handoffs  *handoffs.Service
	history   *history.Service
}

func NewService(pool *db.Pool, authz *auth.AuthzService, a *artifacts.Service, m *memories.Service,
	p *plans.Service, i *ideas.Service, h *handoffs.Service, hi *history.Service) *Service {
	return &Service{pool: pool, authz: authz, artifacts: a, memories: m, plans: p, ideas: i, handoffs: h, history: hi}
}

// archive writes one tar.gz, each blob once.
type archive struct {
	tw    *tar.Writer
	now   time.Time
	blobs map[string]bool
}

func (a *archive) file(name string, data []byte) error {
	if err := a.tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: a.now, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := a.tw.Write(data)
	return err
}

func (a *archive) json(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return a.file(name, append(data, '\n'))
}

// blob stores content under its SHA-256 and returns the path it is at.
func (a *archive) blob(content []byte) (string, error) {
	sum := sha256.Sum256(content)
	name := blobDir + hex.EncodeToString(sum[:])
	if a.blobs[name] {
		return name, nil
	}
	a.blobs[name] = true
	return name, a.file(name, content)
}

// people collects the principals an archive names.
type people map[uuid.UUID]bool

func (p people) id(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	p[id] = true
	return id.String()
}

func (p people) ptr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return p.id(*id)
}

func str(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// Export writes projectID to w as a tar.gz archive and returns its
// manifest. It needs CapProjectExport (OWNER, ADMIN): an archive is the
// whole project in plaintext. Everything is read through the domain
// services as the caller. On error the archive is incomplete and has no
// manifest, and import refuses it.
func (s *Service) Export(ctx context.Context, principalID, projectID uuid.UUID, w io.Writer) (Manifest, error) {
	allowed, err := s.authz.Can(ctx, principalID, projectID, auth.CapProjectExport)
	if err != nil {
		return Manifest{}, fmt.Errorf("portability: authorization check: %w", err)
	}
	if !allowed {
		return Manifest{}, ErrForbidden
	}
	m := Manifest{Format: Format, Version: Version, ExportedAt: time.Now().UTC(), Counts: map[string]int{}, Excluded: Excluded}
	if m.Project, err = s.projectRef(ctx, principalID, projectID); err != nil {
		return Manifest{}, err
	}

	gz := gzip.NewWriter(w)
	a := &archive{tw: tar.NewWriter(gz), now: m.ExportedAt, blobs: map[string]bool{}}
	who := people{}
	steps := []struct {
		name string
		run  func() (int, error)
	}{
		{"artifacts", func() (int, error) { return s.exportArtifacts(ctx, principalID, projectID, a, who) }},
		{"memories", func() (int, error) { return s.exportMemories(ctx, principalID, projectID, a, who) }},
		{"plans", func() (int, error) { return s.exportPlans(ctx, principalID, projectID, a, who) }},
		{"ideas", func() (int, error) { return s.exportIdeas(ctx, principalID, projectID, a, who) }},
		{"handoffs", func() (int, error) { return s.exportHandoffs(ctx, principalID, projectID, a, who) }},
		{"conversations", func() (int, error) { return s.exportHistory(ctx, principalID, projectID, a, who) }},
	}
	for _, step := range steps {
		n, err := step.run()
		if err != nil {
			return Manifest{}, fmt.Errorf("portability: export %s: %w", step.name, err)
		}
		m.Counts[step.name] = n
	}
	list, err := s.resolvePeople(ctx, principalID, who)
	if err != nil {
		return Manifest{}, fmt.Errorf("portability: export people: %w", err)
	}
	if err := a.json(filePeople, list); err != nil {
		return Manifest{}, err
	}
	m.Complete = true
	if err := a.json(fileManifest, m); err != nil {
		return Manifest{}, err
	}
	if err := a.tw.Close(); err != nil {
		return Manifest{}, err
	}
	return m, gz.Close()
}

func (s *Service) projectRef(ctx context.Context, principalID, projectID uuid.UUID) (ProjectRef, error) {
	var ref ProjectRef
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var ns, slug string
		err := tx.QueryRow(ctx,
			`SELECT n.slug, p.slug, p.name FROM projects p JOIN namespaces n ON n.id = p.namespace_id WHERE p.id = $1`,
			projectID).Scan(&ns, &slug, &ref.Name)
		ref.Path = ns + "/" + slug
		return err
	})
	if err != nil {
		return ProjectRef{}, fmt.Errorf("portability: project: %w", err)
	}
	return ref, nil
}

// pages calls next until the cursor runs out.
func pages[T any](next func(cursor string) (paging.Page[T], error), each func(T) error) error {
	cursor := ""
	for {
		page, err := next(cursor)
		if err != nil {
			return err
		}
		for _, item := range page.Items {
			if err := each(item); err != nil {
				return err
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		cursor = page.NextCursor
	}
}

func (s *Service) exportArtifacts(ctx context.Context, principalID, projectID uuid.UUID, a *archive, who people) (int, error) {
	var out []Artifact
	err := pages(func(c string) (paging.Page[artifacts.Artifact], error) {
		return s.artifacts.List(ctx, principalID, projectID, paging.Options{Limit: paging.MaxLimit, Cursor: c})
	}, func(art artifacts.Artifact) error {
		versions, err := s.artifacts.History(ctx, principalID, projectID, art.LogicalPath)
		if err != nil {
			return err
		}
		entry := Artifact{Path: art.LogicalPath, ContentType: art.ContentType}
		sort.Slice(versions, func(i, j int) bool { return versions[i].Version < versions[j].Version })
		for _, v := range versions {
			n := v.Version
			_, content, err := s.artifacts.Read(ctx, principalID, projectID, art.LogicalPath, &n)
			if err != nil {
				return err
			}
			blob, err := a.blob(content)
			if err != nil {
				return err
			}
			entry.Versions = append(entry.Versions, ArtifactVersion{Version: v.Version, ContentHash: v.ContentHash,
				SizeBytes: v.SizeBytes, Blob: blob, CreatedBy: who.id(v.CreatedByPrincipal), CreatedAt: v.CreatedAt})
		}
		out = append(out, entry)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(out), a.json(fileArtifacts, nonNil(out))
}

func (s *Service) exportMemories(ctx context.Context, principalID, projectID uuid.UUID, a *archive, who people) (int, error) {
	var out []Memory
	err := pages(func(c string) (paging.Page[memories.Memory], error) {
		return s.memories.List(ctx, principalID, projectID, memories.Filter{Status: memories.StatusAny}, paging.Options{Limit: paging.MaxLimit, Cursor: c})
	}, func(m memories.Memory) error {
		full, err := s.memories.Get(ctx, principalID, projectID, m.ID)
		if err != nil {
			return err
		}
		out = append(out, Memory{ID: full.ID.String(), Kind: full.Kind, Status: full.Status, Title: full.Title, Body: full.Body,
			Branch: full.Branch, TicketKey: full.TicketKey, TicketURL: full.TicketURL, Tags: full.Tags,
			Assignee: who.ptr(full.Assignee), PlanID: str(full.PlanID), PlanItemID: str(full.PlanItemID),
			SupersededBy: str(full.SupersededBy), CreatedBy: who.id(full.CreatedBy), CreatedAt: full.CreatedAt, UpdatedAt: full.UpdatedAt})
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(out), a.json(fileMemories, nonNil(out))
}

func (s *Service) exportPlans(ctx context.Context, principalID, projectID uuid.UUID, a *archive, who people) (int, error) {
	phases, list, err := s.plans.Snapshot(ctx, principalID, projectID)
	if err != nil {
		return 0, err
	}
	out := Plans{Phases: []Phase{}, Plans: []Plan{}}
	for _, ph := range phases {
		out.Phases = append(out.Phases, Phase{ID: ph.ID.String(), Position: ph.Position, Status: ph.Status, Title: ph.Title,
			Goal: ph.Goal, CreatedBy: who.id(ph.CreatedBy), CreatedAt: ph.CreatedAt})
	}
	for _, p := range list {
		plan := Plan{ID: p.ID.String(), PhaseID: str(p.PhaseID), Status: p.Status, Title: p.Title, Body: p.Body,
			CreatedBy: who.id(p.CreatedBy), CreatedAt: p.CreatedAt, Items: []Item{}}
		for _, it := range p.Items {
			plan.Items = append(plan.Items, Item{ID: it.ID.String(), Position: it.Position, Kind: it.Kind, Status: it.Status,
				Text: it.Text, Note: it.Note, Assignee: who.ptr(it.Assignee), UpdatedBy: who.ptr(it.UpdatedBy),
				UpdatedAt: it.UpdatedAt, CompletedAt: it.CompletedAt})
		}
		out.Plans = append(out.Plans, plan)
	}
	return len(out.Plans), a.json(filePlans, out)
}

func (s *Service) exportIdeas(ctx context.Context, principalID, projectID uuid.UUID, a *archive, who people) (int, error) {
	var out []Idea
	err := pages(func(c string) (paging.Page[ideas.Idea], error) {
		return s.ideas.List(ctx, principalID, projectID, nil, paging.Options{Limit: paging.MaxLimit, Cursor: c})
	}, func(i ideas.Idea) error {
		out = append(out, Idea{ID: i.ID.String(), Status: i.Status, Priority: i.Priority, Position: i.Position,
			Title: i.Title, Body: i.Body, Proposal: i.Proposal, Rejection: i.Rejection, Branch: i.Branch,
			TicketKey: i.TicketKey, TicketURL: i.TicketURL, CreatedBy: who.id(i.CreatedBy), ClaimedBy: who.ptr(i.ClaimedBy),
			ProposedAt: i.ProposedAt, DecidedBy: who.ptr(i.DecidedBy), DecidedAt: i.DecidedAt,
			ResultingPlanID: str(i.ResultingPlanID), CreatedAt: i.CreatedAt})
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(out), a.json(fileIdeas, nonNil(out))
}

func (s *Service) exportHandoffs(ctx context.Context, principalID, projectID uuid.UUID, a *archive, who people) (int, error) {
	paths := map[uuid.UUID]string{}
	err := pages(func(c string) (paging.Page[artifacts.Artifact], error) {
		return s.artifacts.List(ctx, principalID, projectID, paging.Options{Limit: paging.MaxLimit, Cursor: c})
	}, func(art artifacts.Artifact) error {
		paths[art.ID] = art.LogicalPath
		return nil
	})
	if err != nil {
		return 0, err
	}
	var out []Handoff
	err = pages(func(c string) (paging.Page[handoffs.Handoff], error) {
		return s.handoffs.List(ctx, principalID, projectID, nil, paging.Options{Limit: paging.MaxLimit, Cursor: c})
	}, func(h handoffs.Handoff) error {
		_, body, err := s.handoffs.Get(ctx, principalID, projectID, h.ID)
		if err != nil {
			return err
		}
		entry := Handoff{ID: h.ID.String(), Status: h.Status, Summary: body.Summary, CompletedWork: body.CompletedWork,
			NextActions: body.NextActions, CreatedBy: who.id(h.CreatedByPrincipal), CreatedAt: h.CreatedAt,
			ClaimedBy: who.ptr(h.ClaimedByPrincipal), ClaimedAt: h.ClaimedAt, ClosedBy: who.ptr(h.ClosedByPrincipal), ClosedAt: h.ClosedAt}
		for _, id := range body.ReferencedArtifacts {
			if p, ok := paths[id]; ok {
				entry.ReferencedArtifacts = append(entry.ReferencedArtifacts, p)
			}
		}
		out = append(out, entry)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(out), a.json(fileHandoffs, nonNil(out))
}

func (s *Service) exportHistory(ctx context.Context, principalID, projectID uuid.UUID, a *archive, who people) (int, error) {
	out := History{Conversations: []Conversation{}}
	err := pages(func(c string) (paging.Page[history.Conversation], error) {
		return s.history.ListConversations(ctx, principalID, projectID, paging.Options{Limit: paging.MaxLimit, Cursor: c})
	}, func(conv history.Conversation) error {
		entry := Conversation{ID: conv.ID.String(), CreatedBy: who.id(conv.CreatedByPrincipal), CreatedAt: conv.CreatedAt,
			Sessions: []ExternalSession{}, Objects: []HistoryObject{}}
		sessions, err := s.history.ListExternalSessions(ctx, principalID, projectID, conv.ID)
		if err != nil {
			return err
		}
		for _, es := range sessions {
			entry.Sessions = append(entry.Sessions, ExternalSession{ID: es.ID.String(), Provider: es.Provider, Client: es.Client,
				ProviderSessionID: es.ProviderSessionID, ParentID: str(es.ParentExternalSessionID), Relation: es.Relation,
				StartedAt: es.StartedAt, EndedAt: es.EndedAt})
		}
		err = pages(func(c string) (paging.Page[history.HistoryObject], error) {
			return s.history.ListHistoryObjects(ctx, principalID, projectID, conv.ID, paging.Options{Limit: paging.MaxLimit, Cursor: c})
		}, func(o history.HistoryObject) error {
			blob, err := a.blob(o.Body)
			if err != nil {
				return err
			}
			entry.Objects = append(entry.Objects, HistoryObject{ID: o.ID.String(), ExternalSessionID: o.ExternalSessionID.String(),
				Type: o.Type, DerivedFrom: str(o.DerivedFrom), ContentHash: o.ContentHash, Blob: blob, CreatedAt: o.CreatedAt})
			return nil
		})
		if err != nil {
			return err
		}
		edges, err := s.history.GetLineage(ctx, principalID, projectID, conv.ID)
		if err != nil {
			return err
		}
		for _, e := range edges {
			entry.Lineage = append(entry.Lineage, LineageEdge{From: e.FromID.String(), To: e.ToID.String(), Kind: e.Kind})
		}
		out.Conversations = append(out.Conversations, entry)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(out.Conversations), a.json(fileHistory, out)
}

func (s *Service) resolvePeople(ctx context.Context, principalID uuid.UUID, who people) ([]Person, error) {
	out := []Person{}
	if len(who) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, 0, len(who))
	for id := range who {
		ids = append(ids, id)
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT p.id, p.kind, COALESCE(u.email, ''), COALESCE(NULLIF(u.display_name, ''), sp.name, '')
			 FROM principals p
			 LEFT JOIN users u ON u.id = p.user_id
			 LEFT JOIN service_principals sp ON sp.id = p.service_principal_id
			 WHERE p.id = ANY($1) ORDER BY p.id`, ids)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Person, error) {
			var p Person
			var id uuid.UUID
			err := r.Scan(&id, &p.Kind, &p.Email, &p.Name)
			p.ID = id.String()
			return p, err
		})
		return err
	})
	return out, err
}

func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
