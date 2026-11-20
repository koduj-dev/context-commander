package http

import (
	"encoding/base64"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/skills"
)

type signerResponse struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id,omitempty"`
	Name           string     `json:"name"`
	PublicKey      string     `json:"public_key"`
	CreatedAt      time.Time  `json:"created_at"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}

func toSignerResponse(s skills.Signer) signerResponse {
	out := signerResponse{ID: s.ID.String(), Name: s.Name, PublicKey: base64.StdEncoding.EncodeToString(s.PublicKey),
		CreatedAt: s.CreatedAt, RevokedAt: s.RevokedAt}
	if s.OrganizationID != nil {
		out.OrganizationID = s.OrganizationID.String()
	}
	return out
}

// signerScope is the organization a skill-signers route names, nil on the
// installation routes. ok=false means the response was already written.
func signerScope(w http.ResponseWriter, r *http.Request) (*uuid.UUID, bool) {
	raw := chi.URLParam(r, "id")
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "organization id must be a UUID")
		return nil, false
	}
	return &id, true
}

// listSkillSigners is GET /installation/skill-signers and
// GET /organizations/{id}/skill-signers: the keys whose signatures make a
// skill version "signed" (docs/adr/0040). Installation keys are readable by
// any signed-in caller: a public key is public, and an agent deciding
// whether to trust a skill may want to know whose key vouched for it. An
// organization's keys are readable by its members.
func (d Deps) listSkillSigners(w http.ResponseWriter, r *http.Request) {
	orgID, ok := signerScope(w, r)
	if !ok {
		return
	}
	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil {
		internalError(w, r, err, "could not resolve the caller's user")
		return
	}
	list, err := d.Skills.Signers(r.Context(), principalID, userID, orgID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]signerResponse, len(list))
	for i, s := range list {
		out[i] = toSignerResponse(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// addSkillSigner is POST /installation/skill-signers and
// POST /organizations/{id}/skill-signers {name, public_key}. Installation
// administrators, and for an organization also its managers.
func (d Deps) addSkillSigner(w http.ResponseWriter, r *http.Request) {
	orgID, ok := signerScope(w, r)
	if !ok {
		return
	}
	var body struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}
	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil {
		internalError(w, r, err, "could not resolve the caller's user")
		return
	}
	s, err := d.Skills.AddSigner(r.Context(), principalID, userID, orgID, body.Name, body.PublicKey)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSignerResponse(s))
}

// revokeSkillSigner is DELETE /installation/skill-signers/{signerID} and
// DELETE /organizations/{id}/skill-signers/{signerID}.
func (d Deps) revokeSkillSigner(w http.ResponseWriter, r *http.Request) {
	orgID, ok := signerScope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "signerID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "id must be a UUID")
		return
	}
	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil {
		internalError(w, r, err, "could not resolve the caller's user")
		return
	}
	if err := d.Skills.RevokeSigner(r.Context(), principalID, userID, orgID, id); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// approveSkillVersion is POST /skills/versions/{skillVersionID}/approve
// {note}: a person vouches for a version (docs/adr/0040).
func (d Deps) approveSkillVersion(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "skillVersionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "skill version id must be a UUID")
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSONBody(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
			return
		}
	}
	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil {
		internalError(w, r, err, "could not resolve the caller's user")
		return
	}
	if err := d.Skills.Approve(r.Context(), principalID, userID, clientIDFrom(r.Context()), id, body.Note); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
