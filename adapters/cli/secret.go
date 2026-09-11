package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/secrets"
)

// newSecretCmd is the only adapter that exposes Service.Resolve — see
// "resolve" below for why the CLI is trusted with plaintext where
// adapters/http and adapters/mcp deliberately are not.
func newSecretCmd() *cobra.Command {
	secretCmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage project secrets (secret:// references — values are never stored as ordinary artifacts)",
	}
	secretCmd.AddCommand(newSecretCreateCmd(), newSecretListCmd(), newSecretDeleteCmd(), newSecretResolveCmd())
	return secretCmd
}

func secretFlags(cmd *cobra.Command) {
	cmd.Flags().String("project", "", `project path "namespace/slug" (required)`)
	databaseURLFlag(cmd)
	tokenFlag(cmd)
}

type secretContext struct {
	svc         *secrets.Service
	principalID uuid.UUID
	project     projects.Project
}

// setupSecretContext mirrors setupArtifactContext (adapters/cli/artifact.go):
// authenticate the token, resolve the project, construct the same
// internal/secrets.Service the HTTP/MCP adapters use — no logic here beyond
// flag parsing and I/O.
func setupSecretContext(cmd *cobra.Command) (*secretContext, error) {
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
	backend := secrets.NewLocalEncryptedBackend(pool, cryptoProvider)
	svc := secrets.NewService(pool, authz, backend)

	return &secretContext{svc: svc, principalID: principalID, project: project}, nil
}

func newSecretCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Store or rotate a secret value",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			value, _ := cmd.Flags().GetString("value")
			valueStdin, _ := cmd.Flags().GetBool("value-stdin")
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			switch {
			case valueStdin && value != "":
				return fmt.Errorf("pass either --value or --value-stdin, not both")
			case valueStdin:
				// Reading from stdin keeps the secret out of shell history and
				// process listings (ps) — --value doesn't, and is offered only
				// for scripting convenience.
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil && line == "" {
					return fmt.Errorf("read value from stdin: %w", err)
				}
				value = strings.TrimRight(line, "\r\n")
			case value == "":
				return fmt.Errorf("--value or --value-stdin is required")
			}

			sc, err := setupSecretContext(cmd)
			if err != nil {
				return err
			}
			ref, err := sc.svc.Create(cmd.Context(), sc.principalID, sc.project.ID, name, []byte(value))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), ref.String())
			return nil
		},
	}
	cmd.Flags().String("name", "", "secret name (required)")
	cmd.Flags().String("value", "", "secret value (prefer --value-stdin to avoid shell history)")
	cmd.Flags().Bool("value-stdin", false, "read the secret value from stdin instead of --value")
	secretFlags(cmd)
	return cmd
}

func newSecretListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a project's secret references — names only, never values",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := setupSecretContext(cmd)
			if err != nil {
				return err
			}
			page, err := sc.svc.List(cmd.Context(), sc.principalID, sc.project.ID, pagingOptionsFrom(cmd))
			if err != nil {
				return err
			}
			for _, ref := range page.Items {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), ref.String())
			}
			printNextCursor(cmd, page.NextCursor)
			return nil
		},
	}
	secretFlags(cmd)
	pagingFlags(cmd)
	return cmd
}

func newSecretDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a secret value",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			sc, err := setupSecretContext(cmd)
			if err != nil {
				return err
			}
			return sc.svc.Delete(cmd.Context(), sc.principalID, sc.project.ID, name)
		},
	}
	cmd.Flags().String("name", "", "secret name (required)")
	secretFlags(cmd)
	return cmd
}

// newSecretResolveCmd is deliberately CLI-only — adapters/http and
// adapters/mcp never expose Resolve. The CLI runs as a trusted operator on
// the same machine as (or with direct network access to) the database, not
// as a general-purpose surface any connected agent can call; printing a
// plaintext secret to an operator's own terminal is a materially different
// exposure than handing it to an arbitrary MCP client by default. Whether
// to ever add an MCP/HTTP secret.resolve is left to a future decision (see
// the plan's Secrets section).
func newSecretResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Print a secret's plaintext value (operator/CLI only — never exposed over HTTP or MCP)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			sc, err := setupSecretContext(cmd)
			if err != nil {
				return err
			}
			ref := secrets.NewProjectReference(sc.project.ID, name)
			plaintext, err := sc.svc.Resolve(cmd.Context(), sc.principalID, sc.project.ID, ref)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(plaintext))
			return nil
		},
	}
	cmd.Flags().String("name", "", "secret name (required)")
	secretFlags(cmd)
	return cmd
}
