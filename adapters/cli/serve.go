package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
	ccmcp "github.com/koduj-dev/context-commander/adapters/mcp"
	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/contextpack"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/firstrun"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/members"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
	"github.com/koduj-dev/context-commander/internal/secrets"
	"github.com/koduj-dev/context-commander/internal/security"
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

			membersSvc := members.NewService(pool, projStore, authz)
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
			// Constructed last: it composes the services above and owns no
			// storage of its own (internal/contextpack).
			contextSvc := contextpack.NewService(pool, projStore, authz, artifactsSvc, skillsSvc, handoffsSvc, eventsSvc)

			// Structured logs to stderr. JSON by default: these lines are
			// meant to be shipped and queried (CRA Annex I expects security
			// information to be recordable and monitorable), and a human
			// reading them live can pass --log-format=text.
			logFormat, _ := cmd.Flags().GetString("log-format")
			var handler slog.Handler
			if logFormat == "text" {
				handler = slog.NewTextHandler(os.Stderr, nil)
			} else {
				handler = slog.NewJSONHandler(os.Stderr, nil)
			}
			slog.SetDefault(slog.New(handler))

			securityLog, _ := cmd.Flags().GetBool("security-log")
			if !securityLog {
				// Said out loud, once, at startup: an operator who turns
				// this off should see the consequence stated rather than
				// discover it during an incident.
				slog.Warn("security event recording is DISABLED — failed authentications, denied capabilities " +
					"and rate-limit refusals will not be recorded or queryable")
			}
			securityRec := security.NewRecorder(pool, securityLog)
			// Every ALLOW/DENY decision for project content goes through
			// authz (docs/adr/0006), so one hook covers every domain.
			authz.SetDenyRecorder(security.NewDenyRecorder(securityRec))
			securitySvc := security.NewService(pool, installationStore)

			perCredential, _ := cmd.Flags().GetFloat64("rate-limit-per-credential")
			burst, _ := cmd.Flags().GetInt("rate-limit-burst")
			maxAuth, _ := cmd.Flags().GetInt("max-concurrent-auth")
			signInsPerMinute, _ := cmd.Flags().GetFloat64("rate-limit-signin-per-minute")
			signInBurst, _ := cmd.Flags().GetInt("rate-limit-signin-burst")
			maxHash, _ := cmd.Flags().GetInt("max-concurrent-password-hash")
			limiter := ratelimit.New(ratelimit.Limits{
				PerCredential:     perCredential,
				Burst:             burst,
				MaxConcurrentAuth: maxAuth,
				// Expressed per minute on the flag because that is how a
				// human reasons about login attempts, and converted here
				// because that is how a token bucket is configured.
				PerSignIn:                 signInsPerMinute / 60,
				SignInBurst:               signInBurst,
				MaxConcurrentPasswordHash: maxHash,
			})

			// First run: when the installation has no users at all, mint a
			// one-time setup code so the first administrator can be created
			// in the browser instead of through a shell on this host
			// (docs/adr/0019). Held in memory only, so a restart replaces it.
			var firstRunCode *firstrun.Code
			hasUser, err := hasAnyUser(cmd.Context(), pool)
			if err != nil {
				return err
			}
			if !hasUser {
				if firstRunCode, err = firstrun.NewCode(); err != nil {
					return err
				}
				// Deliberately on the logger every deployment watches rather
				// than written to a file: reading this is the one thing the
				// code asks of whoever is allowed to claim the instance.
				slog.Warn("this installation has no users yet — open the web UI and use this one-time setup code to create the first administrator",
					"setup_code", firstRunCode.String())
			}

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
				Pool: pool, Projects: projStore, Members: membersSvc, Authz: authz, Artifacts: artifactsSvc,
				Events: eventsSvc, Organizations: orgSvc, Audit: auditSvc,
				Handoffs: handoffsSvc, History: historySvc,
				Skills: skillsSvc, Gates: gatesAdmin, Secrets: secretsSvc,
				Installation: installationSvc, ContextPack: contextSvc,
				Security: securityRec, Limiter: limiter,
			}))
			mux.Handle("/", cchttp.NewHandler(cchttp.Deps{
				Pool: pool, Projects: projStore, Members: membersSvc, Authz: authz, Artifacts: artifactsSvc,
				Events: eventsSvc, Organizations: orgSvc, Audit: auditSvc,
				Handoffs: handoffsSvc, History: historySvc,
				Skills: skillsSvc, Gates: gatesAdmin, Secrets: secretsSvc,
				Installation: installationSvc, ContextPack: contextSvc,
				Security: securityRec, Limiter: limiter, SecurityEvents: securitySvc,
				FirstRun: firstRunCode,
				// The cookie is Secure whenever the transport is: TLS here,
				// or a reverse proxy in front of a loopback bind. Only an
				// explicit --insecure-allow-cleartext turns it off, because
				// a Secure cookie over plain HTTP is dropped without a word
				// and sign-in would look like it worked.
				SessionCookieSecure: !insecure,
				WebFS:               webFS,
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
	cmd.Flags().String("log-format", "json", `structured log format: "json" or "text"`)
	cmd.Flags().Bool("security-log", true,
		"record failed authentications, denied capabilities and rate-limit refusals to the security_events table "+
			"(--security-log=false opts out; nothing is recorded and attacks become invisible)")
	cmd.Flags().Float64("rate-limit-per-credential", 100,
		"sustained requests per second allowed per credential (0 disables) — deliberately per credential, not per IP: "+
			"X-Forwarded-For is not trusted, so behind a reverse proxy every caller would share one bucket")
	cmd.Flags().Int("rate-limit-burst", 200, "how far above the sustained per-credential rate a short spike may go")
	cmd.Flags().Float64("rate-limit-signin-per-minute", 5,
		"sustained password sign-in attempts per minute allowed for one email address (0 disables) — the per-credential limit cannot see these, since a failed guess belongs to no credential")
	cmd.Flags().Int("rate-limit-signin-burst", 5, "how many sign-in attempts for one address may arrive together")
	cmd.Flags().Int("max-concurrent-password-hash", 4,
		"maximum argon2id verifications in flight (0 disables) — each holds 64 MiB, so this bounds the memory an unauthenticated caller can reserve")
	cmd.Flags().Int("max-concurrent-auth", 64,
		"maximum authentication attempts in flight at once (0 disables) — bounds the database work an "+
			"unauthenticated caller can trigger")
	databaseURLFlag(cmd)
	return cmd
}

// hasAnyUser answers whether this installation has been set up, for deciding
// whether to mint a first-run setup code. Asked once at startup rather than
// per request: the answer only ever goes from false to true, and the HTTP
// endpoint re-checks it anyway before creating anything.
func hasAnyUser(ctx context.Context, pool *db.Pool) (bool, error) {
	var hasUser bool
	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		hasUser, err = identity.NewStore().HasAnyUser(ctx, tx)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("serve: check for users: %w", err)
	}
	return hasUser, nil
}
