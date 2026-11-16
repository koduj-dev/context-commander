# Technical documentation (Annex VII)

The technical documentation of Context Commander, in the order of Annex VII
of the CRA. Most of it points into the repository, which is the
documentation: it is versioned with the code it describes. Kept for ten
years after the product is placed on the market, or for its support
period if longer (Art. 13(13)).

## 1. General description

**(a) Intended purpose.** A self-hosted layer for handing project context
between coding agents and the people who direct them: artifacts, memories,
plans, ideas, handoffs, conversation history and secrets, per project. See
[README.md](../../README.md) and
[docs/architecture-cookbook.md](../architecture-cookbook.md) §1 and §3 for
what it is and is not.

**(b) Versions affecting compliance.** Every release is identified by its
tag (`ccd version`). The changes that affect security are in
[CHANGELOG.md](../../CHANGELOG.md) under *Security*, and the decisions in
[docs/adr/](../adr/).

**(c) Hardware.** None; it is software. It runs on Linux (amd64, arm64) as
binaries, `.deb`/`.rpm` packages and a container image, with PostgreSQL 16+.

**(d) User information and instructions (Annex II).**
[docs/install.md](../install.md), [docs/quickstart.md](../quickstart.md),
[docs/team-deployment.md](../team-deployment.md),
[docs/backup-restore.md](../backup-restore.md),
[docs/AGENT-GUIDE.md](../AGENT-GUIDE.md) and
[SECURITY.md](../../SECURITY.md). **TODO(manufacturer):** a single
user-facing page with the items Annex II lists (manufacturer's name and
address, single point of contact for vulnerabilities, support period end
date, how to get security updates), linked from the README and each
release.

## 2. Design, development, production and vulnerability handling

**(a) Design and development.** The architecture is in
[docs/architecture-cookbook.md](../architecture-cookbook.md), the current
state in [docs/CURRENT-ARCHITECTURE.md](../CURRENT-ARCHITECTURE.md), and
every decision with its reasoning in [docs/adr/](../adr/). System
architecture: a single Go binary (`ccd`) serving the REST API
(`adapters/http`), MCP (`adapters/mcp`) and the embedded web interface
(`web/`), over domain packages in `internal/`, PostgreSQL with row-level
security, and a filesystem object store.

**(b) Vulnerability handling.** [vulnerability-handling.md](vulnerability-handling.md):
the SBOM, the disclosure policy, the contact address, how updates are
distributed.

**(c) Production and monitoring.** Releases are built from a tag by
`.github/workflows/release.yml`: `make release`, SBOM, signed checksums,
packages, provenance attestation, signed container image. Every change to
`main` passes `.github/workflows/ci.yml` (tests, lint, web build and tests,
CodeQL, gitleaks) through a protected branch.

## 3. Cybersecurity risk assessment

[risk-assessment.md](risk-assessment.md), and how each essential
requirement is met in [essential-requirements.md](essential-requirements.md).

## 4. Support period

**TODO(manufacturer):** the support period, during which vulnerabilities
are handled per Annex I, Part II. The CRA expects at least five years
unless the product is expected to be used for less (Art. 13(8)). Proposal:
five years from the first release placed on the market under the CRA, with
security fixes for the latest minor release line throughout. Until then,
only the latest release is supported, as SECURITY.md says.

## 5. Harmonised standards and specifications

No harmonised standard for the CRA has been published yet. The product
follows, where relevant: the OWASP Application Security Verification
Standard as a checklist for authentication, session handling and access
control; RFC 9700 (OAuth 2.0 security best current practice) and RFC 9207
for OIDC; SPDX for the SBOM; SLSA provenance for builds. **TODO(manufacturer):**
list the harmonised standards once published, and which parts are applied.

## 6. Test reports

The test suite is the report: `go test ./...` and `npm test` in `web/`, run
on every pull request against real PostgreSQL and object storage
([docs/testing.md](../testing.md)). Results are in the CI runs of each
release tag. There is no third-party test report.

## 7. EU declaration of conformity

[declaration-of-conformity.md](declaration-of-conformity.md), signed for each
version placed on the market.

## 8. Software bill of materials

Published with every release as `ccd.sbom.spdx.json`, and as an attestation
on the container image. Provided to market surveillance authorities on
request (Annex VII (8)).
