// Command ccd is the Context Commander server/CLI — a single binary combining
// the server, migrations, and administrative CLI (docs/architecture-cookbook.md §21).
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ccd",
		Short: "Context Commander — self-hosted shared context layer for humans and AI agents",
	}

	cmd.AddCommand(newVersionCmd())
	// Domain-backed subcommands (serve, bootstrap, user, token, migrate, sync-skills)
	// land in adapters/cli once internal/identity and internal/db exist (Phase 1).

	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ccd version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	}
}
