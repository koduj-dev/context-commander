package http

import (
	"net/http"
	"strconv"
)

type eventResponse struct {
	Seq          int64   `json:"seq"`
	EventID      string  `json:"event_id"`
	Type         string  `json:"type"`
	ResourceType string  `json:"resource_type"`
	ResourceID   *string `json:"resource_id,omitempty"`
	PayloadKind  string  `json:"payload_kind"`
}

func (d Deps) listEvents(w http.ResponseWriter, r *http.Request) {
	var afterSeq int64
	if raw := r.URL.Query().Get("after_seq"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?after_seq= must be an integer")
			return
		}
		afterSeq = v
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?limit= must be an integer")
			return
		}
		limit = v
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	list, err := d.Events.List(r.Context(), principalID, project.ID, afterSeq, limit)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	out := make([]eventResponse, len(list))
	for i, e := range list {
		out[i] = eventResponse{
			Seq: e.Seq, EventID: e.EventID.String(), Type: e.Type,
			ResourceType: e.ResourceType, PayloadKind: string(e.PayloadKind),
		}
		if e.ResourceID != nil {
			id := e.ResourceID.String()
			out[i].ResourceID = &id
		}
	}
	writeJSON(w, http.StatusOK, out)
}
