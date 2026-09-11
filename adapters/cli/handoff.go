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
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/projects"
)

func newHandoffCmd() *cobra.Command {
	handoffCmd := &cobra.Command{
		Use:   "handoff",
		Short: "Create, read, and list handoffs — structured hand-off state between agents",
	}
	handoffCmd.AddCommand(newHandoffCreateCmd(), newHandoffGetCmd(), newHandoffListCmd())
	return handoffCmd
}

// handoffContext mirrors artifactContext (adapters/cli/artifact.go):
// authenticate, resolve the project, construct the same
// internal/handoffs.Service the HTTP/MCP adapters use.
type handoffContext struct {
	svc         *handoffs.Service
	principalID uuid.UUID
	clientID    uuid.UUID
	project     projects.Project
	pool        *db.Pool
}

func setupHandoffContext(cmd *cobra.Command) (*handoffContext, error) {
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

	principalID, clientID, err := authenticate(cmd.Context(), pool, token)
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
	svc := handoffs.NewService(pool, cryptoProvider, authz)

	return &handoffContext{svc: svc, principalID: principalID, clientID: clientID, project: project, pool: pool}, nil
}

func projectFlag(cmd *cobra.Command) {
	cmd.Flags().String("project", "", `project path "namespace/slug", e.g. "filip/es-migration" (required)`)
	databaseURLFlag(cmd)
	tokenFlag(cmd)
}

func newHandoffCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a handoff",
		RunE: func(cmd *cobra.Command, _ []string) error {
			hc, err := setupHandoffContext(cmd)
			if err != nil {
				return err
			}
			defer hc.pool.Close()

			summary, _ := cmd.Flags().GetString("summary")
			completedWork, _ := cmd.Flags().GetString("completed-work")
			nextActions, _ := cmd.Flags().GetString("next-actions")
			if summary == "" {
				return fmt.Errorf("--summary is required")
			}

			h, err := hc.svc.Create(cmd.Context(), hc.principalID, &hc.clientID, hc.project.ID, nil, handoffs.Body{
				Summary:       summary,
				CompletedWork: completedWork,
				NextActions:   nextActions,
			})
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "created handoff %s (status=%s)\n", h.ID, h.Status)
			return nil
		},
	}
	cmd.Flags().String("summary", "", "short summary of the work so far (required)")
	cmd.Flags().String("completed-work", "", "what has been completed")
	cmd.Flags().String("next-actions", "", "what the next agent should do")
	projectFlag(cmd)
	return cmd
}

func newHandoffGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Read one handoff",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hc, err := setupHandoffContext(cmd)
			if err != nil {
				return err
			}
			defer hc.pool.Close()

			id, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid handoff id: %w", err)
			}
			h, body, err := hc.svc.Get(cmd.Context(), hc.principalID, hc.project.ID, id)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "id: %s\nstatus: %s\ncreated: %s\nsummary: %s\ncompleted_work: %s\nnext_actions: %s\n",
				h.ID, h.Status, h.CreatedAt.Format(time.RFC3339), body.Summary, body.CompletedWork, body.NextActions)
			return nil
		},
	}
	projectFlag(cmd)
	return cmd
}

func newHandoffListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List handoffs in a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			hc, err := setupHandoffContext(cmd)
			if err != nil {
				return err
			}
			defer hc.pool.Close()

			var statusFilter *string
			if s, _ := cmd.Flags().GetString("status"); s != "" {
				statusFilter = &s
			}
			page, err := hc.svc.List(cmd.Context(), hc.principalID, hc.project.ID, statusFilter, pagingOptionsFrom(cmd))
			if err != nil {
				return err
			}
			if len(page.Items) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no handoffs)")
				return nil
			}
			for _, h := range page.Items {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-36s %-8s created %s\n", h.ID, h.Status, h.CreatedAt.Format(time.RFC3339))
			}
			printNextCursor(cmd, page.NextCursor)
			return nil
		},
	}
	cmd.Flags().String("status", "", "filter by status: open or closed")
	projectFlag(cmd)
	pagingFlags(cmd)
	return cmd
}
