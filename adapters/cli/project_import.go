package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/ideas"
	"github.com/koduj-dev/context-commander/internal/memories"
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/portability"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

func newProjectImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a project archive into a new project (as cc_owner)",
		Long: "Creates --project, owned by the user with --owner's email, and writes the archive's content into it, " +
			"encrypted under the new project's key (docs/adr/0035). Authors are mapped to users of this installation " +
			"by email; anyone not found is attributed to the owner. Connect as cc_owner (the role `ccd migrate` uses), " +
			"with the server's $CC_ROOT_KEY and storage directory. Secrets are not in an archive: set them again afterwards.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			file, _ := cmd.Flags().GetString("file")
			path, _ := cmd.Flags().GetString("project")
			ownerEmail, _ := cmd.Flags().GetString("owner")
			name, _ := cmd.Flags().GetString("name")
			if file == "" || path == "" || ownerEmail == "" {
				return errors.New("--file, --project and --owner are required")
			}
			f, err := os.Open(file) //nolint:gosec // operator-supplied archive path
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			files, manifest, err := portability.ReadArchive(f)
			if err != nil {
				return err
			}
			if err := portability.Validate(files); err != nil {
				return err
			}
			if name == "" {
				name = manifest.Project.Name
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
			root, err := crypto.LoadRootKey(cmd.Context(), rootKeyEnv)
			if err != nil {
				return err
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
				return err
			}

			projStore := projects.NewStore()
			var ownerUser, ownerPrincipal uuid.UUID
			var project projects.Project
			err = db.WithoutPrincipal(cmd.Context(), pool, func(tx pgx.Tx) error {
				if err := tx.QueryRow(cmd.Context(),
					`SELECT u.id, p.id FROM users u JOIN principals p ON p.user_id = u.id WHERE lower(u.email) = lower($1)`,
					ownerEmail).Scan(&ownerUser, &ownerPrincipal); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return fmt.Errorf("no user with the email %s in this installation", ownerEmail)
					}
					return err
				}
				return nil
			})
			if err != nil {
				return err
			}
			err = db.WithPrincipal(cmd.Context(), pool, db.Principal{ID: ownerPrincipal}, func(tx pgx.Tx) error {
				var err error
				project, err = projStore.CreateForOwner(cmd.Context(), tx, ownerUser, ownerPrincipal, path, name)
				return err
			})
			if err != nil {
				return fmt.Errorf("create %s: %w", path, err)
			}

			cp := crypto.NewProvider(root)
			authz := auth.NewAuthzService(pool, projStore)
			plansSvc := plans.NewService(pool, cp, authz)
			svc := portability.NewService(pool, authz,
				artifacts.NewService(pool, cp, store, authz, gates.NewEngine(pool)),
				memories.NewService(pool, cp, authz), plansSvc, ideas.NewService(pool, cp, authz, plansSvc),
				handoffs.NewService(pool, cp, authz), history.NewService(pool, cp, authz))
			report, err := svc.Import(cmd.Context(), ownerPrincipal, project.ID, files)
			out := cmd.OutOrStdout()
			if err != nil {
				_, _ = fmt.Fprintf(out, "%s was created, and the import stopped part way: it holds what was written before the error\n", path)
				return err
			}
			kinds := make([]string, 0, len(report.Counts))
			for k, n := range report.Counts {
				kinds = append(kinds, fmt.Sprintf("%d %s", n, k))
			}
			sort.Strings(kinds)
			_, _ = fmt.Fprintf(out, "imported %s from %s into %s (id=%s), owned by %s: %s\n",
				manifest.Project.Path, file, path, project.ID, ownerEmail, strings.Join(kinds, ", "))
			_, _ = fmt.Fprintf(out, "authors: %d found here by email, %d attributed to %s; set secrets again, they are not in an archive\n",
				report.Mapped, report.Unmapped, ownerEmail)
			return nil
		},
	}
	cmd.Flags().String("file", "", "the .ccproject.tar.gz archive (required)")
	cmd.Flags().String("project", "", `the new project's path "namespace/slug" (required)`)
	cmd.Flags().String("owner", "", "email of the user who will own the project (required)")
	cmd.Flags().String("name", "", "display name (defaults to the archive's)")
	cmd.Flags().String("storage-dir", "", fmt.Sprintf("filesystem ObjectStore root (defaults to $%s, or %q)", storageDirEnv, defaultStorageDir))
	databaseURLFlag(cmd)
	return cmd
}
