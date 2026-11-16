package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type deleteProjectRequest struct {
	Confirm string `json:"confirm"`
}

// deleteProject is DELETE /projects/{ns}/{slug}: schedule the project for
// deletion (internal/retention, docs/adr/0036). An OWNER, in the
// interface, confirming with the path. 202: nothing is gone yet, and the
// answer says when it will be.
func (d Deps) deleteProject(w http.ResponseWriter, r *http.Request) {
	var req deleteProjectRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}
	project := projectFrom(r.Context())
	path := chi.URLParam(r, "namespace") + "/" + chi.URLParam(r, "slug")
	purgeAfter, err := d.Retention.RequestDeletion(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), project.ID, path, req.Confirm)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"project": path, "purge_after": purgeAfter})
}

type pendingDeletionResponse struct {
	ID          string    `json:"id"`
	Project     string    `json:"project"`
	Name        string    `json:"name"`
	RequestedAt time.Time `json:"requested_at"`
	RequestedBy string    `json:"requested_by"`
	PurgeAfter  time.Time `json:"purge_after"`
}

// myPendingDeletions is GET /me/projects/pending-deletion: the caller's own
// projects that wait for purge, which nothing else lists any more.
func (d Deps) myPendingDeletions(w http.ResponseWriter, r *http.Request) {
	list, err := d.Retention.PendingDeletions(r.Context(), principalIDFrom(r.Context()))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]pendingDeletionResponse, len(list))
	for i, p := range list {
		out[i] = pendingDeletionResponse{ID: p.ProjectID.String(), Project: p.Path, Name: p.Name,
			RequestedAt: p.RequestedAt, RequestedBy: p.RequestedBy.String(), PurgeAfter: p.PurgeAfter}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// cancelDeletion is POST /me/projects/pending-deletion/{projectID}/cancel.
func (d Deps) cancelDeletion(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "project id must be a UUID")
		return
	}
	if err := d.Retention.CancelDeletion(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), id); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
