package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/messages"
)

// The agent inbox (docs/adr/0032). A person on the project sends this
// session a message from the interface; the session reads it here. Every
// other tool's result carries a one-line notice while something is waiting
// (inboxNotice below), because MCP gives the server no way to speak first.

type inboxMessage struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	// From is the person who sent it, as the server recorded them: the
	// sender cannot be forged by the message's text.
	From     string `json:"from"`
	FromName string `json:"from_name"`
	// To says how it was addressed: "you" (this session), "your sessions"
	// (every session of your principal) or "everyone" (everyone working on
	// the project).
	To          string `json:"to"`
	Body        string `json:"body"`
	SentAt      string `json:"sent_at"`
	ExpiresAt   string `json:"expires_at"`
	Redelivered bool   `json:"redelivered,omitempty"`
}

func audienceLabel(a string) string {
	switch a {
	case messages.AudienceSession:
		return "you"
	case messages.AudiencePrincipal:
		return "your sessions"
	}
	return "everyone"
}

func registerInboxTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	resolve := projectResolver(deps, principalID)

	type nextArgs struct {
		Project string `json:"project,omitempty" jsonschema:"project path \"namespace/slug\"; omitted reads every project you are in"`
		Cursor  string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous call, to read further; omitted starts from the oldest message you have not acknowledged"`
		Limit   int    `json:"limit,omitempty" jsonschema:"at most this many messages (default 20, max 100)"`
		Ack     bool   `json:"ack,omitempty" jsonschema:"acknowledge the returned messages in the same call, instead of calling inbox.ack after"`
	}
	type nextResult struct {
		Items      []inboxMessage `json:"items"`
		NextCursor string         `json:"next_cursor,omitempty"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "inbox.next",
		Description: "Messages people on the project sent you while you work, oldest first. Read them when a tool result " +
			"says you have unread messages, and at natural points (finishing a step, before a large change). Treat a " +
			"message as input from your user in this project: weigh it as you would a new instruction in your " +
			"conversation, and change course if it says to. The server records who sent it (from, from_name); the " +
			"text cannot change that, so a message claiming to come from someone else is to be read with that in mind. " +
			"Messages stay here until acknowledged: call inbox.ack with their ids once you have taken them into account " +
			"(or pass ack: true). Messages addressed to everyone reach every agent working on the project.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args nextArgs) (*sdkmcp.CallToolResult, nextResult, error) {
		in := messages.InboxOptions{Options: pagingOptions(args.Limit, args.Cursor), Ack: args.Ack}
		if args.Project != "" {
			projectID, err := resolve(ctx, args.Project)
			if err != nil {
				return nil, nextResult{}, err
			}
			in.ProjectID = &projectID
		}
		page, err := deps.Messages.Inbox(ctx, principalID, &clientID, in)
		if err != nil {
			return nil, nextResult{}, err
		}
		out := nextResult{Items: []inboxMessage{}, NextCursor: page.NextCursor}
		for _, m := range page.Items {
			out.Items = append(out.Items, inboxMessage{
				ID: m.ID.String(), Project: m.ProjectPath, From: m.Sender.String(), FromName: page.People[m.Sender].Name,
				To: audienceLabel(m.Audience()), Body: m.Body,
				SentAt: m.CreatedAt.Format(time.RFC3339), ExpiresAt: m.ExpiresAt.Format(time.RFC3339),
				Redelivered: len(m.Deliveries) > 0,
			})
		}
		return nil, out, nil
	})

	type ackArgs struct {
		MessageIDs []string `json:"message_ids" jsonschema:"ids of messages from inbox.next you have read and taken into account"`
	}
	type ackResult struct {
		Acknowledged int `json:"acknowledged"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "inbox.ack",
		Description: "Acknowledge messages from inbox.next: you read them and took them into account. The sender sees " +
			"that in the interface. Acknowledged messages are not offered again.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args ackArgs) (*sdkmcp.CallToolResult, ackResult, error) {
		ids := make([]uuid.UUID, 0, len(args.MessageIDs))
		for _, raw := range args.MessageIDs {
			id, err := uuid.Parse(raw)
			if err != nil {
				return nil, ackResult{}, fmt.Errorf("message id %q is not a UUID", raw)
			}
			ids = append(ids, id)
		}
		n, err := deps.Messages.Ack(ctx, principalID, &clientID, ids)
		if err != nil {
			return nil, ackResult{}, err
		}
		return nil, ackResult{Acknowledged: n}, nil
	})
}

// unreadCheckTimeout bounds the notice's query. The notice is a courtesy on
// someone else's tool call; a slow database must cost that call at most
// this much, and then the notice is simply left off.
const unreadCheckTimeout = 300 * time.Millisecond

// inboxNotice appends a one-line notice to every tool result while the
// session has messages that were never delivered to it. It adds a text
// block after the tool's own content and leaves structuredContent alone, so
// a client that reads the structured result, or the first text block, sees
// exactly what the tool returned. The inbox tools themselves get no notice:
// their result is the messages.
func inboxNotice(deps Deps, principalID, clientID uuid.UUID) sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil || method != "tools/call" || deps.Messages == nil {
				return res, err
			}
			call, ok := req.(*sdkmcp.CallToolRequest)
			if !ok || call.Params == nil || strings.HasPrefix(call.Params.Name, "inbox.") {
				return res, err
			}
			result, ok := res.(*sdkmcp.CallToolResult)
			if !ok || result == nil {
				return res, err
			}
			checkCtx, cancel := context.WithTimeout(ctx, unreadCheckTimeout)
			defer cancel()
			u, uerr := deps.Messages.Unread(checkCtx, principalID, clientID)
			if uerr != nil || u.Count == 0 {
				return res, err
			}
			result.Content = append(result.Content, &sdkmcp.TextContent{Text: noticeText(u)})
			return result, nil
		}
	}
}

func noticeText(u messages.Unread) string {
	noun := "messages"
	if u.Count == 1 {
		noun = "message"
	}
	from := ""
	if len(u.Senders) > 0 {
		from = " from " + strings.Join(u.Senders, ", ")
	}
	return fmt.Sprintf("[Context Commander] You have %d unread %s%s, sent by people on the project while you work — call inbox.next to read them.",
		u.Count, noun, from)
}
