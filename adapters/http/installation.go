package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/identity"
)

// Installation-admin endpoints (docs/adr/0017) — every handler here
// resolves the caller's userID (resolveCallerUserID, defined in
// organizations.go and reused as-is: installation_admins is keyed by User,
// same reasoning as organization_members) and lets d.Installation's own
// capability check decide ALLOW/DENY. There is deliberately no endpoint to
// grant/revoke installation-admin rights — that stays a
// `ccd installation grant-admin` operator/--database-url-only action, so
// there is no HTTP path by which one admin can mint another.

type userResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

func (d Deps) createInstallationUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil || body.Email == "" || body.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "email and display_name are required")
		return
	}

	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	u, err := d.Installation.CreateUser(r.Context(), principalID, callerUserID, body.Email, body.DisplayName)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, userResponse{ID: u.ID.String(), Email: u.Email, DisplayName: u.DisplayName, CreatedAt: u.CreatedAt.Format(time.RFC3339)})
}

type servicePrincipalResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
}

func (d Deps) createServicePrincipal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}

	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	sp, err := d.Installation.CreateServicePrincipal(r.Context(), principalID, callerUserID, body.Name, body.Description)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, servicePrincipalResponse{
		ID: sp.ID.String(), Name: sp.Name, Description: sp.Description, CreatedAt: sp.CreatedAt.Format(time.RFC3339),
	})
}

type issueCredentialForRequest struct {
	ClientType       string `json:"client_type"`
	ExpiresInSeconds *int64 `json:"expires_in_seconds"`
}

// issueCredentialFor mirrors adapters/http/tokens.go's issueToken exactly,
// except the target principal comes from the URL (chi "id"), not
// principalIDFrom(ctx) — the whole point of this endpoint.
func (d Deps) issueCredentialFor(w http.ResponseWriter, r *http.Request) {
	targetPrincipalID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "id must be a UUID")
		return
	}
	var req issueCredentialForRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}
	if req.ClientType == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "client_type is required")
		return
	}
	var expiresAt *time.Time
	if req.ExpiresInSeconds != nil {
		t := time.Now().Add(time.Duration(*req.ExpiresInSeconds) * time.Second)
		expiresAt = &t
	}
	if err := identity.ValidateExpiresAt(expiresAt); err != nil {
		writeDomainError(w, err)
		return
	}

	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	token, cred, err := d.Installation.IssueCredentialFor(r.Context(), principalID, callerUserID, targetPrincipalID, req.ClientType, expiresAt)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	resp := issueTokenResponse{Token: token, CredentialID: cred.ID.String(), ClientType: cred.ClientType}
	if cred.ExpiresAt != nil {
		s := cred.ExpiresAt.Format(time.RFC3339)
		resp.ExpiresAt = &s
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (d Deps) listCredentialsFor(w http.ResponseWriter, r *http.Request) {
	targetPrincipalID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "id must be a UUID")
		return
	}

	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	creds, err := d.Installation.ListCredentialsFor(r.Context(), principalID, callerUserID, targetPrincipalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]credentialResponse, len(creds))
	for i, c := range creds {
		out[i] = toCredentialResponse(c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) revokeCredentialFor(w http.ResponseWriter, r *http.Request) {
	credentialID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "id must be a UUID")
		return
	}

	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	if err := d.Installation.RevokeCredentialFor(r.Context(), principalID, callerUserID, credentialID); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type installationAdminResponse struct {
	UserID    string  `json:"user_id"`
	Role      string  `json:"role"`
	GrantedBy *string `json:"granted_by,omitempty"`
	GrantedAt string  `json:"granted_at"`
}

func (d Deps) listInstallationAdmins(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	admins, err := d.Installation.ListAdmins(r.Context(), principalID, callerUserID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]installationAdminResponse, len(admins))
	for i, a := range admins {
		resp := installationAdminResponse{UserID: a.UserID.String(), Role: string(a.Role), GrantedAt: a.GrantedAt.Format(time.RFC3339)}
		if a.GrantedBy != nil {
			s := a.GrantedBy.String()
			resp.GrantedBy = &s
		}
		out[i] = resp
	}
	writeJSON(w, http.StatusOK, out)
}
