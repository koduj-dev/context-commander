package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/paging"
)

type auditRecordResponse struct {
	ID           int64   `json:"id"`
	Timestamp    string  `json:"timestamp"`
	Action       string  `json:"action"`
	ResourceType string  `json:"resource_type"`
	ResourceID   *string `json:"resource_id,omitempty"`
	Result       string  `json:"result"`
	PrincipalID  *string `json:"principal_id,omitempty"`
	ActorEmail   *string `json:"actor_email,omitempty"`
}

// auditQueryOptionsFrom parses the filters both audit endpoints accept.
// Shared rather than duplicated: a project's trail and an organization's
// differ in what they are scoped to, never in how they are filtered or
// paged, and two copies would drift the moment one gained a filter.
func auditQueryOptionsFrom(w http.ResponseWriter, r *http.Request) (audit.QueryOptions, bool) {
	opts := audit.QueryOptions{
		Action:       r.URL.Query().Get("action"),
		ResourceType: r.URL.Query().Get("resource_type"),
	}
	if raw := r.URL.Query().Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?since= must be RFC3339")
			return audit.QueryOptions{}, false
		}
		opts.Since = &t
	}
	if raw := r.URL.Query().Get("until"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?until= must be RFC3339")
			return audit.QueryOptions{}, false
		}
		opts.Until = &t
	}
	pagingOpts, ok := pagingFrom(w, r)
	if !ok {
		return audit.QueryOptions{}, false
	}
	opts.Paging = pagingOpts
	return opts, true
}

// writeAuditPage renders records and batch-resolves the page's distinct
// actor principal ids to emails in one query rather than N — the audit UI
// needs "who did this," which the domain Record carries as a principal id
// and a reader cannot turn into a person.
func (d Deps) writeAuditPage(w http.ResponseWriter, r *http.Request, principalID uuid.UUID, page paging.Page[audit.Record]) {
	seen := make(map[uuid.UUID]bool)
	var principalIDs []uuid.UUID
	for _, rec := range page.Items {
		if rec.PrincipalID != nil && !seen[*rec.PrincipalID] {
			seen[*rec.PrincipalID] = true
			principalIDs = append(principalIDs, *rec.PrincipalID)
		}
	}
	var actors map[uuid.UUID]identity.User
	if len(principalIDs) > 0 {
		err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			var err error
			actors, err = identity.NewStore().GetUsersForPrincipals(r.Context(), tx, principalIDs)
			return err
		})
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
	}

	out := make([]auditRecordResponse, len(page.Items))
	for i, rec := range page.Items {
		out[i] = auditRecordResponse{
			ID: rec.ID, Timestamp: rec.Timestamp.Format(time.RFC3339),
			Action: rec.Action, ResourceType: rec.ResourceType, Result: string(rec.Result),
		}
		if rec.ResourceID != nil {
			id := rec.ResourceID.String()
			out[i].ResourceID = &id
		}
		if rec.PrincipalID != nil {
			id := rec.PrincipalID.String()
			out[i].PrincipalID = &id
			if actor, ok := actors[*rec.PrincipalID]; ok {
				out[i].ActorEmail = &actor.Email
			}
		}
	}
	writeList(w, out, page.NextCursor)
}

func (d Deps) queryAudit(w http.ResponseWriter, r *http.Request) {
	opts, ok := auditQueryOptionsFrom(w, r)
	if !ok {
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	page, err := d.Audit.Query(r.Context(), principalID, project.ID, opts)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	d.writeAuditPage(w, r, principalID, page)
}

// queryOrganizationAudit serves the organization's own trail — membership
// changes and whatever later joins them (migrations/000023).
//
// Gated on CapOrgAuditRead, which a plain organization MEMBER does not
// hold: seeing who is in an organization and seeing who put them there are
// different questions, and the second is the more sensitive one. That is
// the same line auth.CapAuditRead draws for projects.
func (d Deps) queryOrganizationAudit(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid organization id")
		return
	}
	opts, ok := auditQueryOptionsFrom(w, r)
	if !ok {
		return
	}

	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}

	page, err := d.Organizations.QueryAudit(r.Context(), principalID, callerUserID, orgID, opts)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	d.writeAuditPage(w, r, principalID, page)
}
