package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
)

// Self-only PAT management (issue/list/revoke) — see internal/identity's
// package doc: identity has no authz layer and client_credentials has no
// RLS, so every handler below uses principalIDFrom(ctx) as the sole target
// and never a principal id from the request body. Building "issue/revoke a
// token for someone else" is a deliberately separate, unbuilt feature.

type issueTokenRequest struct {
	ClientType       string `json:"client_type"`
	ExpiresInSeconds *int64 `json:"expires_in_seconds"`
}

type issueTokenResponse struct {
	Token        string  `json:"token"`
	CredentialID string  `json:"credential_id"`
	ClientType   string  `json:"client_type"`
	ExpiresAt    *string `json:"expires_at,omitempty"`
}

// issueToken is the only place a PAT's raw token is ever returned — it is
// not recoverable afterwards (only its hash is stored, ADR-0009), so the
// web UI must show it to the caller exactly once.
func (d Deps) issueToken(w http.ResponseWriter, r *http.Request) {
	var req issueTokenRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}
	if req.ClientType == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "client_type is required")
		return
	}
	var expiresAt *time.Time
	if req.ExpiresInSeconds != nil {
		t := time.Now().Add(time.Duration(*req.ExpiresInSeconds) * time.Second)
		expiresAt = &t
	}
	if err := identity.ValidateExpiresAt(expiresAt); err != nil {
		writeDomainError(w, r, err)
		return
	}

	principalID := principalIDFrom(r.Context())
	var token string
	var cred identity.ClientCredential
	err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		token, cred, err = identity.NewStore().IssueCredential(r.Context(), tx, principalID, req.ClientType, expiresAt)
		return err
	})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}

	resp := issueTokenResponse{Token: token, CredentialID: cred.ID.String(), ClientType: cred.ClientType}
	if cred.ExpiresAt != nil {
		s := cred.ExpiresAt.Format(time.RFC3339)
		resp.ExpiresAt = &s
	}
	writeJSON(w, http.StatusCreated, resp)
}

// credentialResponse deliberately has no token/token_hash field — listing
// tokens must never re-expose secret material, only metadata about them.
type credentialResponse struct {
	ID         string  `json:"id"`
	ClientType string  `json:"client_type"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
}

func toCredentialResponse(c identity.ClientCredential) credentialResponse {
	resp := credentialResponse{ID: c.ID.String(), ClientType: c.ClientType, CreatedAt: c.CreatedAt.Format(time.RFC3339)}
	if c.LastUsedAt != nil {
		s := c.LastUsedAt.Format(time.RFC3339)
		resp.LastUsedAt = &s
	}
	if c.ExpiresAt != nil {
		s := c.ExpiresAt.Format(time.RFC3339)
		resp.ExpiresAt = &s
	}
	if c.RevokedAt != nil {
		s := c.RevokedAt.Format(time.RFC3339)
		resp.RevokedAt = &s
	}
	return resp
}

func (d Deps) listTokens(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())

	var creds []identity.ClientCredential
	err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		creds, err = identity.NewStore().ListTokensForPrincipal(r.Context(), tx, principalID)
		return err
	})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}

	out := make([]credentialResponse, len(creds))
	for i, c := range creds {
		out[i] = toCredentialResponse(c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "id must be a UUID")
		return
	}

	principalID := principalIDFrom(r.Context())
	err = db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return identity.NewStore().RevokeCredential(r.Context(), tx, id, principalID)
	})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
