package http

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
)

type meResponse struct {
	PrincipalID         string `json:"principal_id"`
	Kind                string `json:"kind"` // "user" | "service"
	Email               string `json:"email,omitempty"`
	DisplayName         string `json:"display_name,omitempty"`
	Name                string `json:"name,omitempty"` // set for a ServicePrincipal instead of email/display_name
	IsInstallationAdmin bool   `json:"is_installation_admin"`
}

// getMe resolves the authenticated caller's own identity — the web UI's
// login flow uses this to validate a pasted PAT and to render "signed in
// as ...". There is no dedicated session/login endpoint (docs/adr/0009: PAT
// issuance is CLI-only); this is the read-only counterpart every adapter
// still needed and none had. IsInstallationAdmin is the web UI's only way
// to decide whether to show the Administration nav entry at all —
// docs/adr/0017 deliberately has no cheaper way to ask "am I an admin"
// than this.
func (d Deps) getMe(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())

	var resp meResponse
	var callerUserID *uuid.UUID
	err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		identityStore := identity.NewStore()
		p, err := identityStore.GetPrincipal(r.Context(), tx, principalID)
		if err != nil {
			return err
		}
		resp.PrincipalID = p.ID.String()
		resp.Kind = string(p.Kind)

		switch p.Kind {
		case identity.PrincipalKindUser:
			u, err := identityStore.GetUser(r.Context(), tx, *p.UserID)
			if err != nil {
				return err
			}
			resp.Email = u.Email
			resp.DisplayName = u.DisplayName
			callerUserID = p.UserID
		case identity.PrincipalKindService:
			sp, err := identityStore.GetServicePrincipal(r.Context(), tx, *p.ServicePrincipalID)
			if err != nil {
				return err
			}
			resp.Name = sp.Name
		}
		return nil
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// A second, separate transaction — not nested inside the one above,
	// which risks pool exhaustion/deadlock under a small connection pool
	// (same reasoning as internal/skills/internal/gates' checkScopeTarget
	// calling installation.Store directly instead of going through
	// Service inside an already-open tx).
	if callerUserID != nil {
		isAdmin, err := d.Installation.IsAdmin(r.Context(), principalID, *callerUserID)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		resp.IsInstallationAdmin = isAdmin
	}

	writeJSON(w, http.StatusOK, resp)
}

type myProjectResponse struct {
	Namespace  string `json:"namespace"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	Role       string `json:"role"`
}

// listMyProjects lists every project the caller is a member of — the web
// UI's dashboard project picker. No dedicated "list projects" endpoint
// existed before; every other project route requires already knowing a
// namespace/slug.
func (d Deps) listMyProjects(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())

	var list []myProjectResponse
	err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		memberships, err := d.Projects.ListForPrincipal(r.Context(), tx, principalID)
		if err != nil {
			return err
		}
		list = make([]myProjectResponse, len(memberships))
		for i, m := range memberships {
			list[i] = myProjectResponse{
				Namespace: m.NamespaceSlug, Slug: m.Project.Slug, Name: m.Project.Name,
				Visibility: string(m.Project.Visibility), Role: string(m.Role),
			}
		}
		return nil
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type myOrganizationResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// listMyOrganizations lists every organization the caller belongs to — the
// web UI's dashboard org picker. organization_members is keyed by User, not
// Principal (docs/adr/0006), so a ServicePrincipal caller trivially has none
// — that's reported as an empty list, not an error, since "no orgs" is the
// correct answer for that caller, not a forbidden request.
func (d Deps) listMyOrganizations(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())

	var userID *uuid.UUID
	err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		p, err := identity.NewStore().GetPrincipal(r.Context(), tx, principalID)
		if err != nil {
			return err
		}
		userID = p.UserID
		return nil
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if userID == nil {
		writeJSON(w, http.StatusOK, []myOrganizationResponse{})
		return
	}

	memberships, err := d.Organizations.ListMine(r.Context(), principalID, *userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]myOrganizationResponse, len(memberships))
	for i, m := range memberships {
		out[i] = myOrganizationResponse{ID: m.Organization.ID.String(), Name: m.Organization.Name, Role: string(m.Role)}
	}
	writeJSON(w, http.StatusOK, out)
}
