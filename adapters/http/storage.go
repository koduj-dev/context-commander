package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/installation"
)

// storageResponse is one namespace's storage (docs/adr/0021). limit_bytes is
// what is enforced and is omitted when nothing limits the namespace; the
// admin-only fields say whether that is the server default or an exception.
type storageResponse struct {
	NamespaceID string `json:"namespace_id"`
	Slug        string `json:"slug"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	UsedBytes   int64  `json:"used_bytes"`
	LimitBytes  *int64 `json:"limit_bytes,omitempty"`

	CreatedAt string `json:"created_at,omitempty"`
	// Override is "default", "custom" or "unlimited". Admin listing only.
	Override string `json:"override,omitempty"`
}

func storageFrom(n installation.NamespaceStorage) storageResponse {
	return storageResponse{
		NamespaceID: n.NamespaceID.String(), Slug: n.Slug, Kind: n.Kind,
		DisplayName: n.DisplayName, UsedBytes: n.UsedBytes, LimitBytes: n.EffectiveLimit,
	}
}

// getMyStorage is the caller's own namespace and their organizations'.
func (d Deps) getMyStorage(w http.ResponseWriter, r *http.Request) {
	items, err := d.Installation.MyStorage(r.Context(), principalIDFrom(r.Context()))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]storageResponse, len(items))
	for i, n := range items {
		out[i] = storageFrom(n)
	}
	writeList(w, out, "")
}

func (d Deps) listInstallationStorage(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}

	page, err := d.Installation.ListStorage(r.Context(), principalID, callerUserID, opts)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]storageResponse, len(page.Items))
	for i, n := range page.Items {
		out[i] = storageFrom(n)
		out[i].CreatedAt = n.CreatedAt.Format(time.RFC3339)
		switch {
		case n.Unlimited:
			out[i].Override = "unlimited"
		case n.LimitBytes != nil:
			out[i].Override = "custom"
		default:
			out[i].Override = "default"
		}
	}
	writeList(w, out, page.NextCursor)
}

// setNamespaceStorageLimit takes {"limit_bytes": n} for a limit of its own,
// {"unlimited": true} for none, or {} to put the namespace back on the server
// default.
func (d Deps) setNamespaceStorageLimit(w http.ResponseWriter, r *http.Request) {
	namespaceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "namespace id must be a UUID")
		return
	}
	var body struct {
		LimitBytes *int64 `json:"limit_bytes"`
		Unlimited  bool   `json:"unlimited"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	if err := d.Installation.SetStorageLimit(r.Context(), principalID, callerUserID, namespaceID,
		installation.StorageLimit{Bytes: body.LimitBytes, Unlimited: body.Unlimited}); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
