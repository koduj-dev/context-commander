package http

import (
	"encoding/json"
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
	return handoffResponse{
		ID: h.ID, SourceSessionID: h.SourceSessionID,
		Status: h.Status, CreatedAt: h.CreatedAt.Format(time.RFC3339),
	}
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	list, err := d.Handoffs.List(r.Context(), principalID, project.ID, statusFilter)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]handoffResponse, len(list))
	for i, h := range list {
		out[i] = toHandoffResponse(h)
	}
	writeJSON(w, http.StatusOK, out)
}
