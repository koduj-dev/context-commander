package http

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	// Both the role check and the "granting OWNER requires OWNER" rule
	// live in internal/members.Service (docs/adr/0014) — this handler only
	// parses and maps errors.
	if err := d.Members.Add(r.Context(), principalID, project.ID, targetPrincipalID, projects.Role(req.Role)); err != nil {
		writeDomainError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type memberResponse struct {
	PrincipalID string  `json:"principal_id"`
	Role        string  `json:"role"`
	CreatedAt   string  `json:"created_at"`
	Email       *string `json:"email,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
}

// listMembers is the mirror image of addMember: CapMemberRead (unlike
// CapMemberManage) is granted to every role including READER, so any
// project member can see who else is on the project even if they can't add
// or remove anyone. Emails are batch-resolved the same way queryAudit
// resolves actor emails — a service-principal member has no User row and
// so no email/display_name in the response.
func (d Deps) listMembers(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	members, err := d.Members.List(r.Context(), principalID, project.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	principalIDs := make([]uuid.UUID, len(members))
	for i, m := range members {
		principalIDs[i] = m.PrincipalID
	}
	var users map[uuid.UUID]identity.User
	if len(principalIDs) > 0 {
		err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			var err error
			users, err = identity.NewStore().GetUsersForPrincipals(r.Context(), tx, principalIDs)
			return err
		})
		if err != nil {
			writeDomainError(w, err)
			return
		}
	}

	out := make([]memberResponse, len(members))
	for i, m := range members {
		out[i] = memberResponse{PrincipalID: m.PrincipalID.String(), Role: string(m.Role), CreatedAt: m.CreatedAt.Format(time.RFC3339)}
		if u, ok := users[m.PrincipalID]; ok {
			email := u.Email
			out[i].Email = &email
			if u.DisplayName != "" {
				displayName := u.DisplayName
				out[i].DisplayName = &displayName
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func toProjectResponse(p projects.Project, namespaceSlug string) projectResponse {
	return projectResponse{
		ID: p.ID.String(), Namespace: namespaceSlug, Slug: p.Slug, Name: p.Name, Visibility: string(p.Visibility),
	}
}
