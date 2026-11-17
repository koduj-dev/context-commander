# Essential requirements: Annex I, Part I

Each requirement of Annex I, Part I of the CRA, and how Context Commander
meets it, with where to check. Status as of 2026-11-16.

**(1) Designed, developed and produced to ensure an appropriate level of
cybersecurity based on the risks.** The design was settled in the
architecture cookbook and the ADRs before it was built, and went through
two rounds of external architecture review (see `docs/adr/`). The risks it
answers are in [risk-assessment.md](risk-assessment.md). Every pull request
runs tests against a real Postgres and object store, `golangci-lint`
(including gosec), CodeQL code scanning and gitleaks, and needs green checks to merge into
the protected `main` branch.

## (2) On the basis of the risk assessment

**(a) Made available without known exploitable vulnerabilities.**
Dependabot alerts and update pull requests cover Go modules, npm, GitHub
Actions and the container's base images (`.github/dependabot.yml`), and
GitHub code scanning (CodeQL) analyses every pull request. High and
critical alerts are fixed before a release without waiting for a decision.
A release is cut from `main` only with every check green. *Open:* no
dependency scan blocks the release job itself; the gate is the maintainer
checking the alert list (see [vulnerability-handling.md](vulnerability-handling.md)).

**(b) Secure by default configuration, with a way to return to it.**
- `ccd serve` refuses to bind a non-loopback address without TLS unless the
  operator passes `--insecure-allow-cleartext` (`adapters/cli/serve.go`).
- There is no default root key: compose refuses to start without
  `CC_ROOT_KEY`, and the server refuses to start with one that wraps none
  of the project keys (ADR 0005, `crypto.CheckRootKey`).
- The first account is created with a one-time setup code printed to the
  server log; nobody can claim an empty installation from the network.
- OIDC sign-up is off unless `--oidc-allow-signup` (ADR 0020); local
  passwords use argon2id (ADR 0019).
- The security log is on by default (`--security-log`).
- The container runs as an unprivileged user on a distroless base image;
  the packaged service runs as its own user with systemd hardening, and
  migrations run under a separate dynamic user with the owner's credentials
  (`deploy/packaging/`).
- *Reset:* configuration lives in environment files and flags, documented
  with their defaults (`docs/install.md`, `ccd serve --help`); removing an
  override returns to the default.

**(c) Vulnerabilities can be addressed through security updates.** Updates
ship as signed releases (binaries, `.deb`/`.rpm`, a Homebrew formula and a
container image), through the apt repository, Homebrew and GHCR. Context
Commander is server software an operator runs; updates are applied by the
operator's package manager or image pull, not installed by the product
itself, because an unattended restart of a team's server is the operator's
call. Release notes mark security fixes. *Open:* there is no in-product
notice that a newer version exists.

**(d) Protection from unauthorised access.**
- Every request is authenticated: a personal access token (hashed, ADR
  0009), a browser session cookie (HttpOnly, SameSite, with a CSRF header,
  ADR 0019), LDAP or OIDC with PKCE and issuer checks (ADR 0023).
- Every project-scoped operation calls `AuthzService.Can` (ADR 0014), and
  PostgreSQL row-level security repeats the decision beneath it (ADR 0002).
- Roles are fixed and listed in one place (`internal/auth/capability.go`);
  granting or removing ownership needs ownership.
- Tokens can be listed and revoked, effective immediately.
- *Reporting:* refused sign-ins and authorization refusals are written to
  the security log (`internal/security`, migration 000019), readable by
  installation administrators.

**(e) Confidentiality of stored, transmitted and processed data.**
- At rest: every artifact, memory, plan, idea, handoff, message, history
  object and secret is encrypted with AES-256-GCM under a per-resource key,
  wrapped by a per-project key, wrapped by the installation's root key,
  which never enters the database (ADR 0005). The root key can be rotated
  (`ccd root-key rotate`).
- In transit: TLS, enforced as above.
- Embedding vectors for semantic search are encrypted by default (ADR 0028).
- Export archives are plaintext by design and say so where they are made
  (ADR 0035).

**(f) Integrity of data, commands, programs and configuration.**
- AES-GCM authenticates every ciphertext, bound to its project, resource and
  version, so a swapped or altered row fails to decrypt (ADR 0005).
- Artifact versions are immutable at the privilege level, and writes use
  optimistic concurrency (`expected_version`).
- Releases are signed (cosign, keyless), carry SLSA build provenance, and
  the container image is signed and attested (`.github/workflows/release.yml`).
- Migrations are embedded in the binary and versioned.
- *Reporting:* a failed decryption is an error naming the cause (a wrong
  root key is named as such); the audit trail records every write.

**(g) Data minimisation.** The server stores what agents and people write
on purpose, and the metadata needed to find it. Usage is aggregated per day,
not logged per event, and old ledger rows can be dropped
(`--usage-ledger-retention`). Messages to agents are pruned. The web interface
loads nothing from third parties: its font is bundled, and the
Content-Security-Policy allows only the product's own origin.

**(h) Availability of essential functions, resilience against denial of
service.**
- Per-IP rate limits on sign-in, setup and other unauthenticated endpoints,
  behind trusted proxies only (ADR 0029).
- Request bodies are capped (`http.MaxBytesReader`), every growing list is
  paginated with a hard maximum, context packs and searches have byte and
  count budgets, and artifacts have a size limit (ADR 0005).
- Storage quotas per namespace (ADR 0021).
- Background work (embedding, retention) runs off the request path.

**(i) Minimise negative impact on other devices and networks.** The server
makes outbound connections only to what its operator configures: the
database, the object store, the directory, the OIDC provider, an embedding
provider. It does not phone home, and agents poll it, it does not push to
them.

**(j) Limit attack surfaces, including external interfaces.**
- One HTTP listener serving the REST API, MCP and the interface; loopback
  by default in the packages and compose.
- Unauthenticated endpoints are grouped in one place in the router, so the
  list is readable (`adapters/http/http.go`).
- The application's database role has no DDL, no DELETE on immutable
  tables, and no UPDATE on projects; privileged operations (migrations,
  root key rotation, import, purge) need the owner role through separate
  commands.
- Browser hardening headers on every response: a Content-Security-Policy
  without inline script, no framing, nosniff, no referrer.

**(k) Reduce the impact of an incident through exploitation mitigation.**
Memory-safe implementation language (Go). Unprivileged container user on a
distroless image, systemd sandboxing in the packages. Envelope encryption
limits a database or backup leak to ciphertext. Row-level security limits a
missed authorization check to what the caller could already read. The
server never holds the owner credentials.

**(l) Record and monitor relevant internal activity, with an opt-out.** The
security log records refused sign-ins, authorization refusals and other
security-relevant events, and the audit trail records every change to
project content, both readable through the interface and the API.
`ccd serve --security-log=false` opts out of the security log, as the
regulation requires; `docs/quickstart.md` states what is then lost.

**(m) Securely and easily remove all data and settings, and transfer data
securely.**
- Removal: an OWNER deletes a project from its Settings tab; after a grace
  period `ccd purge` removes every row and stored object of it (ADR 0036).
  A member can leave a project; an OWNER can remove members. Long-deleted
  artifacts are purged from storage.
- Transfer: a project exports to an archive and imports into another
  installation under that installation's keys, with integrity checked by
  hash (ADR 0035). The archive is plaintext and is handled as such.
- *Open:* deleting a user account, as opposed to its memberships, has no
  command yet.
