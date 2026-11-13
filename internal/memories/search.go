package memories

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type cursor struct {
	t  time.Time
	id uuid.UUID
}

// MaxSearchHits bounds one search's answer.
const MaxSearchHits = 100

// MaxSearchScan bounds how many memories one project's search decrypts:
// the newest that pass the metadata filter. The text is encrypted
// (docs/adr/0011), so every candidate costs a decrypt; past this the answer
// says it is truncated, and a filter (branch, ticket, kind, a date) narrows
// what is scanned before anything is decrypted.
const MaxSearchScan = 1000

// MaxSearchProjects and MaxSearchScanPerProject bound a search across the
// caller's projects: at most this many projects, the caller's newest, and
// this many candidates in each. Without them the price of a search would
// grow with the caller's membership list rather than with the question.
const (
	MaxSearchProjects       = 50
	MaxSearchScanPerProject = 200
)

// snippetRunes is how much of a body a hit carries around its match.
const snippetRunes = 240

// Ranker decides which candidates answer a query and in what order.
//
// It is the seam for semantic search (docs/adr/0025): candidates arrive
// already narrowed by the SQL metadata filter and decrypted, newest first,
// so a ranker that embeds the query and compares it against stored
// embeddings can replace TextRanker without any caller — MCP, HTTP, the
// interface — changing. A ranker sees only what the caller may read; the
// authorization and the RLS read happen before it is called.
type Ranker interface {
	Rank(ctx context.Context, query string, candidates []Memory) ([]Hit, error)
}

// TextRanker is a case-insensitive substring match: a hit in the title
// ranks before a hit in the body, and newer before older within each.
type TextRanker struct{}

func (TextRanker) Rank(_ context.Context, query string, candidates []Memory) ([]Hit, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	var title, body []Hit
	for _, m := range candidates {
		switch {
		case strings.Contains(strings.ToLower(m.Title), q):
			title = append(title, Hit{Memory: m, Match: MatchTitle, Snippet: snippet(m.Body, q)})
		case strings.Contains(strings.ToLower(m.Body), q):
			body = append(body, Hit{Memory: m, Match: MatchBody, Snippet: snippet(m.Body, q)})
		}
	}
	// Candidates come newest first; a stable sort keeps that order inside
	// each group even when they were gathered from several projects.
	newest := func(h []Hit) {
		sort.SliceStable(h, func(i, j int) bool { return h[i].Memory.CreatedAt.After(h[j].Memory.CreatedAt) })
	}
	newest(title)
	newest(body)
	return append(title, body...), nil
}

// snippet is about snippetRunes of body around the first match of q, or
// the start of the body when q is not in it. Cut on rune boundaries, with
// an ellipsis where text was left out.
func snippet(body, q string) string {
	runes := []rune(body)
	if len(runes) <= snippetRunes {
		return body
	}
	start := 0
	if i := strings.Index(strings.ToLower(body), q); i >= 0 && q != "" {
		// strings.ToLower can change byte lengths, so map back through the
		// rune count of the prefix rather than reuse the byte index.
		start = utf8.RuneCountInString(strings.ToLower(body)[:i]) - snippetRunes/3
		if start < 0 {
			start = 0
		}
	}
	end := start + snippetRunes
	if end > len(runes) {
		end = len(runes)
		start = max(0, end-snippetRunes)
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

func checkQuery(query string) error {
	if strings.TrimSpace(query) == "" {
		return invalid("search needs a query; list memories to filter by metadata alone")
	}
	return checkLen("query", query, MaxTitle)
}

// rank runs the ranker and caps the answer, saying whether it cut anything.
func (s *Service) rank(ctx context.Context, query string, candidates []Memory) ([]Hit, bool, error) {
	hits, err := s.ranker.Rank(ctx, query, candidates)
	if err != nil {
		return nil, false, fmt.Errorf("memories: rank: %w", err)
	}
	if len(hits) > MaxSearchHits {
		return hits[:MaxSearchHits], true, nil
	}
	return hits, false, nil
}

// Search finds a project's memories whose text contains query, among those
// the metadata filter lets through. The filter is SQL; the text match
// decrypts at most MaxSearchScan candidates, newest first.
func (s *Service) Search(ctx context.Context, principalID, projectID uuid.UUID, query string, f Filter) (SearchResult, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return SearchResult{}, err
	}
	if err := checkQuery(query); err != nil {
		return SearchResult{}, err
	}
	if err := validFilter(f); err != nil {
		return SearchResult{}, err
	}
	var out SearchResult
	var candidates []Memory
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		if candidates, err = s.query(ctx, tx, projectID, f, nil, MaxSearchScan+1); err != nil {
			return err
		}
		if len(candidates) > MaxSearchScan {
			candidates, out.Truncated = candidates[:MaxSearchScan], true
		}
		return nil
	})
	if err != nil {
		return SearchResult{}, fmt.Errorf("memories: search: %w", err)
	}
	var cut bool
	if out.Hits, cut, err = s.rank(ctx, query, candidates); err != nil {
		return SearchResult{}, err
	}
	out.Truncated = out.Truncated || cut
	if out.People, err = s.People(ctx, principalID, hitMemories(out.Hits)); err != nil {
		return SearchResult{}, err
	}
	return out, nil
}

// SearchAll is Search across every project the caller can read memories
// in, each project's memories decrypted with that project's own key. Which
// projects those are is decided the way auth.AuthzService.Can decides it
// for one — the caller's membership role through auth.RoleCan — and both
// the memberships and the memories are read under the caller's RLS context,
// so a project the caller is not in is neither listed nor readable even if
// the Go check were wrong. Mirrors plans.Service.SearchAll.
func (s *Service) SearchAll(ctx context.Context, principalID uuid.UUID, query string, f Filter) (SearchResult, error) {
	if err := checkQuery(query); err != nil {
		return SearchResult{}, err
	}
	if err := validFilter(f); err != nil {
		return SearchResult{}, err
	}
	var out SearchResult
	var candidates []Memory
	refs := map[uuid.UUID]ProjectRef{}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		memberships, err := projects.NewStore().ListForPrincipal(ctx, tx, principalID)
		if err != nil {
			return err
		}
		searched := 0
		for _, ms := range memberships {
			if !auth.RoleCan(ms.Role, auth.CapPlanRead) {
				continue
			}
			if searched == MaxSearchProjects {
				out.Truncated = true
				break
			}
			searched++
			list, err := s.query(ctx, tx, ms.Project.ID, f, nil, MaxSearchScanPerProject+1)
			if err != nil {
				return err
			}
			if len(list) > MaxSearchScanPerProject {
				list, out.Truncated = list[:MaxSearchScanPerProject], true
			}
			candidates = append(candidates, list...)
			refs[ms.Project.ID] = ProjectRef{ID: ms.Project.ID, Path: ms.NamespaceSlug + "/" + ms.Project.Slug, Name: ms.Project.Name}
		}
		return nil
	})
	if err != nil {
		return SearchResult{}, fmt.Errorf("memories: search all: %w", err)
	}
	var cut bool
	if out.Hits, cut, err = s.rank(ctx, query, candidates); err != nil {
		return SearchResult{}, err
	}
	out.Truncated = out.Truncated || cut
	named := map[uuid.UUID]bool{}
	for _, h := range out.Hits {
		if !named[h.Memory.ProjectID] {
			named[h.Memory.ProjectID] = true
			out.Projects = append(out.Projects, refs[h.Memory.ProjectID])
		}
	}
	if out.People, err = s.People(ctx, principalID, hitMemories(out.Hits)); err != nil {
		return SearchResult{}, err
	}
	return out, nil
}

func hitMemories(hits []Hit) []Memory {
	out := make([]Memory, len(hits))
	for i, h := range hits {
		out[i] = h.Memory
	}
	return out
}

// Recent is the newest active memories of a project, for context.bootstrap:
// enough for an arriving agent to know they exist and search for the rest.
// It reports whether there were more than limit.
func (s *Service) Recent(ctx context.Context, principalID, projectID uuid.UUID, limit int) ([]Memory, bool, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return nil, false, err
	}
	var list []Memory
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		list, err = s.query(ctx, tx, projectID, Filter{}, nil, limit+1)
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("memories: recent: %w", err)
	}
	if len(list) > limit {
		return list[:limit], true, nil
	}
	return list, false, nil
}

// MaxFacetValues bounds each list in Facets.
const MaxFacetValues = 50

// Facets are the metadata values a project's active memories use, most
// used first: what a screen offers as filters. Plaintext metadata only —
// nothing is decrypted to answer it.
type Facets struct {
	Branches  []string
	Tickets   []string
	Tags      []string
	Assignees []uuid.UUID
	People    map[uuid.UUID]Person
}

func (s *Service) Facets(ctx context.Context, principalID, projectID uuid.UUID) (Facets, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return Facets{}, err
	}
	var out Facets
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		// Each query is a fixed string from this function; the column
		// names are not caller input.
		values := func(q string) ([]string, error) {
			rows, err := tx.Query(ctx, q, projectID, StatusActive, MaxFacetValues)
			if err != nil {
				return nil, err
			}
			return pgx.CollectRows(rows, pgx.RowTo[string])
		}
		var err error
		if out.Branches, err = values(`SELECT branch FROM memories WHERE project_id = $1 AND status = $2 AND branch IS NOT NULL
			GROUP BY branch ORDER BY count(*) DESC, branch LIMIT $3`); err != nil {
			return err
		}
		if out.Tickets, err = values(`SELECT ticket_key FROM memories WHERE project_id = $1 AND status = $2 AND ticket_key IS NOT NULL
			GROUP BY ticket_key ORDER BY count(*) DESC, ticket_key LIMIT $3`); err != nil {
			return err
		}
		if out.Tags, err = values(`SELECT t FROM memories, unnest(tags) AS t WHERE project_id = $1 AND status = $2
			GROUP BY t ORDER BY count(*) DESC, t LIMIT $3`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT assignee_principal FROM memories WHERE project_id = $1 AND status = $2 AND assignee_principal IS NOT NULL
			GROUP BY assignee_principal ORDER BY count(*) DESC LIMIT $3`, projectID, StatusActive, MaxFacetValues)
		if err != nil {
			return err
		}
		if out.Assignees, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return err
		}
		ids := map[uuid.UUID]bool{}
		for _, id := range out.Assignees {
			ids[id] = true
		}
		out.People, err = resolvePeople(ctx, tx, ids)
		return err
	})
	if err != nil {
		return Facets{}, fmt.Errorf("memories: facets: %w", err)
	}
	return out, nil
}
