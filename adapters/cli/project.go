package cli

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/projects"
)

func newProjectCmd() *cobra.Command {
	projectCmd := &cobra.Command{Use: "project", Short: "Manage projects"}
	projectCmd.AddCommand(newProjectCreateCmd())
	return projectCmd
}

// newProjectCreateCmd wraps projects.Store.CreateForOwner — see that
// method's doc for why creating a project and self-adding as OWNER always
// happens together (docs/adr/0015-project-creation-rls-bootstrap.md).
func newProjectCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: `Create a project at "namespace/slug" and become its OWNER`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn, err := resolveDatabaseURL(cmd)
			if err != nil {
				return err
			}
			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			projectPath, _ := cmd.Flags().GetString("project")
			name, _ := cmd.Flags().GetString("name")
			if projectPath == "" || name == "" {
				return fmt.Errorf("--project and --name are required")
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

			identityStore := identity.NewStore()
			projStore := projects.NewStore()
			var project projects.Project

			err = db.WithPrincipal(cmd.Context(), pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
				principal, err := identityStore.GetPrincipal(cmd.Context(), tx, principalID)
				if err != nil {
					return err
				}
				if principal.UserID == nil {
					return errors.New("cli: only a User principal can create a personal namespace this way (ServicePrincipal support is a later feature)")
				}
				project, err = projStore.CreateForOwner(cmd.Context(), tx, *principal.UserID, principalID, projectPath, name)
				return err
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "created %s (id=%s), you are OWNER\n", projectPath, project.ID)
			return nil
		},
	}
	cmd.Flags().String("project", "", `project path "namespace/slug" (required)`)
	cmd.Flags().String("name", "", "display name (required)")
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}
