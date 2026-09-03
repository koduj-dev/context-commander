package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
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
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/secrets"
	"github.com/koduj-dev/context-commander/internal/skills"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
	"github.com/koduj-dev/context-commander/web"
)

const (
	tlsCertEnv                = "CC_TLS_CERT"
	tlsKeyEnv                 = "CC_TLS_KEY"
	insecureAllowCleartextEnv = "CC_INSECURE_ALLOW_CLEARTEXT"
)

// isLoopbackAddr reports whether an http.Server.Addr binds only to the local
// host — the one case where serving REST/MCP (bearer-token authenticated,
// docs/architecture-cookbook.md §25) in cleartext doesn't put credentials
// and decrypted project content on a network any other host can observe.
// Anything else (a wildcard bind, an empty host, a non-loopback hostname or
// IP) is untrusted here; TLS or an explicit override is required for it.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	switch host {
	case "localhost":
		return true
	case "":
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

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

			tlsCert, _ := cmd.Flags().GetString("tls-cert")
			if tlsCert == "" {
				tlsCert = os.Getenv(tlsCertEnv)
			}
			tlsKey, _ := cmd.Flags().GetString("tls-key")
			if tlsKey == "" {
				tlsKey = os.Getenv(tlsKeyEnv)
			}
			if (tlsCert == "") != (tlsKey == "") {
				return fmt.Errorf("serve: --tls-cert and --tls-key (or $%s/$%s) must both be set or both be empty", tlsCertEnv, tlsKeyEnv)
			}
			hasTLS := tlsCert != ""

			insecure, _ := cmd.Flags().GetBool("insecure-allow-cleartext")
			if !insecure && os.Getenv(insecureAllowCleartextEnv) == "true" {
				insecure = true
			}
			if !hasTLS && !isLoopbackAddr(addr) && !insecure {
				return fmt.Errorf(
					"serve: refusing to bind %q without TLS — bearer tokens and decrypted project content would travel in "+
						"plaintext to any host that can reach it (docs/architecture-cookbook.md §25 expects TLS in transit). "+
						"Configure --tls-cert/--tls-key (or $%s/$%s), bind a loopback address behind a TLS-terminating "+
						"reverse proxy instead, or pass --insecure-allow-cleartext (or $%s=true) to override",
					addr, tlsCertEnv, tlsKeyEnv, insecureAllowCleartextEnv,
				)
			}

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
			if err := gates.ValidateRegisteredBindings(cmd.Context(), pool); err != nil {
				return err
			}
			artifactsSvc := artifacts.NewService(pool, cryptoProvider, store, authz, gatesEngine)

			orgStore := organizations.NewStore()
			eventsSvc := events.NewService(pool, authz)
			orgSvc := organizations.NewService(pool, orgStore)
			auditSvc := audit.NewService(pool, authz)
			handoffsSvc := handoffs.NewService(pool, cryptoProvider, authz)
			historySvc := history.NewService(pool, cryptoProvider, authz)
			installationStore := installation.NewStore()
			skillsSvc := skills.NewService(pool, authz, orgStore, projStore, store, installationStore)
			gatesAdmin := gates.NewAdminService(pool, authz, orgStore, projStore, installationStore)
			installationSvc := installation.NewService(pool, installationStore, identity.NewStore())
			secretsBackend := secrets.NewLocalEncryptedBackend(pool, cryptoProvider)
			secretsSvc := secrets.NewService(pool, authz, secretsBackend)

			dev, _ := cmd.Flags().GetBool("dev")
			var webFS fs.FS
			if !dev {
				// In dev mode the UI is served separately by `cd web && npm
				// run dev` (Vite, proxying /v1 back here) — mounting the
				// (placeholder, unbuilt) embedded FS here too would just
				// shadow it. See web/vite.config.ts.
				webFS, err = web.DistFS()
				if err != nil {
					return err
				}
			}

			mux := http.NewServeMux()
			mux.Handle("/mcp", ccmcp.NewHandler(ccmcp.Deps{
				Pool: pool, Projects: projStore, Authz: authz, Artifacts: artifactsSvc,
				Events: eventsSvc, Organizations: orgSvc, Audit: auditSvc,
				Handoffs: handoffsSvc, History: historySvc,
				Skills: skillsSvc, Gates: gatesAdmin, Secrets: secretsSvc,
				Installation: installationSvc,
			}))
			mux.Handle("/", cchttp.NewHandler(cchttp.Deps{
				Pool: pool, Projects: projStore, Authz: authz, Artifacts: artifactsSvc,
				Events: eventsSvc, Organizations: orgSvc, Audit: auditSvc,
				Handoffs: handoffsSvc, History: historySvc,
				Skills: skillsSvc, Gates: gatesAdmin, Secrets: secretsSvc,
				Installation: installationSvc,
				WebFS:        webFS,
			}))

			server := &http.Server{
				Addr:              addr,
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      60 * time.Second,
				IdleTimeout:       120 * time.Second,
			}

			serverErr := make(chan error, 1)
			scheme := "http"
			if hasTLS {
				scheme = "https"
				go func() { serverErr <- server.ListenAndServeTLS(tlsCert, tlsKey) }()
			} else {
				go func() { serverErr <- server.ListenAndServe() }()
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "listening on %s://%s — REST at /v1, MCP at /mcp\n", scheme, addr)

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
	cmd.Flags().String("addr", "127.0.0.1:8080", "listen address — a non-loopback address requires --tls-cert/--tls-key or --insecure-allow-cleartext")
	cmd.Flags().String("storage-dir", "", fmt.Sprintf("filesystem ObjectStore root (defaults to $%s, or %q)", storageDirEnv, defaultStorageDir))
	cmd.Flags().String("tls-cert", "", fmt.Sprintf("TLS certificate file, enables HTTPS (defaults to $%s)", tlsCertEnv))
	cmd.Flags().String("tls-key", "", fmt.Sprintf("TLS private key file, enables HTTPS (defaults to $%s)", tlsKeyEnv))
	cmd.Flags().Bool("insecure-allow-cleartext", false,
		fmt.Sprintf("allow binding a non-loopback --addr without TLS — bearer tokens and content travel in plaintext (defaults to $%s=true)", insecureAllowCleartextEnv))
	cmd.Flags().Bool("dev", false, "skip mounting the embedded web UI — run 'cd web && npm run dev' separately, proxying /v1 back here")
	databaseURLFlag(cmd)
	return cmd
}
