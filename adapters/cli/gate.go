package cli

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
)

func newGateCmd() *cobra.Command {
	gateCmd := &cobra.Command{
		Use:   "gate",
		Short: "Manage gate bindings — which gate runs on which lifecycle hook for which scope (docs/architecture-cookbook.md §13)",
	}
	gateCmd.AddCommand(newGateListBindingsCmd(), newGateBindCmd(), newGateUnbindCmd())
	return gateCmd
}

// gateContext mirrors skillContext/artifactContext — authenticate, resolve
// project, build the domain service, nothing more.
type gateContext struct {
	admin       *gates.AdminService
	principalID uuid.UUID
	userID      uuid.UUID
	project     projects.Project
	pool        *db.Pool
}

func setupGateContext(cmd *cobra.Command) (*gateContext, error) {
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
	var userID uuid.UUID
	err = db.WithPrincipal(cmd.Context(), pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		project, err = projStore.Resolve(cmd.Context(), tx, projectPath)
		if err != nil {
			return err
		}
		var nullableUserID *uuid.UUID
		if err := tx.QueryRow(cmd.Context(), `SELECT user_id FROM principals WHERE id = $1`, principalID).Scan(&nullableUserID); err != nil {
			return err
		}
		if nullableUserID != nil {
			userID = *nullableUserID
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("resolve project %q: %w", projectPath, err)
	}

	authz := auth.NewAuthzService(pool, projStore)
	admin := gates.NewAdminService(pool, authz, organizations.NewStore(), projStore)

	return &gateContext{admin: admin, principalID: principalID, userID: userID, project: project, pool: pool}, nil
}

func gateFlags(cmd *cobra.Command) {
	cmd.Flags().String("project", "", `project path "namespace/slug" — used to authorize the operation (required)`)
	databaseURLFlag(cmd)
	tokenFlag(cmd)
}

func newGateListBindingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-bindings",
		Short: "List gate bindings visible to a project (installation + organization + project scope)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			gc, err := setupGateContext(cmd)
			if err != nil {
				return err
			}
			defer gc.pool.Close()

			var orgID *uuid.UUID
			if raw, _ := cmd.Flags().GetString("organization-id"); raw != "" {
				id, err := uuid.Parse(raw)
				if err != nil {
					return fmt.Errorf("--organization-id: %w", err)
				}
				orgID = &id
			}

			list, err := gc.admin.ListBindings(cmd.Context(), gc.principalID, gc.project.ID, orgID)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no gate bindings)")
				return nil
			}
			for _, b := range list {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-10s %-24s hook=%-24s enabled=%-5t id=%s\n", b.Scope, b.GateName, b.Hook, b.Enabled, b.ID)
			}
			return nil
		},
	}
	cmd.Flags().String("organization-id", "", "organization UUID, if the project belongs to one")
	gateFlags(cmd)
	return cmd
}

func newGateBindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bind",
		Short: "Bind a registered gate to a lifecycle hook for a scope",
		RunE: func(cmd *cobra.Command, _ []string) error {
			gc, err := setupGateContext(cmd)
			if err != nil {
				return err
			}
			defer gc.pool.Close()

			scope, _ := cmd.Flags().GetString("scope")
			gateName := mustFlag(cmd, "gate-name")
			hook := mustFlag(cmd, "hook")
			if gateName == "" || hook == "" {
				return fmt.Errorf("--gate-name and --hook are required")
			}
			var scopeRef *uuid.UUID
			if raw, _ := cmd.Flags().GetString("scope-ref"); raw != "" {
				id, err := uuid.Parse(raw)
				if err != nil {
					return fmt.Errorf("--scope-ref: %w", err)
				}
				scopeRef = &id
			}
			var config json.RawMessage
			if raw, _ := cmd.Flags().GetString("config"); raw != "" {
				config = json.RawMessage(raw)
			}

			b, err := gc.admin.Bind(cmd.Context(), gc.principalID, gc.userID, gc.project.ID, gates.Scope(scope), scopeRef, gateName, hook, config)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "bound binding_id=%s\n", b.ID)
			return nil
		},
	}
	cmd.Flags().String("scope", "project", `"organization" | "project" (installation scope is operator-seeded only)`)
	cmd.Flags().String("scope-ref", "", "organization or project UUID the binding applies to")
	cmd.Flags().String("gate-name", "", "registered gate name, e.g. \"secret-scanner\" (required)")
	cmd.Flags().String("hook", "", `lifecycle hook, e.g. "artifact.before_write" (required)`)
	cmd.Flags().String("config", "", "JSON config passed to the gate provider")
	gateFlags(cmd)
	return cmd
}

func newGateUnbindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unbind",
		Short: "Delete a gate binding",
		RunE: func(cmd *cobra.Command, _ []string) error {
			gc, err := setupGateContext(cmd)
			if err != nil {
				return err
			}
			defer gc.pool.Close()

			bindingID, err := uuid.Parse(mustFlag(cmd, "binding-id"))
			if err != nil {
				return fmt.Errorf("--binding-id: %w", err)
			}
			if err := gc.admin.Unbind(cmd.Context(), gc.principalID, gc.userID, gc.project.ID, bindingID); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "unbound binding_id=%s\n", bindingID)
			return nil
		},
	}
	cmd.Flags().String("binding-id", "", "gate binding UUID (required)")
	gateFlags(cmd)
	return cmd
}
