package http

import (
	"errors"
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
	// PrincipalID is who this user ACTS AS, and it is a different id from
	// ID. An organization membership is keyed by the user, a project
	// membership by the principal (docs/adr/0006) — a client that has only
	// one of them can fill in exactly one of those two forms. Absent on
	// create, where the caller has just been handed the user itself.
	PrincipalID string `json:"principal_id,omitempty"`
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
	if errors.Is(err, identity.ErrAlreadyExists) {
		// Said here rather than in writeDomainError because only this
		// handler knows the clash was an email — and "that already exists"
		// leaves someone re-reading their own form to work out what.
		writeError(w, http.StatusConflict, "conflict", "a user with this email address already exists")
		return
	}
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
	// The principal this agent acts as — see userResponse.PrincipalID. It is
	// what a token is issued for and what a project membership names.
	PrincipalID string `json:"principal_id,omitempty"`
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

// listInstallationUsers is the directory every "who is this" control needed
// and none had. Paginated like every other growing list; admin-gated by the
// service, the same as the create endpoints beside it.
func (d Deps) listInstallationUsers(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}

	page, err := d.Installation.ListUsers(r.Context(), principalID, callerUserID, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]userResponse, len(page.Items))
	for i, u := range page.Items {
		out[i] = userResponse{
			ID: u.ID.String(), Email: u.Email, DisplayName: u.DisplayName,
			CreatedAt: u.CreatedAt.Format(time.RFC3339), PrincipalID: u.PrincipalID.String(),
		}
	}
	writeList(w, out, page.NextCursor)
}

func (d Deps) listServicePrincipals(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}

	page, err := d.Installation.ListServicePrincipals(r.Context(), principalID, callerUserID, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]servicePrincipalResponse, len(page.Items))
	for i, a := range page.Items {
		out[i] = servicePrincipalResponse{
			ID: a.ID.String(), Name: a.Name, Description: a.Description,
			CreatedAt: a.CreatedAt.Format(time.RFC3339), PrincipalID: a.PrincipalID.String(),
		}
	}
	writeList(w, out, page.NextCursor)
}
