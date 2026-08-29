package mcp

import (
	"context"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
)

func registerHistoryTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	_ = clientID // history tools don't attribute writes to a client credential today

	resolve := func(ctx context.Context, projectPath string) (uuid.UUID, error) {
		var id uuid.UUID
		err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			p, err := deps.Projects.Resolve(ctx, tx, projectPath)
			if err != nil {
				return err
			}
			id = p.ID
			return nil
		})
		return id, err
	}

	type conversationResult struct {
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
	}
	type createConversationArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "history.create_conversation",
		Description: "Start a new conversation — Context Commander's own stable identity for a logical thread of work, independent of any provider session ID.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args createConversationArgs) (*sdkmcp.CallToolResult, conversationResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, conversationResult{}, err
		}
		c, err := deps.History.CreateConversation(ctx, principalID, projectID)
		if err != nil {
			return nil, conversationResult{}, err
		}
		return nil, conversationResult{ID: c.ID.String(), CreatedAt: c.CreatedAt.Format(time.RFC3339)}, nil
	})

	type externalSessionResult struct {
		ID                      string `json:"id"`
		ConversationID          string `json:"conversation_id"`
		Provider                string `json:"provider"`
		Client                  string `json:"client"`
		ProviderSessionID       string `json:"provider_session_id"`
		ParentExternalSessionID string `json:"parent_external_session_id,omitempty"`
		Relation                string `json:"relation"`
		StartedAt               string `json:"started_at"`
	}
	type createExternalSessionArgs struct {
		Project                 string `json:"project" jsonschema:"project path \"namespace/slug\""`
		ConversationID          string `json:"conversation_id" jsonschema:"conversation this session belongs to"`
		Provider                string `json:"provider" jsonschema:"e.g. anthropic, openai"`
		Client                  string `json:"client" jsonschema:"e.g. claude-code, chatgpt, codex"`
		ProviderSessionID       string `json:"provider_session_id" jsonschema:"opaque provider session ID — never treated as canonical identity"`
		ParentExternalSessionID string `json:"parent_external_session_id,omitempty" jsonschema:"prior external session this one continues/compacts/forks from"`
		Relation                string `json:"relation" jsonschema:"started | continued | compacted_from | forked_from | imported_from"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "history.create_external_session",
		Description: "Record one provider-side session as an edge into a conversation's lineage.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args createExternalSessionArgs) (*sdkmcp.CallToolResult, externalSessionResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, externalSessionResult{}, err
		}
		conversationID, err := uuid.Parse(args.ConversationID)
		if err != nil {
			return nil, externalSessionResult{}, err
		}
		var parentID *uuid.UUID
		if args.ParentExternalSessionID != "" {
			id, err := uuid.Parse(args.ParentExternalSessionID)
			if err != nil {
				return nil, externalSessionResult{}, err
			}
			parentID = &id
		}
		es, err := deps.History.CreateExternalSession(ctx, principalID, projectID, conversationID,
			args.Provider, args.Client, args.ProviderSessionID, parentID, args.Relation, time.Now().UTC())
		if err != nil {
			return nil, externalSessionResult{}, err
		}
		result := externalSessionResult{
			ID: es.ID.String(), ConversationID: es.ConversationID.String(), Provider: es.Provider, Client: es.Client,
			ProviderSessionID: es.ProviderSessionID, Relation: es.Relation, StartedAt: es.StartedAt.Format(time.RFC3339),
		}
		if es.ParentExternalSessionID != nil {
			result.ParentExternalSessionID = es.ParentExternalSessionID.String()
		}
		return nil, result, nil
	})

	type historyObjectResult struct {
		ID                string `json:"id"`
		ExternalSessionID string `json:"external_session_id"`
		Type              string `json:"type"`
		DerivedFrom       string `json:"derived_from,omitempty"`
		ContentHash       string `json:"content_hash"`
		Content           string `json:"content"`
		CreatedAt         string `json:"created_at"`
	}
	type appendArgs struct {
		Project           string `json:"project" jsonschema:"project path \"namespace/slug\""`
		ConversationID    string `json:"conversation_id" jsonschema:"conversation this object belongs to"`
		ExternalSessionID string `json:"external_session_id" jsonschema:"external session this object was produced in"`
		Type              string `json:"type" jsonschema:"raw | compacted | summary | imported"`
		DerivedFrom       string `json:"derived_from,omitempty" jsonschema:"source HistoryObject ID this one was derived from (compaction/summarization never overwrites the original)"`
		Content           string `json:"content" jsonschema:"the plaintext content to store, encrypted at rest"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "history.append_object",
		Description: "Append an immutable history object to a conversation. Compaction/summarization always " +
			"appends a new object with derived_from set — it never edits or replaces the original.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args appendArgs) (*sdkmcp.CallToolResult, historyObjectResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, historyObjectResult{}, err
		}
		conversationID, err := uuid.Parse(args.ConversationID)
		if err != nil {
			return nil, historyObjectResult{}, err
		}
		externalSessionID, err := uuid.Parse(args.ExternalSessionID)
		if err != nil {
			return nil, historyObjectResult{}, err
		}
		var derivedFrom *uuid.UUID
		if args.DerivedFrom != "" {
			id, err := uuid.Parse(args.DerivedFrom)
			if err != nil {
				return nil, historyObjectResult{}, err
			}
			derivedFrom = &id
		}
		obj, err := deps.History.AppendHistoryObject(ctx, principalID, projectID, conversationID, externalSessionID, args.Type, derivedFrom, []byte(args.Content))
		if err != nil {
			return nil, historyObjectResult{}, err
		}
		result := historyObjectResult{
			ID: obj.ID.String(), ExternalSessionID: obj.ExternalSessionID.String(), Type: obj.Type,
			ContentHash: obj.ContentHash, Content: base64.StdEncoding.EncodeToString(obj.Body), CreatedAt: obj.CreatedAt.Format(time.RFC3339),
		}
		if obj.DerivedFrom != nil {
			result.DerivedFrom = obj.DerivedFrom.String()
		}
		return nil, result, nil
	})

	type listObjectsArgs struct {
		Project        string `json:"project" jsonschema:"project path \"namespace/slug\""`
		ConversationID string `json:"conversation_id" jsonschema:"conversation to list objects for"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "history.list_objects",
		Description: "List a conversation's history objects, decrypted, oldest first.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listObjectsArgs) (*sdkmcp.CallToolResult, []historyObjectResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, nil, err
		}
		conversationID, err := uuid.Parse(args.ConversationID)
		if err != nil {
			return nil, nil, err
		}
		list, err := deps.History.ListHistoryObjects(ctx, principalID, projectID, conversationID)
		if err != nil {
			return nil, nil, err
		}
		out := make([]historyObjectResult, len(list))
		for i, obj := range list {
			out[i] = historyObjectResult{
				ID: obj.ID.String(), ExternalSessionID: obj.ExternalSessionID.String(), Type: obj.Type,
				ContentHash: obj.ContentHash, Content: base64.StdEncoding.EncodeToString(obj.Body), CreatedAt: obj.CreatedAt.Format(time.RFC3339),
			}
			if obj.DerivedFrom != nil {
				out[i].DerivedFrom = obj.DerivedFrom.String()
			}
		}
		return nil, out, nil
	})

	type lineageEdgeResult struct {
		FromID string `json:"from_id"`
		ToID   string `json:"to_id"`
		Kind   string `json:"kind"`
	}
	type lineageArgs struct {
		Project        string `json:"project" jsonschema:"project path \"namespace/slug\""`
		ConversationID string `json:"conversation_id" jsonschema:"conversation to get the lineage graph for"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "history.get_lineage",
		Description: "Get a conversation's lineage graph edges (compaction and session-continuation relationships) — not assumed to be a single tree.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args lineageArgs) (*sdkmcp.CallToolResult, []lineageEdgeResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, nil, err
		}
		conversationID, err := uuid.Parse(args.ConversationID)
		if err != nil {
			return nil, nil, err
		}
		edges, err := deps.History.GetLineage(ctx, principalID, projectID, conversationID)
		if err != nil {
			return nil, nil, err
		}
		out := make([]lineageEdgeResult, len(edges))
		for i, e := range edges {
			out[i] = lineageEdgeResult{FromID: e.FromID.String(), ToID: e.ToID.String(), Kind: e.Kind}
		}
		return nil, out, nil
	})
}
