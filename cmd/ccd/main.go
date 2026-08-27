// Command ccd is the Context Commander server/CLI — a single binary combining
// the server, migrations, and administrative CLI (docs/architecture-cookbook.md §21).
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/adapters/cli"
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
	cmd.AddCommand(cli.NewCommand()...)
	// `serve` (the actual server) and `sync-skills` land with adapters/http
	// and internal/skills respectively (later phases).

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
