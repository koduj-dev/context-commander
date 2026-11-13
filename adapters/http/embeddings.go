package http

import (
	"context"
	"net/http"
	"time"

	"github.com/koduj-dev/context-commander/internal/embeddings"
	"github.com/koduj-dev/context-commander/internal/installation"
)

// EmbeddingsStatus is what the admin status endpoint reads
// (*embeddings.Index implements it).
type EmbeddingsStatus interface {
	Status(ctx context.Context) embeddings.Status
}

// embeddingsStatusResponse is semantic search's configuration and health
// for an administrator (docs/adr/0028): no memory text, no vector, no
// credential. The endpoint is configuration the administrator's operator
// set, and it is shown because "where does our text go" is the question an
// administrator most needs answered.
type embeddingsStatusResponse struct {
	Configured    bool    `json:"configured"`
	Provider      string  `json:"provider,omitempty"`
	Model         string  `json:"model,omitempty"`
	Endpoint      string  `json:"endpoint,omitempty"`
	Store         string  `json:"store,omitempty"`
	Dimensions    int     `json:"dimensions,omitempty"`
	Reachable     *bool   `json:"reachable,omitempty"`
	LastSuccess   *string `json:"last_success,omitempty"`
	LastError     string  `json:"last_error,omitempty"`
	LastErrorAt   *string `json:"last_error_at,omitempty"`
	Queue         int     `json:"queue"`
	QueueCapacity int     `json:"queue_capacity,omitempty"`
	Dropped       int64   `json:"dropped"`
	Pending       int64   `json:"pending"`
	CacheBytes    int64   `json:"cache_bytes,omitempty"`
	CacheLimit    int64   `json:"cache_limit_bytes,omitempty"`
}

func (d Deps) getEmbeddingsStatus(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())
	callerUserID, err := resolveCallerUserID(r, d.Pool, principalID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	admin, err := d.Installation.IsAdmin(r.Context(), principalID, callerUserID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	if !admin {
		writeDomainError(w, r, installation.ErrForbidden)
		return
	}
	if d.Embeddings == nil {
		writeJSON(w, http.StatusOK, embeddingsStatusResponse{})
		return
	}
	st := d.Embeddings.Status(r.Context())
	when := func(t *time.Time) *string {
		if t == nil {
			return nil
		}
		v := t.UTC().Format(time.RFC3339)
		return &v
	}
	writeJSON(w, http.StatusOK, embeddingsStatusResponse{
		Configured: true, Provider: st.Provider, Model: st.Model, Endpoint: st.Endpoint, Store: st.Store,
		Dimensions: st.Dimensions, Reachable: st.Reachable, LastSuccess: when(st.LastSuccess),
		LastError: st.LastError, LastErrorAt: when(st.LastErrorAt),
		Queue: st.Queue, QueueCapacity: st.QueueCapacity, Dropped: st.Dropped, Pending: st.Pending,
		CacheBytes: st.CacheBytes, CacheLimit: st.CacheLimit,
	})
}
