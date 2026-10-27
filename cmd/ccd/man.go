package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// newManCmd writes the manual pages, one per command (ccd.1, ccd-serve.1,
// ...). They are generated from the commands themselves at release time
// (`make release`) and never committed, so a page cannot say something
// --help does not. Hidden: it is a build step, not something an operator
// runs. The date comes from $SOURCE_DATE_EPOCH when set, so a rebuild of the
// same tag produces the same pages.
func newManCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "man DIR",
		Short:  "Write the manual pages into DIR",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := os.MkdirAll(args[0], 0o750); err != nil {
				return err
			}
			root := cmd.Root()
			root.DisableAutoGenTag = true
			header := &doc.GenManHeader{
				Section: "1",
				Source:  "Context Commander " + version,
				Manual:  "Context Commander Manual",
			}
			if err := doc.GenManTree(root, header, args[0]); err != nil {
				return fmt.Errorf("writing manual pages: %w", err)
			}
			return nil
		},
	}
	return cmd
}
