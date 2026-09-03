package cli

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
)

// newInstallationCmd groups two distinct trust tiers under one parent
// command (docs/adr/0017):
//
//   - grant-admin/revoke-admin/list-admins are operator/--database-url-only,
//     the same trust level as `ccd migrate`/`ccd user create` — no
//     authenticated principal can grant the very first installation admin
//     (nobody exists yet to hold CapUserCreate-equivalent rights), so this
//     stays a direct-database-access action, deliberately with no HTTP/MCP
//     equivalent.
//   - create-user/create-service-principal/issue-token/list-tokens/
//     revoke-token go through the authenticated installation.Service, same
//     --token flow as every other CLI command, gated by a real installation
//     admin's capabilities.
func newInstallationCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "installation", Short: "Manage installation-wide administration (docs/adr/0017)"}
	cmd.AddCommand(
		newInstallationGrantAdminCmd(),
		newInstallationRevokeAdminCmd(),
		newInstallationListAdminsCmd(),
		newInstallationCreateUserCmd(),
		newInstallationCreateServicePrincipalCmd(),
		newInstallationIssueTokenCmd(),
		newInstallationListTokensCmd(),
		newInstallationRevokeTokenCmd(),
	)
	return cmd
}

func newInstallationGrantAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grant-admin",
		Short: "Grant installation-admin rights to an existing user, by email (operator action, requires --database-url)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			email, _ := cmd.Flags().GetString("email")
			if email == "" {
				return fmt.Errorf("--email is required")
			}
			dsn, err := resolveDatabaseURL(cmd)
			if err != nil {
				return err
			}
			pool, err := db.NewPool(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			identityStore := identity.NewStore()
			installationStore := installation.NewStore()
			err = db.WithoutPrincipal(cmd.Context(), pool, func(tx pgx.Tx) error {
				u, err := identityStore.GetUserByEmail(cmd.Context(), tx, email)
				if err != nil {
					return err
				}
				_, err = installationStore.Grant(cmd.Context(), tx, u.ID, nil)
				return err
			})
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is now an installation admin\n", email)
			return nil
		},
	}
	cmd.Flags().String("email", "", "email of an existing user (required)")
	databaseURLFlag(cmd)
	return cmd
}

func newInstallationRevokeAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke-admin",
		Short: "Revoke installation-admin rights from a user, by email (operator action, requires --database-url)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			email, _ := cmd.Flags().GetString("email")
			if email == "" {
				return fmt.Errorf("--email is required")
			}
			dsn, err := resolveDatabaseURL(cmd)
			if err != nil {
				return err
			}
			pool, err := db.NewPool(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			identityStore := identity.NewStore()
			installationStore := installation.NewStore()
			err = db.WithoutPrincipal(cmd.Context(), pool, func(tx pgx.Tx) error {
				u, err := identityStore.GetUserByEmail(cmd.Context(), tx, email)
				if err != nil {
					return err
				}
				return installationStore.Revoke(cmd.Context(), tx, u.ID)
			})
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is no longer an installation admin\n", email)
			return nil
		},
	}
	cmd.Flags().String("email", "", "email of an existing installation admin (required)")
	databaseURLFlag(cmd)
	return cmd
}

func newInstallationListAdminsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-admins",
		Short: "List installation admins (operator action, requires --database-url)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn, err := resolveDatabaseURL(cmd)
			if err != nil {
				return err
			}
			pool, err := db.NewPool(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			identityStore := identity.NewStore()
			installationStore := installation.NewStore()
			var lines []string
			err = db.WithoutPrincipal(cmd.Context(), pool, func(tx pgx.Tx) error {
				admins, err := installationStore.List(cmd.Context(), tx)
				if err != nil {
					return err
				}
				for _, a := range admins {
					u, err := identityStore.GetUser(cmd.Context(), tx, a.UserID)
					if err != nil {
						return err
					}
					lines = append(lines, fmt.Sprintf("%s\t%s\t%s", u.Email, a.Role, a.GrantedAt.Format(time.RFC3339)))
				}
				return nil
			})
			if err != nil {
				return err
			}
			if len(lines) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no installation admins")
				return nil
			}
			for _, l := range lines {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), l)
			}
			return nil
		},
	}
	databaseURLFlag(cmd)
	return cmd
}

func newInstallationCreateUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-user",
		Short: "Create a user, as an installation admin (requires CapUserCreate — see docs/adr/0017)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			email, _ := cmd.Flags().GetString("email")
			name, _ := cmd.Flags().GetString("name")
			if email == "" || name == "" {
				return fmt.Errorf("--email and --name are required")
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

			svc := installation.NewService(pool, installation.NewStore(), identity.NewStore())
			u, err := svc.CreateUser(cmd.Context(), principalID, callerUserID, email, name)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "user created: id=%s email=%s\n", u.ID, u.Email)
			return nil
		},
	}
	cmd.Flags().String("email", "", "new user's email (required)")
	cmd.Flags().String("name", "", "new user's display name (required)")
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}

func newInstallationCreateServicePrincipalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-service-principal",
		Short: "Register a service principal (agent), as an installation admin (requires CapServicePrincipalCreate)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			description, _ := cmd.Flags().GetString("description")
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
			callerUserID, err := resolveCallerUserID(cmd.Context(), pool, principalID)
			if err != nil {
				return err
			}

			svc := installation.NewService(pool, installation.NewStore(), identity.NewStore())
			sp, err := svc.CreateServicePrincipal(cmd.Context(), principalID, callerUserID, name, description)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "service principal created: id=%s name=%s\n", sp.ID, sp.Name)
			return nil
		},
	}
	cmd.Flags().String("name", "", "service principal name (required)")
	cmd.Flags().String("description", "", "service principal description")
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}

func newInstallationIssueTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue-token",
		Short: "Issue a PAT for any principal, as an installation admin (requires CapCredentialManage)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			principalRaw, _ := cmd.Flags().GetString("principal")
			clientType, _ := cmd.Flags().GetString("client")
			expiresIn, _ := cmd.Flags().GetDuration("expires-in")
			if principalRaw == "" {
				return fmt.Errorf("--principal is required")
			}
			targetPrincipalID, err := uuid.Parse(principalRaw)
			if err != nil {
				return fmt.Errorf("invalid --principal: %w", err)
			}
			if clientType == "" {
				clientType = "cli"
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

			var expiresAt *time.Time
			if expiresIn > 0 {
				t := time.Now().Add(expiresIn)
				expiresAt = &t
			}

			svc := installation.NewService(pool, installation.NewStore(), identity.NewStore())
			issued, _, err := svc.IssueCredentialFor(cmd.Context(), principalID, callerUserID, targetPrincipalID, clientType, expiresAt)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), issued)
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "^ store this token now — it cannot be shown again, only the hash is persisted")
			return nil
		},
	}
	cmd.Flags().String("principal", "", "target principal UUID to issue a token for (required)")
	cmd.Flags().String("client", "cli", "client type, e.g. claude-code, chatgpt, cli")
	cmd.Flags().Duration("expires-in", 0, "expiry duration, e.g. 720h (0 = never expires)")
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}

func newInstallationListTokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-tokens",
		Short: "List a principal's tokens, as an installation admin (requires CapCredentialManage)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			principalRaw, _ := cmd.Flags().GetString("principal")
			if principalRaw == "" {
				return fmt.Errorf("--principal is required")
			}
			targetPrincipalID, err := uuid.Parse(principalRaw)
			if err != nil {
				return fmt.Errorf("invalid --principal: %w", err)
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

			svc := installation.NewService(pool, installation.NewStore(), identity.NewStore())
			creds, err := svc.ListCredentialsFor(cmd.Context(), principalID, callerUserID, targetPrincipalID)
			if err != nil {
				return err
			}
			if len(creds) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no tokens")
				return nil
			}
			for _, c := range creds {
				status := "active"
				if c.RevokedAt != nil {
					status = "revoked"
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", c.ID, c.ClientType, status, c.CreatedAt.Format(time.RFC3339))
			}
			return nil
		},
	}
	cmd.Flags().String("principal", "", "target principal UUID (required)")
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}

func newInstallationRevokeTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke-token <credential-id>",
		Short: "Revoke any principal's token by credential id, as an installation admin (requires CapCredentialManage)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			credentialID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid credential id: %w", err)
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

			svc := installation.NewService(pool, installation.NewStore(), identity.NewStore())
			if err := svc.RevokeCredentialFor(cmd.Context(), principalID, callerUserID, credentialID); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "token revoked: %s\n", credentialID)
			return nil
		},
	}
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}
