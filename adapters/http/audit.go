package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/koduj-dev/context-commander/internal/audit"
)

type auditRecordResponse struct {
	ID           int64   `json:"id"`
	Timestamp    string  `json:"timestamp"`
	Action       string  `json:"action"`
	ResourceType string  `json:"resource_type"`
	ResourceID   *string `json:"resource_id,omitempty"`
	Result       string  `json:"result"`
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
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?limit= must be an integer")
			return
		}
		opts.Limit = v
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	list, err := d.Audit.Query(r.Context(), principalID, project.ID, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	out := make([]auditRecordResponse, len(list))
	for i, rec := range list {
		out[i] = auditRecordResponse{
			ID: rec.ID, Timestamp: rec.Timestamp.Format(time.RFC3339),
			Action: rec.Action, ResourceType: rec.ResourceType, Result: string(rec.Result),
		}
		if rec.ResourceID != nil {
			id := rec.ResourceID.String()
			out[i].ResourceID = &id
		}
	}
	writeJSON(w, http.StatusOK, out)
}
