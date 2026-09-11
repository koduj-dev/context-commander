package http

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
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

func (d Deps) queryAudit(w http.ResponseWriter, r *http.Request) {
	opts := audit.QueryOptions{
		Action:       r.URL.Query().Get("action"),
		ResourceType: r.URL.Query().Get("resource_type"),
	}
	if raw := r.URL.Query().Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?since= must be RFC3339")
			return
		}
		opts.Since = &t
	}
	if raw := r.URL.Query().Get("until"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?until= must be RFC3339")
			return
		}
		opts.Until = &t
	}
	pagingOpts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	opts.Paging = pagingOpts

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	page, err := d.Audit.Query(r.Context(), principalID, project.ID, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// Batch-resolve the page's distinct actor principal ids to emails in one
	// query rather than N — the audit UI needs "who did this," which the
	// domain Record already carries (PrincipalID) but this response used to
	// drop entirely.
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
			writeDomainError(w, err)
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
