# Documentation index

Each document has one job. If two of them would answer the same question,
one of them is wrong.

## Start here

| Document | Answers |
|---|---|
| [`quickstart.md`](quickstart.md) | How do I run this and get two agents handing work to each other? |
| [`../README.md`](../README.md) | What is Context Commander, and what is it deliberately not? |
| [`../CHANGELOG.md`](../CHANGELOG.md) | What shipped, and when? |
| [`../NEXT.md`](../NEXT.md) | What has *not* been done, and in what order should it be? |

## Design

| Document | Answers |
|---|---|
| [`architecture-cookbook.md`](architecture-cookbook.md) | Why is the system shaped this way? The long-form rationale, by domain. |
| [`adr/`](adr/) | What exactly was decided, when, and what did it rule out? One decision per file. |
| [`context-commander-ui-spec.md`](context-commander-ui-spec.md) | What is the web UI meant to become? |

## Working on it

| Document | Answers |
|---|---|
| [`dev-environment.md`](dev-environment.md) | How is this machine set up, and how do I run the thing from source? |
| [`testing.md`](testing.md) | What are the test tiers, why a real Postgres, and why is the suite fast? |
| [`gotchas.md`](gotchas.md) | What bit us, that a newcomer would otherwise rediscover the hard way? |
| [`../CONTRIBUTING.md`](../CONTRIBUTING.md) | What is the review discipline, and what are the standing rules? |
| [`../SECURITY.md`](../SECURITY.md) | How do I report a vulnerability? |

## Architecture decisions

Newest last. An ADR is never edited to change its decision — a later ADR
supersedes it, so the trail of what was believed when stays readable.

| ADR | Decision |
|---|---|
| [0000](adr/0000-record-architecture-decisions.md) | Record architecture decisions, in this format |
| [0001](adr/0001-project-namespace-not-global-slug.md) | Projects live in namespaces; no global slug |
| [0002](adr/0002-non-recursive-rls.md) | Non-recursive RLS, via a table-owning role that is exempt from it |
| [0003](adr/0003-set-local-transaction-context.md) | The principal travels as `SET LOCAL`, per transaction |
| [0004](adr/0004-principal-model.md) | Users and service principals are both Principals |
| [0005](adr/0005-key-hierarchy-and-artifact-size-limit.md) | Root key → project key → DEK, and a 50 MB artifact limit |
| [0006](adr/0006-organization-vs-project-authorization.md) | Organization and project authorization are separate, and organization membership is keyed by user |
| [0007](adr/0007-metadata-store-abstraction-sqlite.md) | No metadata-store abstraction; Postgres is assumed |
| [0008](adr/0008-agpl-3.0-license.md) | AGPL-3.0 |
| [0009](adr/0009-pat-format-and-bootstrap.md) | PAT format, and bootstrapping the first user without a UI |
| [0010](adr/0010-immutable-artifact-version.md) | ArtifactVersion is immutable; mutable crypto state lives beside it |
| [0011](adr/0011-encryption-classification.md) | What is content payload (encrypted) versus operational metadata (plaintext) |
| [0012](adr/0012-skill-registry-supply-chain.md) | Skill package and skill installation are separate concepts |
| [0013](adr/0013-transactional-outbox-and-storage-lifecycle.md) | Transactional outbox, and the storage write/cleanup lifecycle |
| [0014](adr/0014-authz-check-in-domain-service.md) | Authorization is checked in the domain service, not the adapter |
| [0015](adr/0015-project-creation-rls-bootstrap.md) | How a project's first member gets in past its own RLS policy |
| [0016](adr/0016-mcp-sdk-choice.md) | Which MCP SDK, and why |
| [0017](adr/0017-installation-admin-tier.md) | An installation-admin tier, keyed by user, with no network grant path |
| [0018](adr/0018-skill-package-scope-and-a-browsable-registry.md) | A skill package lives at a scope; the registry is browsable |

## Not documentation

`brand/` and `plans/` are working material, not reference. Nothing here
links into them on purpose.
