package http

import (
	"encoding/base64"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type signerResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	PublicKey string     `json:"public_key"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// listSkillSigners is GET /installation/skill-signers: the keys whose
// signatures make a skill version "signed" (docs/adr/0040). Readable by any
// signed-in caller: a public key is public, and an agent deciding whether
// to trust a skill may want to know whose key vouched for it.
func (d Deps) listSkillSigners(w http.ResponseWriter, r *http.Request) {
	list, err := d.Skills.Signers(r.Context(), principalIDFrom(r.Context()))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]signerResponse, len(list))
	for i, s := range list {
		out[i] = signerResponse{ID: s.ID.String(), Name: s.Name, PublicKey: base64.StdEncoding.EncodeToString(s.PublicKey),
			CreatedAt: s.CreatedAt, RevokedAt: s.RevokedAt}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// addSkillSigner is POST /installation/skill-signers {name, public_key}.
// Installation administrators only.
func (d Deps) addSkillSigner(w http.ResponseWriter, r *http.Request) {
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
	s, err := d.Skills.AddSigner(r.Context(), principalID, userID, body.Name, body.PublicKey)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, signerResponse{ID: s.ID.String(), Name: s.Name,
		PublicKey: base64.StdEncoding.EncodeToString(s.PublicKey), CreatedAt: s.CreatedAt})
}

// revokeSkillSigner is DELETE /installation/skill-signers/{id}.
func (d Deps) revokeSkillSigner(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
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
	if err := d.Skills.RevokeSigner(r.Context(), principalID, userID, id); err != nil {
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
