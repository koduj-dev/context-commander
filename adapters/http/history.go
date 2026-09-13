package http

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/history"
)

type conversationResponse struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt string    `json:"created_at"`
}

func (d Deps) createConversation(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	c, err := d.History.CreateConversation(r.Context(), principalID, project.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationResponse{ID: c.ID, CreatedAt: c.CreatedAt.Format(time.RFC3339)})
}

type externalSessionResponse struct {
	ID                      uuid.UUID  `json:"id"`
	ConversationID          uuid.UUID  `json:"conversation_id"`
	Provider                string     `json:"provider"`
	Client                  string     `json:"client"`
	ProviderSessionID       string     `json:"provider_session_id"`
	ParentExternalSessionID *uuid.UUID `json:"parent_external_session_id,omitempty"`
	Relation                string     `json:"relation"`
	StartedAt               string     `json:"started_at"`
}

type createExternalSessionRequest struct {
	Provider                string     `json:"provider"`
	Client                  string     `json:"client"`
	ProviderSessionID       string     `json:"provider_session_id"`
	ParentExternalSessionID *uuid.UUID `json:"parent_external_session_id,omitempty"`
	Relation                string     `json:"relation"`
}

func (d Deps) createExternalSession(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "conversationID must be a UUID")
		return
	}
	var req createExternalSessionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	es, err := d.History.CreateExternalSession(r.Context(), principalID, project.ID, conversationID,
		req.Provider, req.Client, req.ProviderSessionID, req.ParentExternalSessionID, req.Relation, time.Now().UTC())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, externalSessionResponse{
		ID: es.ID, ConversationID: es.ConversationID, Provider: es.Provider, Client: es.Client,
		ProviderSessionID: es.ProviderSessionID, ParentExternalSessionID: es.ParentExternalSessionID,
		Relation: es.Relation, StartedAt: es.StartedAt.Format(time.RFC3339),
	})
}

type historyObjectResponse struct {
	ID                uuid.UUID  `json:"id"`
	ExternalSessionID uuid.UUID  `json:"external_session_id"`
	Type              string     `json:"type"`
	DerivedFrom       *uuid.UUID `json:"derived_from,omitempty"`
	ContentHash       string     `json:"content_hash"`
	Content           string     `json:"content"`
	CreatedAt         string     `json:"created_at"`
}

type appendHistoryObjectRequest struct {
	ExternalSessionID uuid.UUID  `json:"external_session_id"`
	Type              string     `json:"type"`
	DerivedFrom       *uuid.UUID `json:"derived_from,omitempty"`
	Content           string     `json:"content"`
}

func (d Deps) appendHistoryObject(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "conversationID must be a UUID")
		return
	}
	// history.MaxContentSize's worth of content plus generous headroom for
	// JSON/base64 escaping overhead and the request's other fields — the
	// shared maxJSONBodyBytes cap (decodeJSONBody) is sized for metadata-only
	// bodies and would reject legitimate history content well under
	// history.Service's own domain limit.
	r.Body = http.MaxBytesReader(w, r.Body, 2*history.MaxContentSize)
	var req appendHistoryObjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	obj, err := d.History.AppendHistoryObject(r.Context(), principalID, project.ID, conversationID,
		req.ExternalSessionID, req.Type, req.DerivedFrom, []byte(req.Content))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, historyObjectResponse{
		ID: obj.ID, ExternalSessionID: obj.ExternalSessionID, Type: obj.Type, DerivedFrom: obj.DerivedFrom,
		ContentHash: obj.ContentHash, Content: base64.StdEncoding.EncodeToString(obj.Body), CreatedAt: obj.CreatedAt.Format(time.RFC3339),
	})
}

func (d Deps) listHistoryObjects(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "conversationID must be a UUID")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	page, err := d.History.ListHistoryObjects(r.Context(), principalID, project.ID, conversationID, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]historyObjectResponse, len(page.Items))
	for i, obj := range page.Items {
		out[i] = historyObjectResponse{
			ID: obj.ID, ExternalSessionID: obj.ExternalSessionID, Type: obj.Type, DerivedFrom: obj.DerivedFrom,
			ContentHash: obj.ContentHash, Content: base64.StdEncoding.EncodeToString(obj.Body), CreatedAt: obj.CreatedAt.Format(time.RFC3339),
		}
	}
	writeList(w, out, page.NextCursor)
}

type lineageEdgeResponse struct {
	FromID uuid.UUID `json:"from_id"`
	ToID   uuid.UUID `json:"to_id"`
	Kind   string    `json:"kind"`
}

func (d Deps) getLineage(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "conversationID must be a UUID")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	edges, err := d.History.GetLineage(r.Context(), principalID, project.ID, conversationID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]lineageEdgeResponse, len(edges))
	for i, e := range edges {
		out[i] = lineageEdgeResponse{FromID: e.FromID, ToID: e.ToID, Kind: e.Kind}
	}
	writeJSON(w, http.StatusOK, out)
}

type conversationListResponse struct {
	ID                 string `json:"id"`
	CreatedByPrincipal string `json:"created_by_principal"`
	CreatedAt          string `json:"created_at"`
}

// listConversations lets a client discover a project's conversations
// instead of pasting an id it had to learn some other way.
func (d Deps) listConversations(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	page, err := d.History.ListConversations(r.Context(), principalID, project.ID, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]conversationListResponse, len(page.Items))
	for i, c := range page.Items {
		out[i] = conversationListResponse{
			ID: c.ID.String(), CreatedByPrincipal: c.CreatedByPrincipal.String(),
			CreatedAt: c.CreatedAt.Format(time.RFC3339),
		}
	}
	writeList(w, out, page.NextCursor)
}
