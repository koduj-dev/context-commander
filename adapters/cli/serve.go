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
	"strings"
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
	"github.com/koduj-dev/context-commander/internal/directory"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/firstrun"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/members"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/quota"
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
	ldapURLEnv                = "CC_LDAP_URL"
	// The NAME of an environment variable, not a credential.
	ldapBindPasswordEnv = "CC_LDAP_BIND_PASSWORD" //nolint:gosec // G101: an env var name
	oidcIssuerEnv       = "CC_OIDC_ISSUER"
	// The NAME of an environment variable, not a credential.
	oidcClientSecretEnv = "CC_OIDC_CLIENT_SECRET" //nolint:gosec // G101: an env var name
	storageQuotaEnv     = "CC_STORAGE_QUOTA"
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
			plansSvc := plans.NewService(pool, cryptoProvider, authz)
			contextSvc.WithPlans(plansSvc)

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

			// Parsed before anything writes, and stated at startup either way:
			// "is anything limited here" should not need a database query to
			// answer (docs/adr/0021).
			quotaRaw, _ := cmd.Flags().GetString("storage-quota")
			if quotaRaw == "" {
				quotaRaw = os.Getenv(storageQuotaEnv)
			}
			storageQuota, err := quota.ParseSize(quotaRaw)
			if err != nil {
				return fmt.Errorf("serve: --storage-quota: %w", err)
			}
			pool.SetStorageQuotaDefault(storageQuota)
			if storageQuota > 0 {
				slog.Info("storage quota per namespace", "default_bytes", storageQuota)
			} else {
				slog.Info("no default storage quota: namespaces are unlimited unless an administrator sets a limit")
			}

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

			// LDAP, when an operator configured one. Built before the
			// server starts listening so a typo in a flag is a refusal to
			// start rather than a login screen that fails for everyone at
			// nine in the morning.
			ldapDir, err := ldapFromFlags(cmd)
			if err != nil {
				return err
			}
			if ldapDir != nil {
				slog.Info("LDAP sign-in enabled", "provider", ldapDir.Provider())
			}
			// OIDC, the same way. No network call here: discovery happens on
			// the first sign-in, so a provider that is down costs that
			// method rather than the whole server.
			var oidcProviders []directory.RedirectAuthenticator
			oidcDir, err := oidcFromFlags(cmd)
			if err != nil {
				return err
			}
			if err := distinctDirectoryNames(ldapDir, oidcDir); err != nil {
				return err
			}
			if oidcDir != nil {
				oidcProviders = append(oidcProviders, oidcDir)
				// Said at startup because it is the setting whose wrong value
				// nobody notices until strangers have accounts.
				slog.Info("OIDC sign-in enabled", "provider", oidcDir.Provider(), "label", oidcDir.Label(),
					"signup", oidcDir.AllowsSignup())
			}

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
				Installation: installationSvc, ContextPack: contextSvc, Plans: plansSvc,
				Security: securityRec, Limiter: limiter,
			}))
			mux.Handle("/", cchttp.NewHandler(cchttp.Deps{
				Pool: pool, Projects: projStore, Members: membersSvc, Authz: authz, Artifacts: artifactsSvc,
				Events: eventsSvc, Organizations: orgSvc, Audit: auditSvc,
				Handoffs: handoffsSvc, History: historySvc,
				Skills: skillsSvc, Gates: gatesAdmin, Secrets: secretsSvc,
				Installation: installationSvc, ContextPack: contextSvc, Plans: plansSvc,
				Security: securityRec, Limiter: limiter, SecurityEvents: securitySvc,
				FirstRun: firstRunCode,
				// Nil unless --ldap-url was given, and a nil Directory is what
				// closes the path rather than opening it.
				Directory: ldapDir,
				OIDC:      oidcProviders,
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
	cmd.Flags().String("storage-quota", "",
		fmt.Sprintf("default storage limit per user or organization, e.g. 10GB or 2GiB (defaults to $%s; empty means no limit — an administrator can still set one per namespace)", storageQuotaEnv))
	cmd.Flags().Bool("security-log", true,
		"record failed authentications, denied capabilities and rate-limit refusals to the security_events table "+
			"(--security-log=false opts out; nothing is recorded and attacks become invisible)")
	cmd.Flags().Float64("rate-limit-per-credential", 100,
		"sustained requests per second allowed per credential (0 disables) — deliberately per credential, not per IP: "+
			"X-Forwarded-For is not trusted, so behind a reverse proxy every caller would share one bucket")
	cmd.Flags().Int("rate-limit-burst", 200, "how far above the sustained per-credential rate a short spike may go")
	// LDAP (docs/adr/0019). --ldap-url is the switch: everything else is
	// ignored without it.
	cmd.Flags().String("ldap-url", "",
		fmt.Sprintf("enable LDAP sign-in against this directory, ldap://host:389 or ldaps://host:636 (defaults to $%s)", ldapURLEnv))
	cmd.Flags().String("ldap-name", "ldap", "provider name recorded against each linked identity — change it only if there is more than one directory")
	cmd.Flags().Bool("ldap-start-tls", false, "upgrade a plain ldap:// connection with StartTLS (ignored for ldaps://)")
	cmd.Flags().String("ldap-ca-cert", "", "PEM bundle to verify the directory's certificate against, for an internal CA — there is deliberately no way to skip verification")
	cmd.Flags().String("ldap-bind-dn", "", "service account to SEARCH as, e.g. cn=readonly,ou=services,dc=example,dc=test (empty searches anonymously)")
	// Deliberately not a --ldap-bind-password flag: an argument is visible in
	// `ps` to every user on the machine and lands in shell history. Same
	// reasoning as `ccd user set-password` reading from stdin.
	cmd.Flags().String("ldap-bind-password-file", "",
		fmt.Sprintf("file holding the bind password (defaults to $%s; there is no flag for the password itself, because an argument is visible in `ps`)", ldapBindPasswordEnv))
	cmd.Flags().String("ldap-user-base", "", "subtree to search for people, e.g. ou=people,dc=example,dc=test")
	cmd.Flags().String("ldap-user-filter", "",
		"filter selecting the entry, with %s where the identifier goes (default accepts either a uid or a mail address; the identifier is escaped before substitution)")
	cmd.Flags().String("ldap-mail-attr", "", `attribute holding the email address (default "mail")`)
	cmd.Flags().String("ldap-name-attr", "", `attribute holding the display name (default "displayName", falling back to cn)`)
	cmd.Flags().String("ldap-uid-attr", "",
		`attribute holding the directory's own stable id (default "entryUUID"; Active Directory uses objectGUID) — never the email, which a person can change`)
	cmd.Flags().Duration("ldap-timeout", 0, "bound on the whole exchange: dial, bind, search, bind again (default 10s)")
	// OIDC (docs/adr/0019). --oidc-issuer is the switch, as --ldap-url is.
	cmd.Flags().String("oidc-issuer", "",
		fmt.Sprintf("enable OpenID Connect sign-in with this issuer, exactly as its discovery document states it (defaults to $%s)", oidcIssuerEnv))
	cmd.Flags().String("oidc-client-id", "", "this installation's client id at the provider")
	// No --oidc-client-secret flag, for the reason there is no
	// --ldap-bind-password.
	cmd.Flags().String("oidc-client-secret-file", "",
		fmt.Sprintf("file holding the client secret (defaults to $%s; there is no flag for the secret itself, because an argument is visible in `ps`)", oidcClientSecretEnv))
	cmd.Flags().String("oidc-redirect-url", "",
		"this server's callback exactly as registered at the provider, e.g. https://cc.example.com/v1/auth/oidc/callback — not derived from the request, which behind a proxy names the proxy's choice of host")
	cmd.Flags().String("oidc-name", "oidc", "provider name recorded against each linked identity and used in the sign-in URL")
	cmd.Flags().String("oidc-label", "", `what the sign-in button says, e.g. "Google" or the company's name for its single sign-on (default "Single sign-on")`)
	cmd.Flags().StringSlice("oidc-scopes", nil, "scopes to request (default openid,profile,email — an ID token without an email cannot be provisioned)")
	cmd.Flags().String("oidc-ca-cert", "", "PEM bundle to verify the provider's certificate against, for an internal CA — there is deliberately no way to skip verification")
	cmd.Flags().Duration("oidc-timeout", 0, "bound on each request to the provider: discovery, key fetch, code exchange (default 10s)")
	// Off by default (docs/adr/0020): pointed at an issuer like Google, a
	// first sign-in that creates an account is open self-registration.
	cmd.Flags().Bool("oidc-allow-signup", false,
		"let a first sign-in create a local account; without it OIDC reaches only accounts that exist (linked already, or created by an administrator under the same address)")
	cmd.Flags().StringSlice("oidc-allowed-domains", nil,
		"accept OIDC sign-in only from verified addresses in these domains, e.g. example.com — checked on every sign-in, exact match, no subdomains")

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

// ldapFromFlags builds the LDAP directory, or returns nil when none is
// configured. --ldap-url (or $CC_LDAP_URL) is the switch; every other flag is
// ignored without it, rather than half-configuring something nobody asked for.
//
// It returns the interface, not *directory.LDAP, and that is the fix for a
// real outage: a nil *LDAP stored in Deps.Directory is a non-nil interface,
// so `d.Directory != nil` held on every server WITHOUT a directory, and both
// GET /v1/auth/methods and every mistyped password panicked into a 500.
func ldapFromFlags(cmd *cobra.Command) (directory.Authenticator, error) {
	url, _ := cmd.Flags().GetString("ldap-url")
	if url == "" {
		url = os.Getenv(ldapURLEnv)
	}
	if url == "" {
		return nil, nil
	}

	password, err := ldapBindPassword(cmd)
	if err != nil {
		return nil, err
	}

	name, _ := cmd.Flags().GetString("ldap-name")
	startTLS, _ := cmd.Flags().GetBool("ldap-start-tls")
	caCert, _ := cmd.Flags().GetString("ldap-ca-cert")
	bindDN, _ := cmd.Flags().GetString("ldap-bind-dn")
	userBase, _ := cmd.Flags().GetString("ldap-user-base")
	userFilter, _ := cmd.Flags().GetString("ldap-user-filter")
	mailAttr, _ := cmd.Flags().GetString("ldap-mail-attr")
	nameAttr, _ := cmd.Flags().GetString("ldap-name-attr")
	uidAttr, _ := cmd.Flags().GetString("ldap-uid-attr")
	timeout, _ := cmd.Flags().GetDuration("ldap-timeout")

	ldapDir, err := directory.NewLDAP(directory.LDAPConfig{
		Name: name, URL: url, StartTLS: startTLS, CACertFile: caCert,
		BindDN: bindDN, BindPassword: password,
		UserBase: userBase, UserFilter: userFilter,
		MailAttr: mailAttr, DisplayNameAttr: nameAttr, UniqueIDAttr: uidAttr,
		Timeout: timeout,
	})
	if err != nil {
		return nil, err
	}
	return ldapDir, nil
}

// oidcFromFlags builds the OIDC provider, or returns nil when none is
// configured. --oidc-issuer (or $CC_OIDC_ISSUER) is the switch.
//
// It returns the interface for the reason ldapFromFlags does.
func oidcFromFlags(cmd *cobra.Command) (directory.RedirectAuthenticator, error) {
	issuer, _ := cmd.Flags().GetString("oidc-issuer")
	if issuer == "" {
		issuer = os.Getenv(oidcIssuerEnv)
	}
	if issuer == "" {
		return nil, nil
	}

	secret := os.Getenv(oidcClientSecretEnv)
	if path, _ := cmd.Flags().GetString("oidc-client-secret-file"); path != "" {
		raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied path, same trust level as --tls-key
		if err != nil {
			return nil, fmt.Errorf("serve: read --oidc-client-secret-file: %w", err)
		}
		// Only the trailing newline, as for the LDAP bind password.
		secret = strings.TrimRight(string(raw), "\r\n")
	}

	clientID, _ := cmd.Flags().GetString("oidc-client-id")
	redirect, _ := cmd.Flags().GetString("oidc-redirect-url")
	name, _ := cmd.Flags().GetString("oidc-name")
	label, _ := cmd.Flags().GetString("oidc-label")
	scopes, _ := cmd.Flags().GetStringSlice("oidc-scopes")
	caCert, _ := cmd.Flags().GetString("oidc-ca-cert")
	timeout, _ := cmd.Flags().GetDuration("oidc-timeout")
	allowSignup, _ := cmd.Flags().GetBool("oidc-allow-signup")
	domains, _ := cmd.Flags().GetStringSlice("oidc-allowed-domains")

	oidcDir, err := directory.NewOIDC(directory.OIDCConfig{
		Name: name, Label: label, IssuerURL: issuer,
		ClientID: clientID, ClientSecret: secret, RedirectURL: redirect,
		Scopes: scopes, CACertFile: caCert, Timeout: timeout,
		AllowSignup: allowSignup, AllowedDomains: domains,
	})
	if err != nil {
		return nil, err
	}
	return oidcDir, nil
}

// distinctDirectoryNames refuses two directories under one name. The name is
// half of the user_identities key, so LDAP and OIDC sharing one would resolve
// each other's subjects to the same accounts.
func distinctDirectoryNames(ldap directory.Authenticator, oidc directory.RedirectAuthenticator) error {
	if ldap != nil && oidc != nil && ldap.Provider() == oidc.Provider() {
		return fmt.Errorf("serve: --oidc-name and --ldap-name are both %q; each directory needs its own name", oidc.Provider())
	}
	return nil
}

// ldapBindPassword reads the service account's password from a file or the
// environment, never from an argument.
func ldapBindPassword(cmd *cobra.Command) (string, error) {
	if path, _ := cmd.Flags().GetString("ldap-bind-password-file"); path != "" {
		raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied path, same trust level as --tls-key
		if err != nil {
			return "", fmt.Errorf("serve: read --ldap-bind-password-file: %w", err)
		}
		// Only the trailing newline an editor adds. A password may legitimately
		// end in a space, and trimming it would silently fail to authenticate
		// against a directory that has the right one.
		return strings.TrimRight(string(raw), "\r\n"), nil
	}
	return os.Getenv(ldapBindPasswordEnv), nil
}
