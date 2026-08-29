package cli

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/organizations"
)

func newOrganizationCmd() *cobra.Command {
	orgCmd := &cobra.Command{Use: "organization", Short: "Manage organizations"}
	orgCmd.AddCommand(newOrganizationCreateCmd(), newOrganizationAddMemberCmd())
	return orgCmd
}

// resolveCallerUserID resolves the human User behind the authenticated
// principal — organization_members is keyed by User, never Principal
// (docs/adr/0006: service principals are never organization members).
func resolveCallerUserID(ctx context.Context, pool *db.Pool, principalID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := db.WithPrincipal(ctx, pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		p, err := identity.NewStore().GetPrincipal(ctx, tx, principalID)
		if err != nil {
			return err
		}
		if p.UserID == nil {
			return organizations.ErrForbidden
		}
		userID = *p.UserID
		return nil
	})
	return userID, err
}

func newOrganizationCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an organization; the caller becomes its first OWNER",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return fmt.Errorf("--name is required")
			}
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
			userID, err := resolveCallerUserID(cmd.Context(), pool, principalID)
			if err != nil {
				return err
			}

			svc := organizations.NewService(pool, organizations.NewStore())
			org, err := svc.Create(cmd.Context(), principalID, userID, name)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "organization created: id=%s name=%s\n", org.ID, org.Name)
			return nil
		},
	}
	cmd.Flags().String("name", "", "organization name (required)")
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}

func newOrganizationAddMemberCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-member",
		Short: "Add a member to an organization",
		RunE: func(cmd *cobra.Command, _ []string) error {
			orgRaw, _ := cmd.Flags().GetString("org")
			userRaw, _ := cmd.Flags().GetString("user")
			roleRaw, _ := cmd.Flags().GetString("role")
			if orgRaw == "" || userRaw == "" || roleRaw == "" {
				return fmt.Errorf("--org, --user, and --role are required")
			}
			orgID, err := uuid.Parse(orgRaw)
			if err != nil {
				return fmt.Errorf("invalid --org: %w", err)
			}
			newUserID, err := uuid.Parse(userRaw)
			if err != nil {
				return fmt.Errorf("invalid --user: %w", err)
			}

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
			callerUserID, err := resolveCallerUserID(cmd.Context(), pool, principalID)
			if err != nil {
				return err
			}

			svc := organizations.NewService(pool, organizations.NewStore())
			m, err := svc.AddMember(cmd.Context(), principalID, callerUserID, orgID, newUserID, organizations.Role(roleRaw))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "member added: org=%s user=%s role=%s\n", m.OrganizationID, m.UserID, m.Role)
			return nil
		},
	}
	cmd.Flags().String("org", "", "organization UUID (required)")
	cmd.Flags().String("user", "", "user UUID to add (required)")
	cmd.Flags().String("role", "", "OWNER, ADMIN, or MEMBER (required)")
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}
