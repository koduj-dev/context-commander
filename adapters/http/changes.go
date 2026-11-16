package http

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/changes"
)

type changeResponse struct {
	Kind   string    `json:"kind"`
	ID     string    `json:"id"`
	Title  string    `json:"title,omitempty"`
	Detail string    `json:"detail"`
	Where  string    `json:"where,omitempty"`
	PlanID string    `json:"plan_id,omitempty"`
	By     string    `json:"by"`
	ByName string    `json:"by_name"`
	At     time.Time `json:"at"`
}

// projectChanges is GET /projects/{ns}/{slug}/changes?since=RFC3339: what
// everyone else did on the project since then (internal/changes). Without
// since it returns nothing and the server's now, which is where a client
// starts. The answer carries `now` for the next call.
func (d Deps) projectChanges(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	since := now
	if raw := r.URL.Query().Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "since must be an RFC 3339 time")
			return
		}
		since = t
	}
	list, err := d.Changes.Since(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, since, now)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := struct {
		Now   time.Time        `json:"now"`
		Items []changeResponse `json:"items"`
	}{Now: now, Items: make([]changeResponse, len(list))}
	for i, c := range list {
		out.Items[i] = toChangeResponse(c)
	}
	writeJSON(w, http.StatusOK, out)
}

func toChangeResponse(c changes.Change) changeResponse {
	r := changeResponse{Kind: c.Kind, ID: c.ID.String(), Title: c.Title, Detail: c.Detail, Where: c.Where,
		By: c.By.String(), ByName: c.ByName, At: c.At}
	if c.PlanID != uuid.Nil {
		r.PlanID = c.PlanID.String()
	}
	return r
}
