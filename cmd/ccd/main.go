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
		Long: `Context Commander is a self-hosted, vendor-neutral shared context and
collaboration layer for humans and AI agents. ccd is the whole of it in one
binary: "ccd serve" runs the REST API, the MCP server and the web UI;
"ccd migrate" applies database migrations as the schema owner; the remaining
commands administer an installation from the command line.

Settings are flags or CC_* environment variables; "ccd serve --help" lists
them. Installed from the .deb or .rpm package, the server is ccd.service and
reads /etc/context-commander/ccd.env, and migrations are ccd-migrate.service,
which reads /etc/context-commander/migrate.env before every start.

Documentation: https://github.com/koduj-dev/context-commander/tree/main/docs`,
	}

	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newManCmd())
	cmd.AddCommand(cli.NewCommand()...)

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
