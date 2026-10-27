package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/plans"
)

// Plans, phases and presence (docs/adr/0022). The shapes mirror the MCP
// tools' (adapters/mcp/tools_plan.go) with one difference: people travel as
// ids plus a `people` map, because a screen renders a name in many places
// and a model reads it once.

type effortResponse struct {
	Seconds   int64 `json:"seconds"`
	TokensIn  int64 `json:"tokens_in"`
	TokensOut int64 `json:"tokens_out"`
}

func effortFrom(e plans.Effort) effortResponse {
	return effortResponse{Seconds: e.Seconds, TokensIn: e.TokensIn, TokensOut: e.TokensOut}
}

type countsResponse struct {
	Open       int `json:"open"`
	InProgress int `json:"in_progress"`
	Done       int `json:"done"`
	Blocked    int `json:"blocked"`
	Skipped    int `json:"skipped"`
}

func countsFrom(c plans.Counts) countsResponse {
	return countsResponse{Open: c.Open, InProgress: c.InProgress, Done: c.Done, Blocked: c.Blocked, Skipped: c.Skipped}
}

type itemResponse struct {
	ID          uuid.UUID      `json:"id"`
	PlanID      uuid.UUID      `json:"plan_id"`
	Position    int            `json:"position"`
	Kind        string         `json:"kind"`
	Status      string         `json:"status"`
	Text        string         `json:"text"`
	Note        string         `json:"note,omitempty"`
	Assignee    *uuid.UUID     `json:"assignee,omitempty"`
	UpdatedBy   *uuid.UUID     `json:"updated_by,omitempty"`
	UpdatedAt   string         `json:"updated_at"`
	CompletedAt string         `json:"completed_at,omitempty"`
	Effort      effortResponse `json:"effort"`
}

func itemFrom(it plans.Item) itemResponse {
	r := itemResponse{
		ID: it.ID, PlanID: it.PlanID, Position: it.Position, Kind: it.Kind, Status: it.Status, Text: it.Text, Note: it.Note,
		Assignee: it.Assignee, UpdatedBy: it.UpdatedBy, UpdatedAt: it.UpdatedAt.Format(time.RFC3339), Effort: effortFrom(it.Effort),
	}
	if it.CompletedAt != nil {
		r.CompletedAt = it.CompletedAt.Format(time.RFC3339)
	}
	return r
}

type planResponse struct {
	ID        uuid.UUID      `json:"id"`
	PhaseID   *uuid.UUID     `json:"phase_id,omitempty"`
	Status    string         `json:"status"`
	Version   int            `json:"version"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	CreatedBy uuid.UUID      `json:"created_by"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
	Items     []itemResponse `json:"items"`
}

func planFrom(p plans.Plan) planResponse {
	r := planResponse{
		ID: p.ID, PhaseID: p.PhaseID, Status: p.Status, Version: p.Version, Title: p.Title, Body: p.Body,
		CreatedBy: p.CreatedBy, CreatedAt: p.CreatedAt.Format(time.RFC3339), UpdatedAt: p.UpdatedAt.Format(time.RFC3339),
		Items: []itemResponse{},
	}
	for _, it := range p.Items {
		r.Items = append(r.Items, itemFrom(it))
	}
	return r
}

type phaseResponse struct {
	ID       uuid.UUID      `json:"id"`
	Position int            `json:"position"`
	Status   string         `json:"status"`
	Title    string         `json:"title"`
	Goal     string         `json:"goal"`
	Counts   countsResponse `json:"counts"`
	Effort   effortResponse `json:"effort"`
}

func phaseFrom(ph plans.Phase) phaseResponse {
	return phaseResponse{ID: ph.ID, Position: ph.Position, Status: ph.Status, Title: ph.Title, Goal: ph.Goal}
}

type planSummaryResponse struct {
	ID        uuid.UUID      `json:"id"`
	PhaseID   *uuid.UUID     `json:"phase_id,omitempty"`
	Status    string         `json:"status"`
	Title     string         `json:"title"`
	CreatedBy uuid.UUID      `json:"created_by"`
	UpdatedAt string         `json:"updated_at"`
	Counts    countsResponse `json:"counts"`
	Effort    effortResponse `json:"effort"`
}

type activeItemResponse struct {
	PlanTitle string       `json:"plan_title"`
	Item      itemResponse `json:"item"`
}

type presenceResponse struct {
	ClientID    uuid.UUID  `json:"client_id"`
	ClientType  string     `json:"client_type"`
	PrincipalID uuid.UUID  `json:"principal_id"`
	You         bool       `json:"you"`
	Project     string     `json:"project"`
	PlanID      *uuid.UUID `json:"plan_id,omitempty"`
	PlanTitle   string     `json:"plan_title,omitempty"`
	ItemID      *uuid.UUID `json:"item_id,omitempty"`
	ItemText    string     `json:"item_text,omitempty"`
	Activity    string     `json:"activity,omitempty"`
	Since       string     `json:"since"`
	LastSeen    string     `json:"last_seen"`
	Idle        bool       `json:"idle"`
}

func presenceFrom(p plans.Presence, clientID *uuid.UUID, now time.Time) presenceResponse {
	return presenceResponse{
		ClientID: p.ClientID, ClientType: p.ClientType, PrincipalID: p.PrincipalID,
		You: clientID != nil && *clientID == p.ClientID, Project: p.ProjectPath,
		PlanID: p.PlanID, PlanTitle: p.PlanTitle, ItemID: p.ItemID, ItemText: p.ItemText, Activity: p.Activity,
		Since: p.Since.Format(time.RFC3339), LastSeen: p.LastBeat.Format(time.RFC3339), Idle: p.Idle(now),
	}
}

type personResponse struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func peopleFrom(m map[uuid.UUID]plans.Person) map[string]personResponse {
	out := make(map[string]personResponse, len(m))
	for id, p := range m {
		out[id.String()] = personResponse{Kind: p.Kind, Name: p.Name}
	}
	return out
}

type boardResponse struct {
	Phases     []phaseResponse           `json:"phases"`
	Plans      []planSummaryResponse     `json:"plans"`
	InProgress []activeItemResponse      `json:"in_progress"`
	Blocked    []activeItemResponse      `json:"blocked"`
	Presence   []presenceResponse        `json:"presence"`
	People     map[string]personResponse `json:"people"`
}

func (d Deps) getPlanBoard(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	b, err := d.Plans.Board(r.Context(), principalIDFrom(r.Context()), project.ID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := boardResponse{
		Phases: []phaseResponse{}, Plans: []planSummaryResponse{}, InProgress: []activeItemResponse{},
		Blocked: []activeItemResponse{}, Presence: []presenceResponse{}, People: peopleFrom(b.People),
	}
	for _, ph := range b.Phases {
		pr := phaseFrom(ph.Phase)
		pr.Counts, pr.Effort = countsFrom(ph.Counts), effortFrom(ph.Effort)
		out.Phases = append(out.Phases, pr)
	}
	for _, p := range b.Plans {
		out.Plans = append(out.Plans, planSummaryResponse{
			ID: p.ID, PhaseID: p.PhaseID, Status: p.Status, Title: p.Title, CreatedBy: p.CreatedBy,
			UpdatedAt: p.UpdatedAt.Format(time.RFC3339), Counts: countsFrom(p.Counts), Effort: effortFrom(p.Effort),
		})
	}
	for _, a := range b.InProgress {
		out.InProgress = append(out.InProgress, activeItemResponse{PlanTitle: a.PlanTitle, Item: itemFrom(a.Item)})
	}
	for _, a := range b.Blocked {
		out.Blocked = append(out.Blocked, activeItemResponse{PlanTitle: a.PlanTitle, Item: itemFrom(a.Item)})
	}
	now, clientID := time.Now(), clientIDFrom(r.Context())
	for _, p := range b.Presence {
		out.Presence = append(out.Presence, presenceFrom(p, clientID, now))
	}
	writeJSON(w, http.StatusOK, out)
}

// getMyPresence is where everyone is, across the caller's projects.
func (d Deps) getMyPresence(w http.ResponseWriter, r *http.Request) {
	list, people, err := d.Plans.MyPresence(r.Context(), principalIDFrom(r.Context()))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := struct {
		Items  []presenceResponse        `json:"items"`
		People map[string]personResponse `json:"people"`
	}{Items: []presenceResponse{}, People: peopleFrom(people)}
	now, clientID := time.Now(), clientIDFrom(r.Context())
	for _, p := range list {
		out.Items = append(out.Items, presenceFrom(p, clientID, now))
	}
	writeJSON(w, http.StatusOK, out)
}

type newItemRequest struct {
	Text string `json:"text"`
	Kind string `json:"kind,omitempty"`
}

func newItems(in []newItemRequest) []plans.NewItem {
	out := make([]plans.NewItem, len(in))
	for i, it := range in {
		out[i] = plans.NewItem{Text: it.Text, Kind: it.Kind}
	}
	return out
}

func (d Deps) createPlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PhaseID *uuid.UUID       `json:"phase_id,omitempty"`
		Title   string           `json:"title"`
		Body    string           `json:"body"`
		Items   []newItemRequest `json:"items"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	project := projectFrom(r.Context())
	p, err := d.Plans.CreatePlan(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), project.ID,
		req.PhaseID, req.Title, req.Body, newItems(req.Items))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, planFrom(p))
}

func uuidParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", name+" must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}

func (d Deps) getPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "planID")
	if !ok {
		return
	}
	p, err := d.Plans.GetPlan(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, planFrom(p))
}

func (d Deps) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "planID")
	if !ok {
		return
	}
	// phase_id is read raw so that "absent" (leave it) and null (take the
	// plan out of its phase) stay different.
	var req struct {
		Title           *string         `json:"title"`
		Body            *string         `json:"body"`
		ExpectedVersion int             `json:"expected_version"`
		Status          *string         `json:"status"`
		PhaseID         json.RawMessage `json:"phase_id"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	u := plans.PlanUpdate{Title: req.Title, Body: req.Body, ExpectedVersion: req.ExpectedVersion, Status: req.Status}
	if len(req.PhaseID) > 0 {
		u.PhaseSet = true
		if err := json.Unmarshal(req.PhaseID, &u.PhaseID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "phase_id must be a UUID or null")
			return
		}
	}
	p, err := d.Plans.UpdatePlan(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id, u)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, planFrom(p))
}

func (d Deps) deletePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "planID")
	if !ok {
		return
	}
	if err := d.Plans.DeletePlan(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), projectFrom(r.Context()).ID, id); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) addPlanItems(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "planID")
	if !ok {
		return
	}
	var req struct {
		Items []newItemRequest `json:"items"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	p, err := d.Plans.AddItems(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id, newItems(req.Items))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, planFrom(p))
}

func (d Deps) updatePlanItem(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "itemID")
	if !ok {
		return
	}
	var req struct {
		Status   *string         `json:"status"`
		Text     *string         `json:"text"`
		Note     *string         `json:"note"`
		Assignee json.RawMessage `json:"assignee"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	u := plans.ItemUpdate{Status: req.Status, Text: req.Text, Note: req.Note}
	if len(req.Assignee) > 0 {
		u.AssigneeSet = true
		if err := json.Unmarshal(req.Assignee, &u.Assignee); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "assignee must be a principal UUID or null")
			return
		}
	}
	it, err := d.Plans.UpdateItem(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), projectFrom(r.Context()).ID, id, u)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, itemFrom(it))
}

func (d Deps) listPlanItemHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "itemID")
	if !ok {
		return
	}
	list, err := d.Plans.Transitions(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	type transitionResponse struct {
		From        string     `json:"from"`
		To          string     `json:"to"`
		PrincipalID uuid.UUID  `json:"principal_id"`
		ClientID    *uuid.UUID `json:"client_id,omitempty"`
		At          string     `json:"at"`
	}
	// A bare array: bounded by plans.HistoryPerItem.
	out := []transitionResponse{}
	for _, t := range list {
		out = append(out, transitionResponse{From: t.From, To: t.To, PrincipalID: t.PrincipalID, ClientID: t.ClientID, At: t.At.Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) searchPlans(w http.ResponseWriter, r *http.Request) {
	hits, err := d.Plans.Search(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID,
		r.URL.Query().Get("q"), r.URL.Query().Get("status"))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	type hitResponse struct {
		PlanID     uuid.UUID  `json:"plan_id"`
		PlanTitle  string     `json:"plan_title"`
		PlanStatus string     `json:"plan_status"`
		ItemID     *uuid.UUID `json:"item_id,omitempty"`
		Text       string     `json:"text"`
		Status     string     `json:"status"`
	}
	// A bare array: capped at plans.MaxSearchHits.
	out := []hitResponse{}
	for _, h := range hits {
		out = append(out, hitResponse{PlanID: h.PlanID, PlanTitle: h.PlanTitle, PlanStatus: h.PlanStatus, ItemID: h.ItemID, Text: h.Text, Status: h.Status})
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) reportPresence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlanID    *uuid.UUID `json:"plan_id"`
		ItemID    *uuid.UUID `json:"item_id"`
		Activity  string     `json:"activity"`
		TokensIn  int64      `json:"tokens_in"`
		TokensOut int64      `json:"tokens_out"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	clientID := clientIDFrom(r.Context())
	p, err := d.Plans.Report(r.Context(), principalIDFrom(r.Context()), clientID, projectFrom(r.Context()).ID, plans.Report{
		PlanID: req.PlanID, ItemID: req.ItemID, Activity: req.Activity, TokensIn: req.TokensIn, TokensOut: req.TokensOut,
	})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, presenceFrom(p, clientID, time.Now()))
}

func (d Deps) clearPresence(w http.ResponseWriter, r *http.Request) {
	if err := d.Plans.ClearPresence(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), projectFrom(r.Context()).ID, plans.Effort{}); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) createPhase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Goal  string `json:"goal"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	ph, err := d.Plans.CreatePhase(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, req.Title, req.Goal)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, phaseFrom(ph))
}

func (d Deps) updatePhase(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "phaseID")
	if !ok {
		return
	}
	var req struct {
		Title  *string `json:"title"`
		Goal   *string `json:"goal"`
		Status *string `json:"status"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	ph, err := d.Plans.UpdatePhase(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id,
		plans.PhaseUpdate{Title: req.Title, Goal: req.Goal, Status: req.Status})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, phaseFrom(ph))
}

func (d Deps) deletePhase(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "phaseID")
	if !ok {
		return
	}
	if err := d.Plans.DeletePhase(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, id); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
