package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/handoffs"
)

type handoffResponse struct {
	ID              uuid.UUID  `json:"id"`
	SourceSessionID *uuid.UUID `json:"source_session_id,omitempty"`
	Status          string     `json:"status"`
	CreatedAt       string     `json:"created_at"`

	// Lifecycle attribution, omitted while the handoff is still open —
	// a field that is absent says "this hasn't happened" more clearly than
	// one carrying a zero timestamp.
	// The client ids travel alongside the principal ids, not instead of
	// them: two clients may act as the same principal, so "who claimed
	// this" is only answerable at client granularity.
	ClaimedAt          string     `json:"claimed_at,omitempty"`
	ClaimedByPrincipal *uuid.UUID `json:"claimed_by_principal,omitempty"`
	ClaimedByClient    *uuid.UUID `json:"claimed_by_client,omitempty"`
	ClosedAt           string     `json:"closed_at,omitempty"`
	ClosedByPrincipal  *uuid.UUID `json:"closed_by_principal,omitempty"`
	ClosedByClient     *uuid.UUID `json:"closed_by_client,omitempty"`
}

type handoffDetailResponse struct {
	handoffResponse
	Summary             string      `json:"summary"`
	CompletedWork       string      `json:"completed_work"`
	NextActions         string      `json:"next_actions"`
	ReferencedArtifacts []uuid.UUID `json:"referenced_artifacts,omitempty"`
	ReferencedTasks     []uuid.UUID `json:"referenced_tasks,omitempty"`
}

func toHandoffResponse(h handoffs.Handoff) handoffResponse {
	r := handoffResponse{
		ID: h.ID, SourceSessionID: h.SourceSessionID,
		Status: h.Status, CreatedAt: h.CreatedAt.Format(time.RFC3339),
	}
	if h.ClaimedAt != nil {
		r.ClaimedAt = h.ClaimedAt.Format(time.RFC3339)
		r.ClaimedByPrincipal = h.ClaimedByPrincipal
		r.ClaimedByClient = h.ClaimedByClient
	}
	if h.ClosedAt != nil {
		r.ClosedAt = h.ClosedAt.Format(time.RFC3339)
		r.ClosedByPrincipal = h.ClosedByPrincipal
		r.ClosedByClient = h.ClosedByClient
	}
	return r
}

type createHandoffRequest struct {
	SourceSessionID     *uuid.UUID  `json:"source_session_id,omitempty"`
	Summary             string      `json:"summary"`
	CompletedWork       string      `json:"completed_work"`
	NextActions         string      `json:"next_actions"`
	ReferencedArtifacts []uuid.UUID `json:"referenced_artifacts,omitempty"`
	ReferencedTasks     []uuid.UUID `json:"referenced_tasks,omitempty"`
}

func (d Deps) createHandoff(w http.ResponseWriter, r *http.Request) {
	var req createHandoffRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())
	clientID := clientIDFrom(r.Context())

	h, err := d.Handoffs.Create(r.Context(), principalID, clientID, project.ID, req.SourceSessionID, handoffs.Body{
		Summary:             req.Summary,
		CompletedWork:       req.CompletedWork,
		NextActions:         req.NextActions,
		ReferencedArtifacts: req.ReferencedArtifacts,
		ReferencedTasks:     req.ReferencedTasks,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toHandoffResponse(h))
}

func (d Deps) getHandoff(w http.ResponseWriter, r *http.Request) {
	handoffID, err := uuid.Parse(chi.URLParam(r, "handoffID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "handoffID must be a UUID")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	h, body, err := d.Handoffs.Get(r.Context(), principalID, project.ID, handoffID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, handoffDetailResponse{
		handoffResponse:     toHandoffResponse(h),
		Summary:             body.Summary,
		CompletedWork:       body.CompletedWork,
		NextActions:         body.NextActions,
		ReferencedArtifacts: body.ReferencedArtifacts,
		ReferencedTasks:     body.ReferencedTasks,
	})
}

func (d Deps) listHandoffs(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	var statusFilter *string
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = &s
	}

	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	page, err := d.Handoffs.List(r.Context(), principalID, project.ID, statusFilter, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]handoffResponse, len(page.Items))
	for i, h := range page.Items {
		out[i] = toHandoffResponse(h)
	}
	writeList(w, out, page.NextCursor)
}

// claimHandoff and closeHandoff are POSTs on a sub-resource rather than a
// PATCH of a "status" field. The transitions are not interchangeable
// assignments: each has its own precondition, its own audit action, and its
// own failure that the caller must be able to tell apart (claiming
// something another client already took is a 409, not a bad request). A
// generic status write would collapse all of that into one handler that has
// to re-derive which transition was meant.
func (d Deps) claimHandoff(w http.ResponseWriter, r *http.Request) {
	d.transitionHandoff(w, r, handoffs.StatusClaimed)
}

func (d Deps) closeHandoff(w http.ResponseWriter, r *http.Request) {
	d.transitionHandoff(w, r, handoffs.StatusClosed)
}

func (d Deps) transitionHandoff(w http.ResponseWriter, r *http.Request, target string) {
	handoffID, err := uuid.Parse(chi.URLParam(r, "handoffID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "handoffID must be a UUID")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())
	clientID := clientIDFrom(r.Context())

	var h handoffs.Handoff
	if target == handoffs.StatusClaimed {
		h, err = d.Handoffs.Claim(r.Context(), principalID, clientID, project.ID, handoffID)
	} else {
		h, err = d.Handoffs.Close(r.Context(), principalID, clientID, project.ID, handoffID)
	}
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toHandoffResponse(h))
}

// getCurrentHandoff answers "what am I supposed to continue?" — the
// question an agent handed a project and a token cannot answer from a list
// of UUIDs. 404 when everything is closed: "nothing to take over" is a real
// answer, and the handoff history is still one List away.
func (d Deps) getCurrentHandoff(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	h, err := d.Handoffs.Current(r.Context(), principalID, project.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	// The body, not just the metadata: a caller asking for the current
	// handoff wants to read it, and making that two round trips would be a
	// pointless second call every single time.
	_, body, err := d.Handoffs.Get(r.Context(), principalID, project.ID, h.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, handoffDetailResponse{
		handoffResponse:     toHandoffResponse(h),
		Summary:             body.Summary,
		CompletedWork:       body.CompletedWork,
		NextActions:         body.NextActions,
		ReferencedArtifacts: body.ReferencedArtifacts,
		ReferencedTasks:     body.ReferencedTasks,
	})
}
