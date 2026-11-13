package http

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/ideas"
)

// The idea stack (docs/adr/0026). The shapes mirror the MCP tools'
// (adapters/mcp/tools_idea.go); people travel as ids plus a `people` map, as
// on the plan board. Accept and reject exist only here: they need a person
// in a browser session, which the service checks.

type ideaResponse struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	Position  int       `json:"position"`
	Version   int       `json:"version"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	Proposal  string    `json:"proposal,omitempty"`
	Rejection string    `json:"rejection,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	TicketKey string    `json:"ticket_key,omitempty"`
	TicketURL string    `json:"ticket_url,omitempty"`

	CreatedBy      uuid.UUID  `json:"created_by"`
	ClaimedBy      *uuid.UUID `json:"claimed_by,omitempty"`
	ClaimedAt      string     `json:"claimed_at,omitempty"`
	ClaimExpiresAt string     `json:"claim_expires_at,omitempty"`
	ClaimedByYou   bool       `json:"claimed_by_you"`
	ClaimLapsed    bool       `json:"claim_lapsed"`
	ProposedAt     string     `json:"proposed_at,omitempty"`
	DecidedBy      *uuid.UUID `json:"decided_by,omitempty"`
	DecidedAt      string     `json:"decided_at,omitempty"`
	// ResultingPlanID is the draft plan the proposal came with; absent when
	// it brought none or the plan was deleted since.
	ResultingPlanID *uuid.UUID `json:"resulting_plan_id,omitempty"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
}

func rfc3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func ideaFrom(i ideas.Idea, clientID *uuid.UUID, now time.Time) ideaResponse {
	return ideaResponse{
		ID: i.ID, Status: i.Status, Priority: i.Priority, Position: i.Position, Version: i.Version,
		Title: i.Title, Body: i.Body, Proposal: i.Proposal, Rejection: i.Rejection,
		Branch: i.Branch, TicketKey: i.TicketKey, TicketURL: i.TicketURL,
		CreatedBy: i.CreatedBy, ClaimedBy: i.ClaimedBy, ClaimedAt: rfc3339(i.ClaimedAt), ClaimExpiresAt: rfc3339(i.ClaimExpiresAt),
		ClaimedByYou: i.Status == ideas.StatusClaimed && i.HeldBy(clientID), ClaimLapsed: i.Lapsed(now),
		ProposedAt: rfc3339(i.ProposedAt), DecidedBy: i.DecidedBy, DecidedAt: rfc3339(i.DecidedAt),
		ResultingPlanID: i.ResultingPlanID,
		CreatedAt:       i.CreatedAt.Format(time.RFC3339), UpdatedAt: i.UpdatedAt.Format(time.RFC3339),
	}
}

// ideaEnvelope is one idea with the names of the people it mentions.
type ideaEnvelope struct {
	ideaResponse
	People map[string]personResponse `json:"people"`
}

func (d Deps) people(ctx context.Context, projectID uuid.UUID, list []ideas.Idea) map[string]personResponse {
	// Names are decoration on a read that already succeeded; failing to
	// resolve them shows ids rather than refusing the idea.
	people, err := d.Ideas.People(ctx, principalIDFrom(ctx), projectID, list)
	if err != nil {
		return map[string]personResponse{}
	}
	return peopleFrom(people)
}

func (d Deps) writeIdea(w http.ResponseWriter, r *http.Request, status int, i ideas.Idea) {
	project := projectFrom(r.Context())
	writeJSON(w, status, ideaEnvelope{
		ideaResponse: ideaFrom(i, clientIDFrom(r.Context()), d.Ideas.Now()),
		People:       d.people(r.Context(), project.ID, []ideas.Idea{i}),
	})
}

func (d Deps) createIdea(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     string `json:"title"`
		Body      string `json:"body"`
		Priority  string `json:"priority"`
		Branch    string `json:"branch"`
		TicketKey string `json:"ticket_key"`
		TicketURL string `json:"ticket_url"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	project := projectFrom(r.Context())
	i, err := d.Ideas.Create(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), project.ID, ideas.NewIdea{
		Title: req.Title, Body: req.Body, Priority: req.Priority,
		Branch: req.Branch, TicketKey: req.TicketKey, TicketURL: req.TicketURL,
	})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	d.writeIdea(w, r, http.StatusCreated, i)
}

// listIdeas takes repeated ?status=; without one it lists every idea, most
// recently changed first. ?status=open (optionally with claimed) is the
// queue, in the order claim-next takes it.
func (d Deps) listIdeas(w http.ResponseWriter, r *http.Request) {
	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	project := projectFrom(r.Context())
	page, err := d.Ideas.List(r.Context(), principalIDFrom(r.Context()), project.ID, r.URL.Query()["status"], opts)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := struct {
		Items      []ideaResponse            `json:"items"`
		NextCursor string                    `json:"next_cursor,omitempty"`
		People     map[string]personResponse `json:"people"`
	}{Items: []ideaResponse{}, NextCursor: page.NextCursor, People: d.people(r.Context(), project.ID, page.Items)}
	now, clientID := d.Ideas.Now(), clientIDFrom(r.Context())
	for _, i := range page.Items {
		out.Items = append(out.Items, ideaFrom(i, clientID, now))
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) getIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "ideaID")
	if !ok {
		return
	}
	i, err := d.Ideas.Get(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	d.writeIdea(w, r, http.StatusOK, i)
}

// updateIdea reprioritises or moves an open idea.
func (d Deps) updateIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "ideaID")
	if !ok {
		return
	}
	var req struct {
		Priority *string `json:"priority"`
		Position *int    `json:"position"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	i, err := d.Ideas.Update(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id,
		ideas.Update{Priority: req.Priority, Position: req.Position})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	d.writeIdea(w, r, http.StatusOK, i)
}

// claimNextIdea answers 204 when nothing is waiting: an empty queue is not
// an error, and there is no idea to return.
func (d Deps) claimNextIdea(w http.ResponseWriter, r *http.Request) {
	i, found, _, err := d.Ideas.ClaimNext(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), projectFrom(r.Context()).ID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	d.writeIdea(w, r, http.StatusOK, i)
}

// ideaAction serves the transitions that take nothing but the idea.
func (d Deps) ideaAction(act func(s *ideas.Service, ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, ideaID uuid.UUID) (ideas.Idea, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := uuidParam(w, r, "ideaID")
		if !ok {
			return
		}
		i, err := act(d.Ideas, r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), projectFrom(r.Context()).ID, id)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		d.writeIdea(w, r, http.StatusOK, i)
	}
}

func (d Deps) proposeIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "ideaID")
	if !ok {
		return
	}
	var req struct {
		Proposal string `json:"proposal"`
		Plan     *struct {
			PhaseID *uuid.UUID       `json:"phase_id"`
			Title   string           `json:"title"`
			Body    string           `json:"body"`
			Items   []newItemRequest `json:"items"`
		} `json:"plan"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	var plan *ideas.DraftPlan
	if req.Plan != nil {
		plan = &ideas.DraftPlan{PhaseID: req.Plan.PhaseID, Title: req.Plan.Title, Body: req.Plan.Body}
		for _, it := range req.Plan.Items {
			plan.Items = append(plan.Items, ideas.PlanItem{Text: it.Text, Kind: it.Kind})
		}
	}
	i, err := d.Ideas.Propose(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), projectFrom(r.Context()).ID, id, req.Proposal, plan)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	d.writeIdea(w, r, http.StatusOK, i)
}

func (d Deps) rejectIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "ideaID")
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
		// Reopen sends the idea back to the queue for another proposal
		// instead of rejecting it for good.
		Reopen bool `json:"reopen"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	i, err := d.Ideas.Reject(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), projectFrom(r.Context()).ID, id, req.Reason, req.Reopen)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	d.writeIdea(w, r, http.StatusOK, i)
}
