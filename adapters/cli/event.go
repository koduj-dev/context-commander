package cli

import (
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
)

func newEventCmd() *cobra.Command {
	eventCmd := &cobra.Command{Use: "event", Short: "Read the project event outbox"}
	eventCmd.AddCommand(newEventListCmd())
	return eventCmd
}

func newEventListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List events for a project after a given sequence number",
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectPath, _ := cmd.Flags().GetString("project")
			if projectPath == "" {
				return fmt.Errorf("--project is required")
			}
			afterSeq, _ := cmd.Flags().GetInt64("after-seq")

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

			projStore := projects.NewStore()
			var project projects.Project
			err = db.WithPrincipal(cmd.Context(), pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
				var err error
				project, err = projStore.Resolve(cmd.Context(), tx, projectPath)
				return err
			})
			if err != nil {
				return fmt.Errorf("resolve project %q: %w", projectPath, err)
			}

			authz := auth.NewAuthzService(pool, projStore)
			svc := events.NewService(pool, authz)

			// --after-seq is kept as the ergonomic front door: every line
			// of output starts with the event's seq, so resuming from one
			// you just read should not require a cursor you have to save.
			// It is translated into the same cursor the rest of the API
			// uses; an explicit --cursor wins if both are given.
			opts := pagingOptionsFrom(cmd)
			if opts.Cursor == "" && afterSeq > 0 {
				opts.Cursor = paging.EncodeSeq(afterSeq)
			}
			page, err := svc.List(cmd.Context(), principalID, project.ID, opts)
			if err != nil {
				return err
			}
			for _, e := range page.Items {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%d\t%s\t%s\t%s\n", e.Seq, e.EventID, e.Type, e.ResourceType)
			}
			printNextCursor(cmd, page.NextCursor)
			return nil
		},
	}
	cmd.Flags().String("project", "", `project path "namespace/slug" (required)`)
	cmd.Flags().Int64("after-seq", 0, "only list events with seq greater than this (shorthand for --cursor)")
	pagingFlags(cmd)
	databaseURLFlag(cmd)
	tokenFlag(cmd)
	return cmd
}
