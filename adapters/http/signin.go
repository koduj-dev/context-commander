package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/signin"
)

// Sign-in rules (docs/adr/0024): which domains and addresses may sign in
// through OIDC, and which organizations a new account joins.

type signInOrganizationResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type signInRuleResponse struct {
	ID            string                       `json:"id"`
	Kind          string                       `json:"kind"`
	Value         string                       `json:"value"`
	Organizations []signInOrganizationResponse `json:"organizations"`
	CreatedAt     string                       `json:"created_at"`
}

func signInRuleFrom(r signin.Rule) signInRuleResponse {
	orgs := make([]signInOrganizationResponse, len(r.Organizations))
	for i, o := range r.Organizations {
		orgs[i] = signInOrganizationResponse{ID: o.ID.String(), Name: o.Name}
	}
	return signInRuleResponse{
		ID: r.ID.String(), Kind: string(r.Kind), Value: r.Value,
		Organizations: orgs, CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

// organizationIDs parses the ids a rule grants. An empty list is a rule that
// admits and grants nothing, which is legitimate.
func organizationIDs(w http.ResponseWriter, raw []string) ([]uuid.UUID, bool) {
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "organization_ids must be UUIDs")
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func (d Deps) listSignInRules(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	rules, err := d.Installation.SignInRules(r.Context(), principalID, callerUserID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]signInRuleResponse, len(rules))
	for i, rule := range rules {
		out[i] = signInRuleFrom(rule)
	}
	writeList(w, out, "")
}

func (d Deps) listSignInOrganizations(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	orgs, err := d.Installation.SignInOrganizations(r.Context(), principalID, callerUserID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]signInOrganizationResponse, len(orgs))
	for i, o := range orgs {
		out[i] = signInOrganizationResponse{ID: o.ID.String(), Name: o.Name}
	}
	writeList(w, out, "")
}

// createSignInRule takes {"value": "example.com" | "someone@example.com",
// "organization_ids": [...]}. The kind follows from the value.
func (d Deps) createSignInRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value           string   `json:"value"`
		OrganizationIDs []string `json:"organization_ids"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	orgs, ok := organizationIDs(w, body.OrganizationIDs)
	if !ok {
		return
	}
	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	rule, err := d.Installation.CreateSignInRule(r.Context(), principalID, callerUserID, body.Value, orgs)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, signInRuleFrom(rule))
}

func (d Deps) setSignInRuleOrganizations(w http.ResponseWriter, r *http.Request) {
	ruleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "rule id must be a UUID")
		return
	}
	var body struct {
		OrganizationIDs []string `json:"organization_ids"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	orgs, ok := organizationIDs(w, body.OrganizationIDs)
	if !ok {
		return
	}
	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	rule, err := d.Installation.SetSignInRuleOrganizations(r.Context(), principalID, callerUserID, ruleID, orgs)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, signInRuleFrom(rule))
}

func (d Deps) deleteSignInRule(w http.ResponseWriter, r *http.Request) {
	ruleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "rule id must be a UUID")
		return
	}
	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	if err := d.Installation.DeleteSignInRule(r.Context(), principalID, callerUserID, ruleID); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
