package http

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/gates"
)

type gateBindingResponse struct {
	ID       uuid.UUID       `json:"id"`
	Scope    string          `json:"scope"`
	ScopeRef *uuid.UUID      `json:"scope_ref,omitempty"`
	GateName string          `json:"gate_name"`
	Hook     string          `json:"hook"`
	Enabled  bool            `json:"enabled"`
	Config   json.RawMessage `json:"config"`
}

func toGateBindingResponse(b gates.Binding) gateBindingResponse {
	return gateBindingResponse{
		ID: b.ID, Scope: string(b.Scope), ScopeRef: b.ScopeRef,
		GateName: b.GateName, Hook: b.Hook, Enabled: b.Enabled, Config: b.Config,
	}
}

func (d Deps) listGateBindings(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	var orgID *uuid.UUID
	if raw := r.URL.Query().Get("organization_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?organization_id= must be a UUID")
			return
		}
		orgID = &id
	}

	list, err := d.Gates.ListBindings(r.Context(), principalID, project.ID, orgID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]gateBindingResponse, len(list))
	for i, b := range list {
		out[i] = toGateBindingResponse(b)
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) bindGate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope    string          `json:"scope"`
		ScopeRef *uuid.UUID      `json:"scope_ref"`
		GateName string          `json:"gate_name"`
		Hook     string          `json:"hook"`
		Config   json.RawMessage `json:"config"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil {
		internalError(w, r, err, "could not resolve the caller's user")
		return
	}

	b, err := d.Gates.Bind(r.Context(), principalID, userID, project.ID, gates.Scope(body.Scope), body.ScopeRef, body.GateName, body.Hook, body.Config)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toGateBindingResponse(b))
}

func (d Deps) unbindGate(w http.ResponseWriter, r *http.Request) {
	rawID := r.URL.Query().Get("id")
	bindingID, err := uuid.Parse(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "?id= must be a UUID")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil {
		internalError(w, r, err, "could not resolve the caller's user")
		return
	}

	if err := d.Gates.Unbind(r.Context(), principalID, userID, project.ID, bindingID); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
