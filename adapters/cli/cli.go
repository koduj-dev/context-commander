// Package cli implements the ccd command-line interface — bootstrap,
// user/token management, and migrations. It calls internal/identity,
// internal/projects, and internal/db directly, the same domain code the
// future HTTP/MCP adapters call — no bootstrap-only logic lives here (see
// docs/adr/0009-pat-format-and-bootstrap.md).
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
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
		newContextCmd(),
		newEventCmd(),
		newOrganizationCmd(),
		newAuditCmd(),
		newHandoffCmd(),
		newHistoryCmd(),
		newSkillCmd(),
		newSyncSkillsCmd(),
		newGateCmd(),
		newSecretCmd(),
		newInstallationCmd(),
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

	// Why an operator needs this at all: it is the way back in when nobody
	// can sign in — the first-run setup code only exists while the
	// installation has no users (docs/adr/0019), so once it has one and that
	// person's password is lost, this command and direct SQL are the only
	// remaining doors. Same trust level as `ccd migrate`: it takes a
	// --database-url and authenticates nobody.
	setPassword := &cobra.Command{
		Use:   "set-password",
		Short: "Set or replace a user's local password",
		Long: "Set or replace a user's local password.\n\n" +
			"The password is read from stdin, never from a flag: an argument would be " +
			"recorded in shell history and visible to every process on the machine in " +
			"the command line. Type it at the prompt, or pipe it in.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			email, _ := cmd.Flags().GetString("email")
			if email == "" {
				return fmt.Errorf("--email is required")
			}

			password, err := readPassword(cmd)
			if err != nil {
				return err
			}
			// Validated before the database is touched, so a password the
			// server would later refuse does not get as far as a connection.
			if err := identity.ValidatePassword(password); err != nil {
				return err
			}
			hash, err := identity.HashPassword(password)
			if err != nil {
				return err
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
				u, err := store.GetUserByEmail(cmd.Context(), tx, email)
				if err != nil {
					return err
				}
				return store.SetPassword(cmd.Context(), tx, u.ID, hash)
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "password set for %s\n", email)
			return nil
		},
	}
	setPassword.Flags().String("email", "", "user email (required)")
	databaseURLFlag(setPassword)

	userCmd.AddCommand(create)
	userCmd.AddCommand(setPassword)
	return userCmd
}

// readPassword reads a password from stdin, without echoing it when stdin is
// a terminal. A piped stdin is read as a single line, so the command works
// unattended in a provisioning script as well as by hand.
func readPassword(cmd *cobra.Command) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), "New password: ")
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		_, _ = fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(raw), nil
	}

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return "", fmt.Errorf("no password on stdin")
	}
	// Only the trailing newline is stripped: a password may legitimately
	// begin or end with a space, and silently trimming it would set a
	// password nobody can then type.
	return strings.TrimRight(scanner.Text(), "\r"), nil
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
			installationStore := installation.NewStore()
			err = db.WithoutPrincipal(cmd.Context(), pool, func(tx pgx.Tx) error {
				u, err := store.CreateUser(cmd.Context(), tx, email, name)
				if err != nil {
					return err
				}
				if _, err := store.CreatePrincipalForUser(cmd.Context(), tx, u.ID); err != nil {
					return err
				}
				// The bootstrap user becomes the first installation admin —
				// otherwise nobody could ever become one without direct
				// database access (docs/adr/0017). grantedBy is nil: there
				// is no admin yet to have granted it.
				_, err = installationStore.Grant(cmd.Context(), tx, u.ID, nil)
				return err
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is now an installation admin\n", email)
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
