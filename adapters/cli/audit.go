package cli

import (
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/projects"
)

func newAuditCmd() *cobra.Command {
	auditCmd := &cobra.Command{Use: "audit", Short: "Query the project audit trail"}
	auditCmd.AddCommand(newAuditListCmd())
	return auditCmd
}

func newAuditListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List audit log entries for a project. Requires ADMIN or OWNER",
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectPath, _ := cmd.Flags().GetString("project")
			if projectPath == "" {
				return fmt.Errorf("--project is required")
			}
			action, _ := cmd.Flags().GetString("action")
			resourceType, _ := cmd.Flags().GetString("resource-type")

			dsn, err := resolveDatabaseURL(cmd)
			if err != nil {
				return err
			}
			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			pool, err := db.NewPool(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			principalID, _, err := authenticate(cmd.Context(), pool, token)
			if err != nil {
				return err
			}

			projStore := projects.NewStore()
			var project projects.Project
			err = db.WithPrincipal(cmd.Context(), pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
				var err error
				project, err = projStore.Resolve(cmd.Context(), tx, projectPath)
				return err
			})
			if err != nil {
				return fmt.Errorf("resolve project %q: %w", projectPath, err)
			}

			authz := auth.NewAuthzService(pool, projStore)
			svc := audit.NewService(pool, authz)

			page, err := svc.Query(cmd.Context(), principalID, project.ID, audit.QueryOptions{
				Action: action, ResourceType: resourceType,
				Paging: pagingOptionsFrom(cmd),
			})
			if err != nil {
				return err
			}
			for _, rec := range page.Items {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%d\t%s\t%s\t%s\t%s\n", rec.ID, rec.Timestamp.Format(timeLayoutRFC3339), rec.Action, rec.ResourceType, rec.Result)
			}
			printNextCursor(cmd, page.NextCursor)
			return nil
		},
	}
	cmd.Flags().String("project", "", `project path "namespace/slug" (required)`)
	cmd.Flags().String("action", "", "exact-match action filter")
	cmd.Flags().String("resource-type", "", "exact-match resource type filter")
	pagingFlags(cmd)
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}

const timeLayoutRFC3339 = "2006-01-02T15:04:05Z07:00"
