package http

import (
	"net/http"
	"time"
)

// securityEventResponse is one refusal. The ids travel as strings that may
// name nothing — an unknown credential id is the most interesting row in
// this list, and rendering it as an ordinary reference would invite a
// reader to assume it resolves.
type securityEventResponse struct {
	ID           int64  `json:"id"`
	TS           string `json:"ts"`
	Kind         string `json:"kind"`
	PrincipalID  string `json:"principal_id,omitempty"`
	CredentialID string `json:"credential_id,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
	Capability   string `json:"capability,omitempty"`
	Reason       string `json:"reason"`
	RequestID    string `json:"request_id,omitempty"`
}

// listSecurityEvents serves the refusal timeline to installation admins.
//
// Mounted under /v1/installation rather than under a project: a refusal is
// frequently about a project the caller has no access to, and the pattern
// worth seeing — one credential probing several projects — is invisible
// from inside any single one of them.
func (d Deps) listSecurityEvents(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}

	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	page, err := d.SecurityEvents.List(r.Context(), principalID, userID, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	out := make([]securityEventResponse, len(page.Items))
	for i, e := range page.Items {
		out[i] = securityEventResponse{
			ID: e.ID, TS: e.TS.Format(time.RFC3339), Kind: string(e.Kind),
			Capability: e.Capability, Reason: e.Reason, RequestID: e.RequestID,
		}
		if e.PrincipalID != nil {
			out[i].PrincipalID = e.PrincipalID.String()
		}
		if e.CredentialID != nil {
			out[i].CredentialID = e.CredentialID.String()
		}
		if e.ProjectID != nil {
			out[i].ProjectID = e.ProjectID.String()
		}
	}
	writeList(w, out, page.NextCursor)
}
