package cli

import (
	"errors"
	"fmt"
	"strings"

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

// newProjectCreateCmd creates a project at "namespace/slug", creating a
// personal namespace for the caller first if it doesn't exist yet, and
// adds the caller as OWNER — all in one transaction, per
// docs/adr/0015-project-creation-rls-bootstrap.md: a project committed
// without an immediate self-membership insert would be permanently
// inaccessible to everyone, itself included.
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
			namespaceSlug, projectSlug, ok := strings.Cut(projectPath, "/")
			if !ok || namespaceSlug == "" || projectSlug == "" {
				return fmt.Errorf(`--project must look like "namespace/slug", got %q`, projectPath)
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

				ns, err := projStore.GetNamespaceBySlug(cmd.Context(), tx, namespaceSlug)
				if errors.Is(err, projects.ErrNotFound) {
					ns, err = projStore.CreatePersonalNamespace(cmd.Context(), tx, *principal.UserID, namespaceSlug)
				}
				if err != nil {
					return fmt.Errorf("resolve/create namespace %q: %w", namespaceSlug, err)
				}
				if ns.OwnerUserID == nil || *ns.OwnerUserID != *principal.UserID {
					return fmt.Errorf("namespace %q is not yours to create a project in", namespaceSlug)
				}

				project, err = projStore.Create(cmd.Context(), tx, ns.ID, projectSlug, name)
				if err != nil {
					return fmt.Errorf("create project: %w", err)
				}

				_, err = projStore.AddMember(cmd.Context(), tx, project.ID, principalID, projects.RoleOwner)
				if err != nil {
					return fmt.Errorf("add self as OWNER: %w", err)
				}
				return nil
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "created %s/%s (id=%s), you are OWNER\n", namespaceSlug, projectSlug, project.ID)
			return nil
		},
	}
	cmd.Flags().String("project", "", `project path "namespace/slug" (required)`)
	cmd.Flags().String("name", "", "display name (required)")
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}
