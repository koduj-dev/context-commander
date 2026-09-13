package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/organizations"
)

type organizationResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type membershipResponse struct {
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id"`
	Role           string `json:"role"`
}

// resolveCallerUserID resolves the human User behind the authenticated
// principal — organization_members is keyed by User, never Principal
// (docs/adr/0006: service principals are never organization members), so
// every organizations handler needs this in addition to
// principalIDFrom(ctx).
func resolveCallerUserID(r *http.Request, pool *db.Pool, principalID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := db.WithPrincipal(r.Context(), pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		p, err := identity.NewStore().GetPrincipal(r.Context(), tx, principalID)
		if err != nil {
			return err
		}
		if p.UserID == nil {
			return organizations.ErrForbidden
		}
		userID = *p.UserID
		return nil
	})
	return userID, err
}

func (d Deps) createOrganization(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}

	principalID := principalIDFrom(r.Context())
	userID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	org, err := d.Organizations.Create(r.Context(), principalID, userID, body.Name)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, organizationResponse{ID: org.ID.String(), Name: org.Name, CreatedAt: org.CreatedAt.Format(time.RFC3339)})
}

// addOrganizationMember expects a chi route "id" URL param — see the
// required http.go route wiring in the phase-3 integration notes.
func (d Deps) addOrganizationMember(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid organization id")
		return
	}
	var body struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid body")
		return
	}
	newUserID, err := uuid.Parse(body.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid user_id")
		return
	}

	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	m, err := d.Organizations.AddMember(r.Context(), principalID, callerUserID, orgID, newUserID, organizations.Role(body.Role))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, membershipResponse{OrganizationID: m.OrganizationID.String(), UserID: m.UserID.String(), Role: string(m.Role)})
}

type organizationMemberResponse struct {
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// listOrganizationMembers is the counterpart projects had and organizations
// did not: members could be added and never enumerated, so "who is in this
// organization" was a question only SQL could answer.
func (d Deps) listOrganizationMembers(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid organization id")
		return
	}

	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	members, err := d.Organizations.ListMembers(r.Context(), principalID, callerUserID, orgID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]organizationMemberResponse, len(members))
	for i, m := range members {
		out[i] = organizationMemberResponse{
			UserID: m.UserID.String(), Role: string(m.Role), CreatedAt: m.CreatedAt.Format(time.RFC3339),
		}
	}
	// A bounded set, like a project's member list: no cursor
	// (docs/architecture-cookbook.md §22.1 covers growing lists).
	writeJSON(w, http.StatusOK, out)
}
