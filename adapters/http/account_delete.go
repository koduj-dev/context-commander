package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/identity"
)

type deleteAccountRequest struct {
	Confirm string `json:"confirm"`
}

// deleteOwnAccount is DELETE /me {"confirm": "<email>"}: a person deletes
// their own account (identity.DeleteUser, docs/adr/0042), and the session
// cookie goes with it.
func (d Deps) deleteOwnAccount(w http.ResponseWriter, r *http.Request) {
	var req deleteAccountRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}
	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil || userID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "only a user account can be deleted")
		return
	}
	if err := identity.DeleteUser(r.Context(), d.Pool, principalID, clientIDFrom(r.Context()), userID, req.Confirm); err != nil {
		writeDomainError(w, r, err)
		return
	}
	d.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// deleteInstallationUser is DELETE /installation/users/{id}
// {"confirm": "<their email>"}: an installation administrator deletes
// someone's account.
func (d Deps) deleteInstallationUser(w http.ResponseWriter, r *http.Request) {
	target, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "id must be a UUID")
		return
	}
	var req deleteAccountRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}
	if err := identity.DeleteUser(r.Context(), d.Pool, principalIDFrom(r.Context()), clientIDFrom(r.Context()), target, req.Confirm); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
