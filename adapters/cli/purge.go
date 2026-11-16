package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/retention"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

// ownerTools opens what an operator's retention command needs: a cc_owner
// connection and the server's object store.
func ownerTools(cmd *cobra.Command) (*db.Pool, *retention.Purger, error) {
	dsn, err := resolveDatabaseURL(cmd)
	if err != nil {
		return nil, nil, err
	}
	pool, err := db.NewPool(cmd.Context(), dsn)
	if err != nil {
		return nil, nil, err
	}
	storageDir, _ := cmd.Flags().GetString("storage-dir")
	if storageDir == "" {
		storageDir = os.Getenv(storageDirEnv)
	}
	if storageDir == "" {
		storageDir = defaultStorageDir
	}
	store, err := filesystem.New(storageDir)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return pool, retention.NewPurger(pool, store), nil
}

// projectIDByPath finds any project by path, scheduled for deletion or not:
// an operator's connection sees past row-level security.
func projectIDByPath(ctx context.Context, pool *db.Pool, path string) (uuid.UUID, error) {
	ns, slug, ok := strings.Cut(path, "/")
	if !ok {
		return uuid.Nil, fmt.Errorf("project path %q is not namespace/slug", path)
	}
	var id uuid.UUID
	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT p.id FROM projects p JOIN namespaces n ON n.id = p.namespace_id WHERE n.slug = $1 AND p.slug = $2`,
			ns, slug).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("no project %s", path)
	}
	return id, err
}

func storageFlags(cmd *cobra.Command) {
	cmd.Flags().String("storage-dir", "", fmt.Sprintf("filesystem ObjectStore root (defaults to $%s, or %q)", storageDirEnv, defaultStorageDir))
	databaseURLFlag(cmd)
}

func newPurgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Remove what is due: projects past their deletion grace period, and long-deleted artifacts (as cc_owner)",
		Long: "Purges every project whose scheduled deletion is past its grace period, and the stored versions of " +
			"artifacts deleted more than --deleted-artifacts-after days ago. Projects under legal hold are left alone. " +
			"Connect as cc_owner with the server's storage directory; run it daily from cron or a systemd timer " +
			"(docs/adr/0036). --dry-run says what it would remove.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			days, _ := cmd.Flags().GetInt("deleted-artifacts-after")
			dry, _ := cmd.Flags().GetBool("dry-run")
			pool, purger, err := ownerTools(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			r, err := purger.Run(cmd.Context(), days, dry)
			verb := "purged"
			if dry {
				verb = "would purge"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %d projects %v, %d deleted artifacts, %d stored objects; %d due projects held\n",
				verb, len(r.Projects), r.Projects, r.Artifacts, r.Objects, r.HeldSkips)
			return err
		},
	}
	cmd.Flags().Int("deleted-artifacts-after", retention.DefaultDeletedArtifactDays, "days a deleted artifact's versions stay before they are purged; 0 keeps them forever")
	cmd.Flags().Bool("dry-run", false, "only report what would be purged")
	storageFlags(cmd)
	return cmd
}

func newProjectPurgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Remove a project and everything in it now, whatever its state (as cc_owner)",
		Long: "Deletes the project's rows and stored objects at once, without waiting for a scheduled deletion — " +
			"for an import that stopped part way, or a deletion that cannot wait. Refused under legal hold. " +
			"Cannot be undone: pass --yes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("project")
			yes, _ := cmd.Flags().GetBool("yes")
			if path == "" {
				return errors.New("--project is required")
			}
			if !yes {
				return errors.New("this deletes the project for good; pass --yes")
			}
			pool, purger, err := ownerTools(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			id, err := projectIDByPath(cmd.Context(), pool, path)
			if err != nil {
				return err
			}
			n, err := purger.PurgeProject(cmd.Context(), id, "purged by an operator")
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "purged %s (id=%s) and %d stored objects\n", path, id, n)
			return nil
		},
	}
	cmd.Flags().String("project", "", `project path "namespace/slug" (required)`)
	cmd.Flags().Bool("yes", false, "confirm that the project is to be deleted for good")
	storageFlags(cmd)
	return cmd
}

func newProjectHoldCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hold",
		Short: "Place or lift a legal hold on a project (as cc_owner)",
		Long: "A project under legal hold cannot be scheduled for deletion and is skipped by every purge, " +
			"deleted artifacts included. Placing a hold cancels a scheduled deletion. --release lifts it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("project")
			reason, _ := cmd.Flags().GetString("reason")
			release, _ := cmd.Flags().GetBool("release")
			if path == "" || (!release && reason == "") {
				return errors.New("--project is required, and --reason unless --release")
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
			id, err := projectIDByPath(cmd.Context(), pool, path)
			if err != nil {
				return err
			}
			if err := retention.SetLegalHold(cmd.Context(), pool, id, !release, reason); err != nil {
				return err
			}
			state := "placed on"
			if release {
				state = "lifted from"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "legal hold %s %s\n", state, path)
			return nil
		},
	}
	cmd.Flags().String("project", "", `project path "namespace/slug" (required)`)
	cmd.Flags().String("reason", "", "why the project is held (required to place a hold)")
	cmd.Flags().Bool("release", false, "lift the hold")
	databaseURLFlag(cmd)
	return cmd
}
