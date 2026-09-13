package cli

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/projects"
)

func newHistoryCmd() *cobra.Command {
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "Manage conversations, external sessions, and history objects — mainly for debugging/testing lineage",
	}
	historyCmd.AddCommand(
		newHistoryCreateConversationCmd(),
		newHistoryListConversationsCmd(),
		newHistoryCreateSessionCmd(),
		newHistoryAppendObjectCmd(),
		newHistoryListObjectsCmd(),
		newHistoryLineageCmd(),
	)
	return historyCmd
}

// historyContext mirrors handoffContext/artifactContext: authenticate,
// resolve the project, construct the same internal/history.Service the
// HTTP/MCP adapters use.
type historyContext struct {
	svc         *history.Service
	principalID uuid.UUID
	project     projects.Project
	pool        *db.Pool
}

func setupHistoryContext(cmd *cobra.Command) (*historyContext, error) {
	dsn, err := resolveDatabaseURL(cmd)
	if err != nil {
		return nil, err
	}
	token, err := resolveToken(cmd)
	if err != nil {
		return nil, err
	}
	projectPath, _ := cmd.Flags().GetString("project")
	if projectPath == "" {
		return nil, fmt.Errorf("--project is required")
	}

	pool, err := db.NewPool(cmd.Context(), dsn)
	if err != nil {
		return nil, err
	}

	principalID, _, err := authenticate(cmd.Context(), pool, token)
	if err != nil {
		return nil, err
	}

	projStore := projects.NewStore()
	var project projects.Project
	err = db.WithPrincipal(cmd.Context(), pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		project, err = projStore.Resolve(cmd.Context(), tx, projectPath)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("resolve project %q: %w", projectPath, err)
	}

	root, err := crypto.NewEnvRootKeyProvider(rootKeyEnv)
	if err != nil {
		return nil, err
	}
	cryptoProvider := crypto.NewProvider(root)
	authz := auth.NewAuthzService(pool, projStore)
	svc := history.NewService(pool, cryptoProvider, authz)

	return &historyContext{svc: svc, principalID: principalID, project: project, pool: pool}, nil
}

func newHistoryCreateConversationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-conversation",
		Short: "Start a new conversation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			hc, err := setupHistoryContext(cmd)
			if err != nil {
				return err
			}
			defer hc.pool.Close()

			c, err := hc.svc.CreateConversation(cmd.Context(), hc.principalID, hc.project.ID)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "created conversation %s\n", c.ID)
			return nil
		},
	}
	projectFlag(cmd)
	return cmd
}

func newHistoryCreateSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-session <conversation-id>",
		Short: "Record an external (provider) session as an edge into a conversation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hc, err := setupHistoryContext(cmd)
			if err != nil {
				return err
			}
			defer hc.pool.Close()

			conversationID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid conversation id: %w", err)
			}
			provider, _ := cmd.Flags().GetString("provider")
			client, _ := cmd.Flags().GetString("client")
			providerSessionID, _ := cmd.Flags().GetString("provider-session-id")
			relation, _ := cmd.Flags().GetString("relation")
			parentRaw, _ := cmd.Flags().GetString("parent")
			var parent *uuid.UUID
			if parentRaw != "" {
				id, err := uuid.Parse(parentRaw)
				if err != nil {
					return fmt.Errorf("invalid --parent: %w", err)
				}
				parent = &id
			}

			es, err := hc.svc.CreateExternalSession(cmd.Context(), hc.principalID, hc.project.ID, conversationID,
				provider, client, providerSessionID, parent, relation, time.Now().UTC())
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "created external session %s (relation=%s)\n", es.ID, es.Relation)
			return nil
		},
	}
	cmd.Flags().String("provider", "", "e.g. anthropic, openai (required)")
	cmd.Flags().String("client", "", "e.g. claude-code, chatgpt, codex (required)")
	cmd.Flags().String("provider-session-id", "", "opaque provider session ID (required)")
	cmd.Flags().String("relation", "started", "started | continued | compacted_from | forked_from | imported_from")
	cmd.Flags().String("parent", "", "parent external session ID, if any")
	projectFlag(cmd)
	return cmd
}

func newHistoryAppendObjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "append-object <conversation-id> <external-session-id>",
		Short: "Append an immutable history object (raw/compacted/summary/imported) to a conversation",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			hc, err := setupHistoryContext(cmd)
			if err != nil {
				return err
			}
			defer hc.pool.Close()

			conversationID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid conversation id: %w", err)
			}
			externalSessionID, err := uuid.Parse(args[1])
			if err != nil {
				return fmt.Errorf("invalid external session id: %w", err)
			}
			objType, _ := cmd.Flags().GetString("type")
			content, _ := cmd.Flags().GetString("content")
			derivedRaw, _ := cmd.Flags().GetString("derived-from")
			var derivedFrom *uuid.UUID
			if derivedRaw != "" {
				id, err := uuid.Parse(derivedRaw)
				if err != nil {
					return fmt.Errorf("invalid --derived-from: %w", err)
				}
				derivedFrom = &id
			}

			obj, err := hc.svc.AppendHistoryObject(cmd.Context(), hc.principalID, hc.project.ID, conversationID, externalSessionID, objType, derivedFrom, []byte(content))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "appended history object %s (type=%s, content_hash=%s)\n", obj.ID, obj.Type, obj.ContentHash)
			return nil
		},
	}
	cmd.Flags().String("type", "raw", "raw | compacted | summary | imported")
	cmd.Flags().String("content", "", "plaintext content to store, encrypted at rest (required)")
	cmd.Flags().String("derived-from", "", "source history object ID this one was derived from")
	projectFlag(cmd)
	return cmd
}

func newHistoryListObjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-objects <conversation-id>",
		Short: "List a conversation's history objects, oldest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hc, err := setupHistoryContext(cmd)
			if err != nil {
				return err
			}
			defer hc.pool.Close()

			conversationID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid conversation id: %w", err)
			}
			page, err := hc.svc.ListHistoryObjects(cmd.Context(), hc.principalID, hc.project.ID, conversationID, pagingOptionsFrom(cmd))
			if err != nil {
				return err
			}
			if len(page.Items) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no history objects)")
				return nil
			}
			for _, obj := range page.Items {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-36s %-10s created %s\n", obj.ID, obj.Type, obj.CreatedAt.Format(time.RFC3339))
			}
			printNextCursor(cmd, page.NextCursor)
			return nil
		},
	}
	projectFlag(cmd)
	pagingFlags(cmd)
	return cmd
}

func newHistoryLineageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lineage <conversation-id>",
		Short: "Show a conversation's lineage graph edges",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hc, err := setupHistoryContext(cmd)
			if err != nil {
				return err
			}
			defer hc.pool.Close()

			conversationID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid conversation id: %w", err)
			}
			edges, err := hc.svc.GetLineage(cmd.Context(), hc.principalID, hc.project.ID, conversationID)
			if err != nil {
				return err
			}
			if len(edges) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no lineage edges)")
				return nil
			}
			for _, e := range edges {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s --%s--> %s\n", e.FromID, e.Kind, e.ToID)
			}
			return nil
		},
	}
	projectFlag(cmd)
	return cmd
}

// newHistoryListConversationsCmd is the discovery half: before it, the only
// way to find a conversation was to already know its id.
func newHistoryListConversationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-conversations",
		Short: "List a project's conversations, most recent first",
		RunE: func(cmd *cobra.Command, _ []string) error {
			hc, err := setupHistoryContext(cmd)
			if err != nil {
				return err
			}
			defer hc.pool.Close()

			page, err := hc.svc.ListConversations(cmd.Context(), hc.principalID, hc.project.ID, pagingOptionsFrom(cmd))
			if err != nil {
				return err
			}
			if len(page.Items) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no conversations)")
				return nil
			}
			for _, c := range page.Items {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  created %s\n", c.ID, c.CreatedAt.Format(time.RFC3339))
			}
			printNextCursor(cmd, page.NextCursor)
			return nil
		},
	}
	projectFlag(cmd)
	pagingFlags(cmd)
	return cmd
}
