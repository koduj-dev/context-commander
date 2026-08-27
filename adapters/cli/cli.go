// Package cli implements the ccd command-line interface — bootstrap,
// user/token management, and migrations. It calls internal/identity,
// internal/projects, and internal/db directly, the same domain code the
// future HTTP/MCP adapters call — no bootstrap-only logic lives here (see
// docs/adr/0009-pat-format-and-bootstrap.md).
package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
)

const databaseURLEnv = "CC_DATABASE_URL"

// NewCommand returns the "ccd" root command's subcommands that depend on
// the domain layer (bootstrap, user, token, migrate). cmd/ccd/main.go wires
// this into the root command alongside command-independent ones (version).
func NewCommand() []*cobra.Command {
	return []*cobra.Command{
		newMigrateCmd(),
		newBootstrapCmd(),
		newUserCmd(),
		newTokenCmd(),
		newProjectCmd(),
		newArtifactCmd(),
		newServeCmd(),
	}
}

func databaseURLFlag(cmd *cobra.Command) {
	cmd.Flags().String("database-url", "", fmt.Sprintf("Postgres connection string (defaults to $%s)", databaseURLEnv))
}

func resolveDatabaseURL(cmd *cobra.Command) (string, error) {
	if v, _ := cmd.Flags().GetString("database-url"); v != "" {
		return v, nil
	}
	if v := os.Getenv(databaseURLEnv); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("no database URL: pass --database-url or set $%s", databaseURLEnv)
}

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations (requires an owner-privileged connection — see docs/adr/0002)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn, err := resolveDatabaseURL(cmd)
			if err != nil {
				return err
			}
			if err := db.Migrate(dsn); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "migrations applied")
			return nil
		},
	}
	databaseURLFlag(cmd)
	return cmd
}

func newUserCmd() *cobra.Command {
	userCmd := &cobra.Command{Use: "user", Short: "Manage users"}

	create := &cobra.Command{
		Use:   "create",
		Short: "Create a user and its Principal",
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
			pool, err := db.NewPool(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			store := identity.NewStore()
			var principal identity.Principal
			err = db.WithoutPrincipal(cmd.Context(), pool, func(tx pgx.Tx) error {
				u, err := store.CreateUser(cmd.Context(), tx, email, name)
				if err != nil {
					return err
				}
				principal, err = store.CreatePrincipalForUser(cmd.Context(), tx, u.ID)
				return err
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "user created: email=%s principal_id=%s\n", email, principal.ID)
			return nil
		},
	}
	create.Flags().String("email", "", "user email (required)")
	create.Flags().String("name", "", "display name (required)")
	databaseURLFlag(create)

	userCmd.AddCommand(create)
	return userCmd
}

func newTokenCmd() *cobra.Command {
	tokenCmd := &cobra.Command{Use: "token", Short: "Manage Personal Access Tokens"}

	create := &cobra.Command{
		Use:   "create",
		Short: "Issue a PAT for an existing user (docs/adr/0009-pat-format-and-bootstrap.md)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			email, _ := cmd.Flags().GetString("user")
			clientType, _ := cmd.Flags().GetString("client")
			expiresIn, _ := cmd.Flags().GetDuration("expires-in")
			if email == "" {
				return fmt.Errorf("--user (email) is required")
			}
			if clientType == "" {
				clientType = "cli"
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

			token, err := issueTokenForUserEmail(cmd.Context(), pool, email, clientType, expiresIn)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), token)
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "^ store this token now — it cannot be shown again, only the hash is persisted")
			return nil
		},
	}
	create.Flags().String("user", "", "user email (required)")
	create.Flags().String("client", "cli", "client type, e.g. claude-code, chatgpt, cli")
	create.Flags().Duration("expires-in", 0, "expiry duration, e.g. 720h (0 = never expires)")
	databaseURLFlag(create)

	tokenCmd.AddCommand(create)
	return tokenCmd
}

func newBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Create the first user and PAT on a fresh install — no UI, no manual SQL required",
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
			pool, err := db.NewPool(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			store := identity.NewStore()
			err = db.WithoutPrincipal(cmd.Context(), pool, func(tx pgx.Tx) error {
				u, err := store.CreateUser(cmd.Context(), tx, email, name)
				if err != nil {
					return err
				}
				_, err = store.CreatePrincipalForUser(cmd.Context(), tx, u.ID)
				return err
			})
			if err != nil {
				return err
			}

			token, err := issueTokenForUserEmail(cmd.Context(), pool, email, "cli", 0)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "user created: %s\n", email)
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), token)
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "^ store this token now — it cannot be shown again, only the hash is persisted")
			return nil
		},
	}
	cmd.Flags().String("email", "", "email for the first user (required)")
	cmd.Flags().String("name", "", "display name for the first user (required)")
	databaseURLFlag(cmd)
	return cmd
}

func issueTokenForUserEmail(ctx context.Context, pool *db.Pool, email, clientType string, expiresIn time.Duration) (string, error) {
	store := identity.NewStore()
	var token string

	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var userID, principalID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
			return fmt.Errorf("cli: look up user %q: %w", email, err)
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM principals WHERE user_id = $1`, userID).Scan(&principalID); err != nil {
			return fmt.Errorf("cli: look up principal for %q: %w", email, err)
		}

		var expiresAt *time.Time
		if expiresIn > 0 {
			t := time.Now().Add(expiresIn)
			expiresAt = &t
		}

		var err error
		token, _, err = store.IssueCredential(ctx, tx, principalID, clientType, expiresAt)
		return err
	})
	return token, err
}
