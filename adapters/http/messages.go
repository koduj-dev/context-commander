package http

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/messages"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// Messages to agents mid-work (docs/adr/0032). A person sends from the
// interface into a project; an agent's session reads its inbox across every
// project it is in, which is the shape GET /v1/me/presence already has.

type deliveryResponse struct {
	ClientID    uuid.UUID `json:"client_id"`
	ClientType  string    `json:"client_type"`
	PrincipalID uuid.UUID `json:"principal_id"`
	DeliveredAt string    `json:"delivered_at"`
	AckedAt     string    `json:"acked_at,omitempty"`
}

type messageResponse struct {
	ID       uuid.UUID `json:"id"`
	Project  string    `json:"project,omitempty"`
	From     uuid.UUID `json:"from"`
	FromName string    `json:"from_name,omitempty"`
	// Audience is "session", "principal" or "project".
	Audience    string             `json:"audience"`
	ToPrincipal *uuid.UUID         `json:"to_principal,omitempty"`
	ToClient    *uuid.UUID         `json:"to_client,omitempty"`
	Body        string             `json:"body"`
	CreatedAt   string             `json:"created_at"`
	ExpiresAt   string             `json:"expires_at"`
	Expired     bool               `json:"expired"`
	Deliveries  []deliveryResponse `json:"deliveries"`
	// Redelivered, in an inbox, says this session was handed the message
	// before and has not acknowledged it.
	Redelivered bool `json:"redelivered,omitempty"`
}

func messageFrom(m messages.Message, now time.Time) messageResponse {
	r := messageResponse{
		ID: m.ID, Project: m.ProjectPath, From: m.Sender, Audience: m.Audience(),
		ToPrincipal: m.RecipientPrincipal, ToClient: m.RecipientClient, Body: m.Body,
		CreatedAt: m.CreatedAt.Format(time.RFC3339), ExpiresAt: m.ExpiresAt.Format(time.RFC3339),
		Expired: !m.ExpiresAt.After(now), Deliveries: []deliveryResponse{},
	}
	for _, d := range m.Deliveries {
		dr := deliveryResponse{ClientID: d.ClientID, ClientType: d.ClientType, PrincipalID: d.PrincipalID, DeliveredAt: d.DeliveredAt.Format(time.RFC3339)}
		if d.AckedAt != nil {
			dr.AckedAt = d.AckedAt.Format(time.RFC3339)
		}
		r.Deliveries = append(r.Deliveries, dr)
	}
	return r
}

type sendMessageRequest struct {
	Body string `json:"body"`
	// ToClient addresses one session (a client_id from presence);
	// ToPrincipal every session of a principal. Neither: everyone working
	// on the project.
	ToClient         *uuid.UUID `json:"to_client,omitempty"`
	ToPrincipal      *uuid.UUID `json:"to_principal,omitempty"`
	ExpiresInMinutes int        `json:"expires_in_minutes,omitempty"`
}

// sendMessage is POST /v1/projects/{ns}/{slug}/messages. Only a person in
// the interface may send (messages.ErrNeedsPerson).
func (d Deps) sendMessage(w http.ResponseWriter, r *http.Request) {
	var req sendMessageRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	// Bounded here as well as in the service, so a huge number cannot wrap
	// around time.Duration into something that passes.
	if req.ExpiresInMinutes < 0 || req.ExpiresInMinutes > int(messages.MaxTTL/time.Minute) {
		writeError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("expires_in_minutes must be between 1 and %d", int(messages.MaxTTL/time.Minute)))
		return
	}
	m, err := d.Messages.Send(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), projectFrom(r.Context()).ID, messages.NewMessage{
		Body: req.Body, RecipientClient: req.ToClient, RecipientPrincipal: req.ToPrincipal,
		TTL: time.Duration(req.ExpiresInMinutes) * time.Minute,
	})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, messageFrom(m, time.Now()))
}

// listSentMessages is GET /v1/projects/{ns}/{slug}/messages: what the
// caller sent in the project, newest first, with who received it.
func (d Deps) listSentMessages(w http.ResponseWriter, r *http.Request) {
	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	page, err := d.Messages.Sent(r.Context(), principalIDFrom(r.Context()), projectFrom(r.Context()).ID, opts)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	now := time.Now()
	out := struct {
		Items      []messageResponse         `json:"items"`
		NextCursor string                    `json:"next_cursor,omitempty"`
		People     map[string]personResponse `json:"people"`
	}{Items: []messageResponse{}, NextCursor: page.NextCursor, People: peopleFrom(page.People)}
	for _, m := range page.Items {
		out.Items = append(out.Items, messageFrom(m, now))
	}
	writeJSON(w, http.StatusOK, out)
}

// getMyInbox is GET /v1/me/inbox: the messages waiting for the calling
// session, oldest first, across its projects (?project= narrows it). Reading
// is delivery — the response is the hand-over — so this GET records it;
// it is idempotent in what it returns, because nothing returned is gone
// until POST /v1/me/inbox/ack.
func (d Deps) getMyInbox(w http.ResponseWriter, r *http.Request) {
	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	in := messages.InboxOptions{Options: opts}
	principalID := principalIDFrom(r.Context())
	if raw := r.URL.Query().Get("project"); raw != "" {
		var project projects.Project
		err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			var err error
			project, err = d.Projects.Resolve(r.Context(), tx, raw)
			return err
		})
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		in.ProjectID = &project.ID
	}
	if raw := r.URL.Query().Get("ack"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?ack= must be true or false")
			return
		}
		in.Ack = v
	}
	page, err := d.Messages.Inbox(r.Context(), principalID, clientIDFrom(r.Context()), in)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	now := time.Now()
	out := struct {
		Items      []messageResponse `json:"items"`
		NextCursor string            `json:"next_cursor,omitempty"`
	}{Items: []messageResponse{}, NextCursor: page.NextCursor}
	for _, m := range page.Items {
		mr := messageFrom(m, now)
		mr.FromName = page.People[m.Sender].Name
		// An inbox shows the reader's own receipt, not everyone's.
		mr.Redelivered = len(m.Deliveries) > 0
		mr.Deliveries = []deliveryResponse{}
		out.Items = append(out.Items, mr)
	}
	writeJSON(w, http.StatusOK, out)
}

// ackMyInbox is POST /v1/me/inbox/ack with {"message_ids": [...]}.
func (d Deps) ackMyInbox(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MessageIDs []uuid.UUID `json:"message_ids"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	n, err := d.Messages.Ack(r.Context(), principalIDFrom(r.Context()), clientIDFrom(r.Context()), req.MessageIDs)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"acknowledged": n})
}
