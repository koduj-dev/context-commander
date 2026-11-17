# Cybersecurity risk assessment

Art. 13(2) of the CRA asks the manufacturer to assess the cybersecurity
risks of the product and to take the assessment into account in its
design, development, production, delivery and maintenance. This is that
assessment, as of 2026-11-16. The threat model it builds on is in
[SECURITY.md](../../SECURITY.md#scope--threat-model).

## The product and its intended use

A self-hosted server that coding agents and the people who direct them use
to share project context: artifacts, memories, plans, ideas, handoffs,
conversation history and secrets, per project, over a REST API, MCP and a
web interface. It runs on a team's own infrastructure with PostgreSQL and a
filesystem object store. It is intended for small engineering teams; it is
not intended to hold regulated data such as health or payment records.

**Reasonably foreseeable misuse:** exposing it to the internet without TLS
or behind a proxy that does not set forwarding headers; sharing one token
between agents; putting credentials into artifacts instead of secrets;
granting OWNER widely.

## Assets

| Asset | Why it matters |
|---|---|
| Project content | Source code context, decisions, secrets: the reason the product exists |
| The root key | Decrypts everything; never in the database |
| Credentials | Tokens and sessions act as their principal |
| The owner database role | Can do anything to the schema; never held by the server |
| Availability | Agents stall without their context |
| Integrity of releases | An altered build would run with every key |

## Threats, mitigations and what remains

| Threat | Mitigation | Residual risk |
|---|---|---|
| Database or backup leak | Envelope encryption, root key outside the database (ADR 0005) | Metadata (paths, statuses, timestamps) is plaintext |
| Stolen token | Hashed at rest, revocable at once, scoped to its principal (ADR 0009) | A stolen token acts until revoked |
| A member reads another project | `Can()` in every service, RLS beneath (ADR 0002, 0014) | None known; covered by negative tests |
| Privilege escalation inside a project | Ownership changes need ownership; last OWNER kept | An OWNER can do everything in their project |
| Cross-site request forgery, clickjacking | CSRF header on cookie requests, SameSite cookies, CSP without framing | None known |
| Script injection through agent-written text | Markdown rendered to React elements, never HTML; CSP without inline script | — |
| Brute force on sign-in | Per-IP limits behind trusted proxies (ADR 0029), argon2id | Distributed guessing below the limit |
| Prompt injection through messages and memories | Server-written headers naming the sender; only people can message agents (ADR 0032) | A model may still follow injected text; the agent's client is responsible |
| Server process compromise | Distroless, unprivileged, sandboxed; owner role not held | An attacker with the process has the root key and every decrypted read |
| Wrong root key after restore | Refusal to start (`CheckRootKey`) | — |
| Denial of service | Body caps, pagination limits, budgets, quotas, rate limits | Volumetric attacks are the network's to absorb |
| Supply chain: dependencies | Dependabot, CodeQL, pinned action SHAs, SBOM | A compromised upstream release before an advisory exists |
| Supply chain: our releases | Tag-built in CI, cosign keyless signatures, SLSA provenance | A compromised maintainer GitHub account |
| Data kept longer than needed | Usage retention, message pruning, project deletion and purge (ADR 0036) | Memories, history and audit have no age-based retention yet |
| Third-party tracking | Nothing loaded from third parties; font bundled; CSP limits every fetch to the product's origin | — |

## Decisions this assessment led to

The ADRs record them; the ones made for security reasons are 0002, 0005,
0009, 0014, 0019, 0020, 0023, 0029, 0032, 0033 (read-only change feed, no
push channel), 0035 (import as an operator command) and 0036.

## Review

Review this assessment with every minor release and after any security
fix, and record what changed in the same pull request.
