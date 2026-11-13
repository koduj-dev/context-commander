package http

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/plans"
)

// The usage ledger, summed (docs/adr/0027). GET .../usage?from=&to=&group=
// with from and to as YYYY-MM-DD UTC days, both inclusive (default: the last
// 30 days), and group a comma-separated list of project, day, principal,
// model, branch, ticket.

type usageRowResponse struct {
	ProjectID     *uuid.UUID `json:"project_id,omitempty"`
	ProjectPath   string     `json:"project_path,omitempty"`
	Day           string     `json:"day,omitempty"`
	PrincipalID   *uuid.UUID `json:"principal_id,omitempty"`
	PrincipalKind string     `json:"principal_kind,omitempty"`
	PrincipalName string     `json:"principal_name,omitempty"`
	// Model, Branch and TicketKey are present when grouped by them; "" means
	// "not stated", which is a group of its own.
	Model            *string  `json:"model,omitempty"`
	Branch           *string  `json:"branch,omitempty"`
	TicketKey        *string  `json:"ticket_key,omitempty"`
	Seconds          int64    `json:"seconds"`
	TokensIn         int64    `json:"tokens_in"`
	TokensOut        int64    `json:"tokens_out"`
	CacheReadTokens  int64    `json:"cache_read_tokens"`
	CacheWriteTokens int64    `json:"cache_write_tokens"`
	Turns            int64    `json:"turns"`
	Cost             *float64 `json:"cost,omitempty"`
}

type usageResponse struct {
	From      string             `json:"from"`
	To        string             `json:"to"`
	Group     []string           `json:"group"`
	Rows      []usageRowResponse `json:"rows"`
	Total     usageRowResponse   `json:"total"`
	Currency  string             `json:"currency,omitempty"`
	AsOf      string             `json:"as_of,omitempty"`
	Unpriced  []string           `json:"unpriced,omitempty"`
	Truncated bool               `json:"truncated"`
}

func usageRowFrom(r plans.UsageRow) usageRowResponse {
	out := usageRowResponse{
		ProjectID: r.ProjectID, ProjectPath: r.ProjectPath,
		PrincipalID: r.PrincipalID, PrincipalKind: r.PrincipalKind, PrincipalName: r.PrincipalName,
		Model: r.Model, Branch: r.Branch, TicketKey: r.TicketKey,
		Seconds: r.Seconds, TokensIn: r.Tokens.TokensIn, TokensOut: r.Tokens.TokensOut,
		CacheReadTokens: r.Tokens.CacheReadTokens, CacheWriteTokens: r.Tokens.CacheWriteTokens,
		Turns: r.Turns, Cost: r.Cost,
	}
	if r.Day != nil {
		out.Day = r.Day.Format(time.DateOnly)
	}
	return out
}

// usageQueryFrom reads from, to and group.
func usageQueryFrom(w http.ResponseWriter, r *http.Request) (plans.UsageQuery, bool) {
	q, err := plans.ParseUsageQuery(r.URL.Query().Get("from"), r.URL.Query().Get("to"), r.URL.Query().Get("group"), time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return plans.UsageQuery{}, false
	}
	return q, true
}

func writeUsage(w http.ResponseWriter, q plans.UsageQuery, s plans.UsageSummary) {
	out := usageResponse{
		From: q.From.Format(time.DateOnly), To: q.To.Format(time.DateOnly), Group: q.GroupBy,
		Rows: make([]usageRowResponse, len(s.Rows)), Total: usageRowFrom(s.Total),
		Currency: s.Currency, AsOf: s.AsOf, Unpriced: s.Unpriced, Truncated: s.Truncated,
	}
	if out.Group == nil {
		out.Group = []string{}
	}
	for i, row := range s.Rows {
		out.Rows[i] = usageRowFrom(row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) projectUsage(w http.ResponseWriter, r *http.Request) {
	q, ok := usageQueryFrom(w, r)
	if !ok {
		return
	}
	id := projectFrom(r.Context()).ID
	q.ProjectID = &id
	s, err := d.Plans.Usage(r.Context(), principalIDFrom(r.Context()), q)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeUsage(w, q, s)
}

// myUsage sums every project the caller can read plans in.
func (d Deps) myUsage(w http.ResponseWriter, r *http.Request) {
	q, ok := usageQueryFrom(w, r)
	if !ok {
		return
	}
	s, err := d.Plans.Usage(r.Context(), principalIDFrom(r.Context()), q)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeUsage(w, q, s)
}
