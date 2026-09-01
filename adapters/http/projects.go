package http

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type createProjectRequest struct {
	Project string `json:"project"` // "namespace/slug"
	Name    string `json:"name"`
}

type projectResponse struct {
	ID         string `json:"id"`
	Namespace  string `json:"namespace"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

// createProject creates a project and adds the caller as OWNER in the same
// transaction — see docs/adr/0015-project-creation-rls-bootstrap.md: a
// project committed without an immediate self-membership insert would be
// permanently inaccessible to everyone, itself included.
func (d Deps) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}
	namespaceSlug, _, ok := strings.Cut(req.Project, "/")
	if !ok || namespaceSlug == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", `"project" must look like "namespace/slug", and "name" is required`)
		return
	}

	principalID := principalIDFrom(r.Context())
	identityStore := identity.NewStore()

	var project projects.Project
	err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		principal, err := identityStore.GetPrincipal(r.Context(), tx, principalID)
		if err != nil {
			return err
		}
		if principal.UserID == nil {
			return errors.New("only a User principal can create a personal namespace this way")
		}
		project, err = d.Projects.CreateForOwner(r.Context(), tx, *principal.UserID, principalID, req.Project, req.Name)
		return err
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, toProjectResponse(project, namespaceSlug))
}

func (d Deps) getProject(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	writeJSON(w, http.StatusOK, toProjectResponse(project, chi.URLParam(r, "namespace")))
}

type addMemberRequest struct {
	PrincipalID string `json:"principal_id"`
	Role        string `json:"role"`
}

func (d Deps) addMember(w http.ResponseWriter, r *http.Request) {
	var req addMemberRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}
	targetPrincipalID, err := uuid.Parse(req.PrincipalID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "principal_id must be a UUID")
		return
	}
	role := projects.Role(req.Role)
	switch role {
	case projects.RoleOwner, projects.RoleAdmin, projects.RoleContributor, projects.RoleReader:
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "role must be one of OWNER, ADMIN, CONTRIBUTOR, READER")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	allowed, err := d.Authz.Can(r.Context(), principalID, project.ID, auth.CapMemberManage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "authorization check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "forbidden", "you do not have permission to manage members on this project")
		return
	}

	err = db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		_, err := d.Projects.AddMember(r.Context(), tx, project.ID, targetPrincipalID, role)
		return err
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "could not add member: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func toProjectResponse(p projects.Project, namespaceSlug string) projectResponse {
	return projectResponse{
		ID: p.ID.String(), Namespace: namespaceSlug, Slug: p.Slug, Name: p.Name, Visibility: string(p.Visibility),
	}
}
