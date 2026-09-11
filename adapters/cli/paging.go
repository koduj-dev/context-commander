package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/paging"
)

// pagingFlags adds the two flags every list command shares. They are
// deliberately the same two everywhere — a user who learns --cursor on one
// command already knows it on the rest.
func pagingFlags(cmd *cobra.Command) {
	cmd.Flags().Int("limit", 0, fmt.Sprintf("max rows to return (default %d, max %d)", paging.DefaultLimit, paging.MaxLimit))
	cmd.Flags().String("cursor", "", "continue from a previous run's next-cursor line")
}

func pagingOptionsFrom(cmd *cobra.Command) paging.Options {
	limit, _ := cmd.Flags().GetInt("limit")
	cursor, _ := cmd.Flags().GetString("cursor")
	return paging.Options{Limit: limit, Cursor: cursor}
}

// printNextCursor tells the user how to get the rest, on stderr so that
// piping a list into another tool doesn't pick the line up as data. A last
// page prints nothing, which is how "that was everything" is expressed.
func printNextCursor(cmd *cobra.Command, nextCursor string) {
	if nextCursor == "" {
		return
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "\nmore rows available — rerun with --cursor %s\n", nextCursor)
}
