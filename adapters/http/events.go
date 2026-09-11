package http

import (
	"net/http"
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
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	page, err := d.Events.List(r.Context(), principalID, project.ID, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	out := make([]eventResponse, len(page.Items))
	for i, e := range page.Items {
		out[i] = eventResponse{
			Seq: e.Seq, EventID: e.EventID.String(), Type: e.Type,
			ResourceType: e.ResourceType, PayloadKind: string(e.PayloadKind),
		}
		if e.ResourceID != nil {
			id := e.ResourceID.String()
			out[i].ResourceID = &id
		}
	}
	writeList(w, out, page.NextCursor)
}
