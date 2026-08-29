package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
	ccmcp "github.com/koduj-dev/context-commander/adapters/mcp"
	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/secrets"
	"github.com/koduj-dev/context-commander/internal/skills"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

// newServeCmd runs the REST API (at "/") and the MCP server (at "/mcp") on
// one listener, both backed by the exact same domain services — see
// adapters/http and adapters/mcp's package docs: neither adapter carries
// its own business logic, so serving both is just wiring one Deps struct
// twice.
func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP REST API and MCP server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn, err := resolveDatabaseURL(cmd)
			if err != nil {
				return err
			}
			addr, _ := cmd.Flags().GetString("addr")

			storageDir, _ := cmd.Flags().GetString("storage-dir")
			if storageDir == "" {
				storageDir = os.Getenv(storageDirEnv)
			}
			if storageDir == "" {
				storageDir = defaultStorageDir
			}
			if err := os.MkdirAll(storageDir, 0o750); err != nil { //nolint:gosec // operator-supplied --storage-dir/$CC_STORAGE_DIR, not untrusted network input
				return fmt.Errorf("create storage dir %q: %w", storageDir, err)
			}
			store, err := filesystem.New(storageDir)
			if err != nil {
				return err
			}

			pool, err := db.NewPool(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			root, err := crypto.NewEnvRootKeyProvider(rootKeyEnv)
			if err != nil {
				return err
			}
			cryptoProvider := crypto.NewProvider(root)
			projStore := projects.NewStore()
			authz := auth.NewAuthzService(pool, projStore)
			gatesEngine := gates.NewEngine(pool)
			artifactsSvc := artifacts.NewService(pool, cryptoProvider, store, authz, gatesEngine)

			orgStore := organizations.NewStore()
			eventsSvc := events.NewService(pool, authz)
			orgSvc := organizations.NewService(pool, orgStore)
			auditSvc := audit.NewService(pool, authz)
			handoffsSvc := handoffs.NewService(pool, cryptoProvider, authz)
			historySvc := history.NewService(pool, cryptoProvider, authz)
			skillsSvc := skills.NewService(pool, authz, orgStore, store)
			gatesAdmin := gates.NewAdminService(pool, authz, orgStore)
			secretsBackend := secrets.NewLocalEncryptedBackend(pool, cryptoProvider)
			secretsSvc := secrets.NewService(pool, authz, secretsBackend)

			mux := http.NewServeMux()
			mux.Handle("/mcp", ccmcp.NewHandler(ccmcp.Deps{
				Pool: pool, Projects: projStore, Artifacts: artifactsSvc,
				Events: eventsSvc, Organizations: orgSvc, Audit: auditSvc,
				Handoffs: handoffsSvc, History: historySvc,
				Skills: skillsSvc, Gates: gatesAdmin, Secrets: secretsSvc,
			}))
			mux.Handle("/", cchttp.NewHandler(cchttp.Deps{
				Pool: pool, Projects: projStore, Artifacts: artifactsSvc,
				Events: eventsSvc, Organizations: orgSvc, Audit: auditSvc,
				Handoffs: handoffsSvc, History: historySvc,
				Skills: skillsSvc, Gates: gatesAdmin, Secrets: secretsSvc,
			}))

			server := &http.Server{
				Addr:              addr,
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
			}

			serverErr := make(chan error, 1)
			go func() { serverErr <- server.ListenAndServe() }()
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "listening on %s — REST at /v1, MCP at /mcp\n", addr)

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			select {
			case err := <-serverErr:
				if err != nil && !errors.Is(err, http.ErrServerClosed) {
					return fmt.Errorf("serve: %w", err)
				}
				return nil
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "shutting down...")
				return server.Shutdown(shutdownCtx)
			}
		},
	}
	cmd.Flags().String("addr", ":8080", "listen address")
	cmd.Flags().String("storage-dir", "", fmt.Sprintf("filesystem ObjectStore root (defaults to $%s, or %q)", storageDirEnv, defaultStorageDir))
	databaseURLFlag(cmd)
	return cmd
}
