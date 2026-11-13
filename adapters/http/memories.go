package http

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/memories"
)

// Memories (docs/adr/0025). The shapes mirror the MCP tools'
// (adapters/mcp/tools_memory.go) with plans' one difference: people travel
// as ids plus a `people` map, because a screen renders a name in many places.

type memoryResponse struct {
	ID           uuid.UUID  `json:"id"`
	Kind         string     `json:"kind"`
	Status       string     `json:"status"`
	Version      int        `json:"version"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	Branch       string     `json:"branch,omitempty"`
	TicketKey    string     `json:"ticket_key,omitempty"`
	TicketURL    string     `json:"ticket_url,omitempty"`
	Tags         []string   `json:"tags"`
	Assignee     *uuid.UUID `json:"assignee,omitempty"`
	PlanID       *uuid.UUID `json:"plan_id,omitempty"`
	PlanItemID   *uuid.UUID `json:"plan_item_id,omitempty"`
	SupersededBy *uuid.UUID `json:"superseded_by,omitempty"`
	CreatedBy    uuid.UUID  `json:"created_by"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
}

func memoryFrom(m memories.Memory) memoryResponse {
	r := memoryResponse{
		ID: m.ID, Kind: m.Kind, Status: m.Status, Version: m.Version, Title: m.Title, Body: m.Body,
		Branch: m.Branch, TicketKey: m.TicketKey, TicketURL: m.TicketURL, Tags: m.Tags,
		Assignee: m.Assignee, PlanID: m.PlanID, PlanItemID: m.PlanItemID, SupersededBy: m.SupersededBy,
		CreatedBy: m.CreatedBy, CreatedAt: m.CreatedAt.Format(time.RFC3339), UpdatedAt: m.UpdatedAt.Format(time.RFC3339),
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	return r
}

func memoryPeopleFrom(m map[uuid.UUID]memories.Person) map[string]personResponse {
	out := make(map[string]personResponse, len(m))
	for id, p := range m {
		out[id.String()] = personResponse{Kind: p.Kind, Name: p.Name}
	}
	return out
}

// memoryFilterFrom reads the metadata filters list and search share. A
// malformed id or time is a 400 here; an unknown kind or status is the
// service's to refuse.
func memoryFilterFrom(w http.ResponseWriter, q url.Values) (memories.Filter, bool) {
	f := memories.Filter{
		Kind: q.Get("kind"), Branch: q.Get("branch"), TicketKey: q.Get("ticket"), Tag: q.Get("tag"), Status: q.Get("status"),
	}
	for key, dst := range map[string]**uuid.UUID{"assignee": &f.Assignee, "author": &f.Author} {
		if raw := q.Get(key); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "?"+key+"= must be a principal UUID")
				return memories.Filter{}, false
			}
			*dst = &id
		}
	}
	for key, dst := range map[string]**time.Time{"created_after": &f.CreatedAfter, "created_before": &f.CreatedBefore} {
		if raw := q.Get(key); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "?"+key+"= must be an RFC 3339 time")
				return memories.Filter{}, false
			}
			*dst = &t
		}
	}
	return f, true
}

func (d Deps) listMemories(w http.ResponseWriter, r *http.Request) {
	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	f, ok := memoryFilterFrom(w, r.URL.Query())
	if !ok {
		return
	}
	principalID := principalIDFrom(r.Context())
	page, err := d.Memories.List(r.Context(), principalID, projectFrom(r.Context()).ID, f, opts)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	people, err := d.Memories.People(r.Context(), principalID, page.Items)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	// The list envelope plus `people`, so the screen can name authors and
	// assignees without a read per row.
	out := struct {
		Items      []memoryResponse          `json:"items"`
		NextCursor string                    `json:"next_cursor,omitempty"`
		People     map[string]personResponse `json:"people"`
	}{Items: []memoryResponse{}, NextCursor: page.NextCursor, People: memoryPeopleFrom(people)}
	for _, m := range page.Items {
		out.Items = append(out.Items, memoryFrom(m))
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) getMemoryFacets(w http.ResponseWriter, r *http.Request) {
	fc, err := d.Memories.Facets(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	nonNil := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	// search_modes says what the server can answer, so the interface offers
	// a mode switch only when semantic search is configured rather than a
	// control that would fail (docs/adr/0028).
	out := struct {
		Kinds             []string                  `json:"kinds"`
		Branches          []string                  `json:"branches"`
		Tickets           []string                  `json:"tickets"`
		Tags              []string                  `json:"tags"`
		Assignees         []uuid.UUID               `json:"assignees"`
		People            map[string]personResponse `json:"people"`
		SearchModes       []string                  `json:"search_modes"`
		DefaultSearchMode string                    `json:"default_search_mode"`
	}{
		Kinds: memories.Kinds, Branches: nonNil(fc.Branches), Tickets: nonNil(fc.Tickets), Tags: nonNil(fc.Tags),
		Assignees: fc.Assignees, People: memoryPeopleFrom(fc.People),
		SearchModes: d.Memories.Modes(), DefaultSearchMode: d.Memories.DefaultMode(),
	}
	if out.Assignees == nil {
		out.Assignees = []uuid.UUID{}
	}
	writeJSON(w, http.StatusOK, out)
}

type memoryHitResponse struct {
	memoryResponse
	Project     string  `json:"project,omitempty"`
	ProjectName string  `json:"project_name,omitempty"`
	Match       string  `json:"match"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score,omitempty"`
}

// writeMemorySearch answers both searches: an object, so it can say which
// project each hit is in and whether a bound cut the answer. Bodies are
// left out of hits; the snippet stands in, and the memory itself has the rest.
func writeMemorySearch(w http.ResponseWriter, res memories.SearchResult) {
	refs := map[uuid.UUID]memories.ProjectRef{}
	for _, p := range res.Projects {
		refs[p.ID] = p
	}
	// mode is the mode that answered, so a caller that sent none learns
	// the default; degraded says a hybrid search fell back to text because
	// the embedding provider failed (docs/adr/0028).
	out := struct {
		Items     []memoryHitResponse       `json:"items"`
		Truncated bool                      `json:"truncated"`
		Mode      string                    `json:"mode"`
		Degraded  bool                      `json:"degraded,omitempty"`
		People    map[string]personResponse `json:"people"`
	}{Items: []memoryHitResponse{}, Truncated: res.Truncated, Mode: res.Mode, Degraded: res.Degraded, People: memoryPeopleFrom(res.People)}
	for _, h := range res.Hits {
		m := memoryFrom(h.Memory)
		m.Body = ""
		ref := refs[h.Memory.ProjectID]
		out.Items = append(out.Items, memoryHitResponse{
			memoryResponse: m, Project: ref.Path, ProjectName: ref.Name, Match: h.Match, Snippet: h.Snippet, Score: h.Score,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) searchMemories(w http.ResponseWriter, r *http.Request) {
	f, ok := memoryFilterFrom(w, r.URL.Query())
	if !ok {
		return
	}
	q := r.URL.Query()
	res, err := d.Memories.Search(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, q.Get("q"), q.Get("mode"), f)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeMemorySearch(w, res)
}

// searchMyMemories is searchMemories across every project the caller can
// read (memories.Service.SearchAll).
func (d Deps) searchMyMemories(w http.ResponseWriter, r *http.Request) {
	f, ok := memoryFilterFrom(w, r.URL.Query())
	if !ok {
		return
	}
	q := r.URL.Query()
	res, err := d.Memories.SearchAll(r.Context(), principalIDFrom(r.Context()), q.Get("q"), q.Get("mode"), f)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeMemorySearch(w, res)
}

// relatedMemories is the memories most similar in meaning to one memory
// (docs/adr/0028), shaped like a search answer. A 400 when the server has
// no embedding provider.
func (d Deps) relatedMemories(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "memoryID")
	if !ok {
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid_request", "?limit= must be a positive integer")
			return
		}
		limit = n
	}
	res, err := d.Memories.Related(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id, limit)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeMemorySearch(w, res)
}

func (d Deps) createMemory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind       string     `json:"kind"`
		Title      string     `json:"title"`
		Body       string     `json:"body"`
		Branch     string     `json:"branch"`
		TicketKey  string     `json:"ticket_key"`
		TicketURL  string     `json:"ticket_url"`
		Tags       []string   `json:"tags"`
		Assignee   *uuid.UUID `json:"assignee"`
		PlanID     *uuid.UUID `json:"plan_id"`
		PlanItemID *uuid.UUID `json:"plan_item_id"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	m, err := d.Memories.Create(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), projectFrom(r.Context()).ID,
		memories.NewMemory{
			Kind: req.Kind, Title: req.Title, Body: req.Body, Branch: req.Branch, TicketKey: req.TicketKey, TicketURL: req.TicketURL,
			Tags: req.Tags, Assignee: req.Assignee, PlanID: req.PlanID, PlanItemID: req.PlanItemID,
		})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, memoryFrom(m))
}

func (d Deps) getMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "memoryID")
	if !ok {
		return
	}
	m, err := d.Memories.Get(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, memoryFrom(m))
}

func (d Deps) updateMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "memoryID")
	if !ok {
		return
	}
	// assignee is read raw so that "absent" (leave it) and null (clear it)
	// stay different.
	var req struct {
		ExpectedVersion int             `json:"expected_version"`
		Kind            *string         `json:"kind"`
		Title           *string         `json:"title"`
		Body            *string         `json:"body"`
		Branch          *string         `json:"branch"`
		TicketKey       *string         `json:"ticket_key"`
		TicketURL       *string         `json:"ticket_url"`
		Tags            *[]string       `json:"tags"`
		Assignee        json.RawMessage `json:"assignee"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	u := memories.Update{
		ExpectedVersion: req.ExpectedVersion, Kind: req.Kind, Title: req.Title, Body: req.Body,
		Branch: req.Branch, TicketKey: req.TicketKey, TicketURL: req.TicketURL, Tags: req.Tags,
	}
	if len(req.Assignee) > 0 {
		u.AssigneeSet = true
		if err := json.Unmarshal(req.Assignee, &u.Assignee); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "assignee must be a principal UUID or null")
			return
		}
	}
	m, err := d.Memories.Update(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id, u)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, memoryFrom(m))
}

// setMemoryStatus archives, reactivates or supersedes a memory: with
// superseded_by it supersedes, otherwise it sets status.
func (d Deps) setMemoryStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "memoryID")
	if !ok {
		return
	}
	var req struct {
		Status       string     `json:"status"`
		SupersededBy *uuid.UUID `json:"superseded_by"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	principalID, projectID := principalIDFrom(r.Context()), projectFrom(r.Context()).ID
	var m memories.Memory
	var err error
	if req.SupersededBy != nil {
		m, err = d.Memories.Supersede(r.Context(), principalID, projectID, id, *req.SupersededBy)
	} else {
		m, err = d.Memories.SetStatus(r.Context(), principalID, projectID, id, req.Status)
	}
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, memoryFrom(m))
}

func (d Deps) deleteMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "memoryID")
	if !ok {
		return
	}
	if err := d.Memories.Delete(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), projectFrom(r.Context()).ID, id); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
