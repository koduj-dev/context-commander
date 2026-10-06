package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// This file deliberately exposes only Create/List/Delete — never Resolve.
// secret.resolve returns plaintext and is not something an arbitrary HTTP
// caller should be handed generically; see
// /home/filip/.claude/plans/joyful-conjuring-garden.md's Secrets section.
// Resolve stays reachable only through adapters/cli (a trusted,
// same-machine operator surface), not here or in adapters/mcp.

type createSecretRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"` // travels in the JSON body, never a URL query — query strings land in access logs.
}

type secretReferenceResponse struct {
	Reference string `json:"reference"`
	Name      string `json:"name"`
}

func (d Deps) createSecret(w http.ResponseWriter, r *http.Request) {
	var req createSecretRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}
	if req.Name == "" || req.Value == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", `"name" and "value" are required`)
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	ref, err := d.Secrets.Create(r.Context(), principalID, project.ID, req.Name, []byte(req.Value))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, secretReferenceResponse{Reference: ref.String(), Name: ref.Name})
}

func (d Deps) listSecrets(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	page, err := d.Secrets.List(r.Context(), principalID, project.ID, opts)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]secretReferenceResponse, len(page.Items))
	for i, ref := range page.Items {
		out[i] = secretReferenceResponse{Reference: ref.String(), Name: ref.Name}
	}
	writeList(w, out, page.NextCursor)
}

func (d Deps) deleteSecret(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "secret name is required")
		return
	}
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	if err := d.Secrets.Delete(r.Context(), principalID, project.ID, name); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
