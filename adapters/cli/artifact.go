package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

const (
	storageDirEnv     = "CC_STORAGE_DIR"
	rootKeyEnv        = "CC_ROOT_KEY"
	defaultStorageDir = "./data/objects"
)

func newArtifactCmd() *cobra.Command {
	artifactCmd := &cobra.Command{
		Use:   "artifact",
		Short: "Write, read, and list artifacts — a CLI smoke-test path ahead of the HTTP/MCP adapters (Phase 4)",
	}
	artifactCmd.AddCommand(newArtifactWriteCmd(), newArtifactReadCmd(), newArtifactListCmd())
	return artifactCmd
}

func projectAndStorageFlags(cmd *cobra.Command) {
	cmd.Flags().String("project", "", `project path "namespace/slug", e.g. "filip/es-migration" (required)`)
	cmd.Flags().String("storage-dir", "", fmt.Sprintf("filesystem ObjectStore root (defaults to $%s, or %q)", storageDirEnv, defaultStorageDir))
	databaseURLFlag(cmd)
	tokenFlag(cmd)
}

// artifactService wires together everything a CLI artifact subcommand
// needs: authenticates the token, resolves the project, and constructs the
// same internal/artifacts.Service the future HTTP/MCP adapters will use —
// no logic lives in this package beyond flag parsing and I/O.
type artifactContext struct {
	svc         *artifacts.Service
	principalID uuid.UUID
	clientID    uuid.UUID
	project     projects.Project
	pool        *db.Pool
}

func setupArtifactContext(cmd *cobra.Command) (*artifactContext, error) {
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

	principalID, clientID, err := authenticate(cmd.Context(), pool, token)
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

	storageDir, _ := cmd.Flags().GetString("storage-dir")
	if storageDir == "" {
		storageDir = os.Getenv(storageDirEnv)
	}
	if storageDir == "" {
		storageDir = defaultStorageDir
	}
	if err := os.MkdirAll(storageDir, 0o750); err != nil { //nolint:gosec // operator-supplied --storage-dir/$CC_STORAGE_DIR, not untrusted network input
		return nil, fmt.Errorf("create storage dir %q: %w", storageDir, err)
	}
	store, err := filesystem.New(storageDir)
	if err != nil {
		return nil, err
	}

	root, err := crypto.NewEnvRootKeyProvider(rootKeyEnv)
	if err != nil {
		return nil, err
	}
	cryptoProvider := crypto.NewProvider(root)
	authz := auth.NewAuthzService(pool, projStore)
	svc := artifacts.NewService(pool, cryptoProvider, store, authz, gates.NewEngine(pool))

	return &artifactContext{
		svc: svc, principalID: principalID, clientID: clientID, project: project, pool: pool,
	}, nil
}

func newArtifactWriteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write a new artifact version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ac, err := setupArtifactContext(cmd)
			if err != nil {
				return err
			}
			defer ac.pool.Close()

			path, _ := cmd.Flags().GetString("path")
			contentType, _ := cmd.Flags().GetString("content-type")
			file, _ := cmd.Flags().GetString("file")
			if path == "" {
				return fmt.Errorf("--path is required")
			}

			var content []byte
			if file == "" || file == "-" {
				content, err = io.ReadAll(cmd.InOrStdin())
			} else {
				content, err = os.ReadFile(file) //nolint:gosec // operator-supplied CLI path, not untrusted input
			}
			if err != nil {
				return fmt.Errorf("read content: %w", err)
			}

			expectedVersion, err := resolveExpectedVersion(cmd, ac)
			if err != nil {
				return err
			}

			v, err := ac.svc.Write(cmd.Context(), ac.principalID, &ac.clientID, ac.project.ID, path, contentType, content, expectedVersion)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s/%s v%d (content_hash=%s, %d bytes)\n",
				ac.project.Slug, path, v.Version, v.ContentHash, v.SizeBytes)
			return nil
		},
	}
	cmd.Flags().String("path", "", "logical artifact path, e.g. notes/architecture.md (required)")
	cmd.Flags().String("content-type", "text/plain", "MIME content type")
	cmd.Flags().String("file", "", "file to read content from (default: stdin)")
	cmd.Flags().Int("expected-version", -1, "expected current version (default: auto-detected via Stat)")
	projectAndStorageFlags(cmd)
	return cmd
}

// resolveExpectedVersion auto-fills --expected-version from a live Stat call
// when the flag wasn't passed — nicer for interactive CLI use than making
// every write require the caller to have tracked the version themselves,
// while still going through the same atomic conflict check in Service.Write
// if it's stale by the time the write actually happens.
func resolveExpectedVersion(cmd *cobra.Command, ac *artifactContext) (int, error) {
	if v, _ := cmd.Flags().GetInt("expected-version"); v >= 0 {
		return v, nil
	}
	path, _ := cmd.Flags().GetString("path")
	stat, err := ac.svc.Stat(cmd.Context(), ac.principalID, ac.project.ID, path)
	if err != nil {
		if errors.Is(err, artifacts.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return stat.CurrentVersion, nil
}

func newArtifactReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read",
		Short: "Read an artifact (latest version, or a specific one with --version)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ac, err := setupArtifactContext(cmd)
			if err != nil {
				return err
			}
			defer ac.pool.Close()

			path, _ := cmd.Flags().GetString("path")
			if path == "" {
				return fmt.Errorf("--path is required")
			}
			var version *int
			if v, _ := cmd.Flags().GetInt("version"); v > 0 {
				version = &v
			}

			out, _ := cmd.Flags().GetString("output")

			v, content, err := ac.svc.Read(cmd.Context(), ac.principalID, ac.project.ID, path, version)
			if err != nil {
				return err
			}

			if out == "" || out == "-" {
				_, err = cmd.OutOrStdout().Write(content)
				return err
			}
			if err := os.WriteFile(out, content, 0o600); err != nil {
				return fmt.Errorf("write %q: %w", out, err)
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s/%s v%d (%d bytes) to %s\n", ac.project.Slug, path, v.Version, len(content), out)
			return nil
		},
	}
	cmd.Flags().String("path", "", "logical artifact path (required)")
	cmd.Flags().Int("version", 0, "specific version to read (default: latest)")
	cmd.Flags().String("output", "", "file to write content to (default: stdout)")
	projectAndStorageFlags(cmd)
	return cmd
}

func newArtifactListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List non-deleted artifacts in a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ac, err := setupArtifactContext(cmd)
			if err != nil {
				return err
			}
			defer ac.pool.Close()

			page, err := ac.svc.List(cmd.Context(), ac.principalID, ac.project.ID, pagingOptionsFrom(cmd))
			if err != nil {
				return err
			}
			if len(page.Items) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no artifacts)")
				return nil
			}
			for _, a := range page.Items {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-40s v%-4d %-24s updated %s\n",
					a.LogicalPath, a.CurrentVersion, a.ContentType, a.UpdatedAt.Format(time.RFC3339))
			}
			printNextCursor(cmd, page.NextCursor)
			return nil
		},
	}
	projectAndStorageFlags(cmd)
	pagingFlags(cmd)
	return cmd
}
