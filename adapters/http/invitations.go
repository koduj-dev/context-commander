package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/members"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// Project invitations (docs/adr/0030).

type invitationResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	InvitedBy string `json:"invited_by"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

func invitationFrom(i members.Invitation) invitationResponse {
	return invitationResponse{
		ID: i.ID.String(), Email: i.Email, Role: string(i.Role), InvitedBy: i.InvitedBy.String(),
		CreatedAt: i.CreatedAt.Format(time.RFC3339), ExpiresAt: i.ExpiresAt.Format(time.RFC3339),
	}
}

// createInvitation takes {"email": ..., "role": ..., "expires_in_days": n}.
// The answer is the same whether or not the address has an account here.
func (d Deps) createInvitation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email         string `json:"email"`
		Role          string `json:"role"`
		ExpiresInDays int    `json:"expires_in_days"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	if req.ExpiresInDays < 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "expires_in_days cannot be negative")
		return
	}
	inv, err := d.Members.Invite(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID,
		req.Email, projects.Role(req.Role), time.Duration(req.ExpiresInDays)*24*time.Hour)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, invitationFrom(inv))
}

func (d Deps) listInvitations(w http.ResponseWriter, r *http.Request) {
	list, err := d.Members.Invitations(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]invitationResponse, len(list))
	for i, inv := range list {
		out[i] = invitationFrom(inv)
	}
	writeList(w, out, "")
}

func (d Deps) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "invitationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invitation id must be a UUID")
		return
	}
	if err := d.Members.RevokeInvitation(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
