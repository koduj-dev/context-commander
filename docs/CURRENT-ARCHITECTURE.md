# Current architecture: authority, settled decisions, and drift

> **What this file is for.** Three different things kept getting mixed
> together: settled product decisions, target-state design, and what the
> repository actually contains today. Treating them as one document is what
> produces the failure this file exists to prevent — an agent deleting
> working code because a summary did not happen to mention it.
>
> This file does not re-state the architecture. [`architecture-cookbook.md`](architecture-cookbook.md)
> is the long-form *why*; [`adr/`](adr/) is the per-decision record; the code,
> migrations and tests are what *is*. This file says which of those wins when
> they disagree, and registers where they currently do.

---

## 1. Precedence

**For product and architecture decisions:**

1. An explicit current decision from the maintainer
2. Accepted ADRs
3. This file and the consolidated master context
4. Older plans, cookbook prose, review documents

A newer or more specific accepted ADR beats the consolidated architecture
text. A question the maintainer has settled is not re-opened unless
implementation evidence shows the decision is impossible or unsafe.

**For what exists right now**, the order is different and the documents come
last:

1. Current code
2. Migrations and schema
3. Tests
4. Current configuration
5. Documentation

Documentation must not claim a target-state component already exists.
**Absence from an architecture document is not a reason to remove implemented
behaviour.**

**When architecture and implementation disagree**, that is *architecture
drift*. Do not automatically repair either side. Classify it first —
intentional, transitional, stale documentation, or a real bug — and change
code only when the intended direction is actually settled. Section 4 is the
register of known drifts; each says which it is.

---

## 2. Settled — do not re-open

| Question | Decision | Recorded in |
|---|---|---|
| How do Gates execute? | Trusted built-in Gates, synchronously in-process through a Go registry. No WASM, no webhooks, no policy DSL in v1. | cookbook §13 |
| Which Secrets backend? | LocalEncryptedBackend first, backend abstraction from day one. Vault is not required for standalone v1. | cookbook §15 |
| Do Handoffs reference Tasks? | **No.** There is no Task domain in v1 and `referenced_tasks` has been removed everywhere it appeared. | cookbook §9 |
| What is a skill package scoped to? | The package carries its own scope; the installation scope is the marketplace shelf. | [ADR 0018](adr/0018-skill-package-scope-and-a-browsable-registry.md) |
| Who may read an audit trail? | Project: ADMIN/OWNER (`CapAuditRead`). Organization: ADMIN/OWNER (`CapOrgAuditRead`). Not plain members. | `internal/auth/capability.go`, `internal/organizations/capability.go` |

Product framing, unchanged: agents are clients and context is the product;
core stores context, skills add capability, gates enforce behaviour; the
project is the normal authorization boundary; MCP is an adapter, not the
domain; the UI is optional for core operations. Context Commander is not an
orchestrator, not an AI filesystem wrapper, not a Git replacement, and not a
task manager.

---

## 3. Implemented and easy to miss

These exist, are tested, and are **not** to be removed for being absent from
an older summary. Names and semantics come from the code, not from memory.

| Capability | Where |
|---|---|
| Aggregated starting context (`context.bootstrap`) | `internal/contextpack`, `GET .../context`, `ccd context` |
| Handoff lifecycle: open → claimed → closed, plus "current" | `internal/handoffs`, migration 000018 |
| Skill content reads (`skill.get`) | `internal/skills/content.go` |
| Browsable registry: search, versions, promote | `internal/skills/catalog.go`, migration 000021 |
| Client-side skill delivery | `ccd sync-skills` |
| Refusal recording + CRA opt-out (`--security-log`) | `internal/security`, migration 000019 |
| Per-credential rate limiting, bounded auth concurrency | `internal/ratelimit` |
| Organization audit trail (write and read) | migration 000023, `organizations.Service.QueryAudit` |
| Conversation enumeration | `history.Service.ListConversations` |

---

## 4. Drift register

Each entry says what the architecture wants, what the repository has, and
what to do — which is usually **nothing yet**.

### 4.1 Artifact content hash — OPEN, ADR required

*Architecture:* prefers a project-keyed fingerprint/HMAC and warns against
baking a public plaintext hash into the API and schema, because a raw hash
leaks equality.

*Repository:* `artifact_versions.content_hash` is a SHA-256 of the plaintext,
computed before encryption, and it is exposed through the API.

*Action:* none without a deliberate migration strategy and an ADR. This is a
shipped schema and a shipped API field; changing it is a migration plus a
breaking response change, not a cleanup.

### 4.2 Secrets model — TRANSITIONAL

*Architecture:* `Secret` / `SecretVersion` / `SecretVersionEnvelope`, with
rotation and version history.

*Repository:* one `secret_values` table. No version history. `Service` has
Create, Resolve, List and Delete — **no rotate**.

*Action:* document the current shape honestly; do not fabricate version
history or a rotate action in UI or API. A migration happens when it is
scheduled, not because a target-state section names richer entities.

### 4.3 Members API — INCOMPLETE

*Architecture:* names `project.remove_member`.

*Repository:* `members.Service` has `Add` and `List` only. Organizations
likewise have add and list, no removal.

*Action:* treat the API list as desired surface. The frontend must not offer
removal until the backend has it.

### 4.4 Integrations — NOT BUILT

*Architecture:* an `internal/integrations` module, project repository
bindings, an Integrations UI.

*Repository:* the package does not exist and nothing implements it.

*Action:* do not render functional Integrations management. Unavailable or
absent, not faked.

### 4.5 Event catalogue — PARTIAL

*Architecture:* a broad conceptual catalogue — `project.created`,
`project.member_added`, `skill.installed`, `gate.denied`, and more.

*Repository:* the types actually published are `artifact.updated`,
`artifact.deleted`, `handoff.created`, `handoff.claimed`, `handoff.closed`.

*Action:* keep the conceptual catalogue separate from the emitted set. A
frontend activity view must not invent events the backend never emits.

### 4.6 Technology facts — STALE DOCUMENTATION

*Architecture:* Go 1.23+, `sqlc` for SQL generation.

*Repository:* Go 1.27 (`go.mod`, `Dockerfile`), and every query is written
directly against `pgx`. `sqlc` is not used anywhere.

*Action:* correct the document. **Do not introduce `sqlc` to make a sentence
true.**

---

## 5. Intentional exceptions to invariants

The default is real: a domain mutation, its audit row and its outbox event
are written in one transaction. Two places deliberately deviate, and both
would be broken by "fixing" them.

**`skills.Service.Publish` is multi-phase.** The skill row is reserved in one
transaction, the content is uploaded to the object store between
transactions, and the immutable `skill_versions` row is committed after the
bytes definitely exist. An object store does not participate in a Postgres
transaction; the failure direction is chosen so a row never references
missing content ([ADR 0013](adr/0013-transactional-outbox-and-storage-lifecycle.md)).

**`security.Recorder.Record` writes in its own transaction.** A denial is
frequently produced inside a transaction that is about to roll back — a
capability check inside a domain mutation — and a record that disappears
with the rollback is a record of nothing. It is also best-effort by
construction: it takes no error return, because every caller is already on a
path that has decided to refuse the request and none of them may fail
differently because the recording failed.

---

## 6. UI framing

The UI is a **management and control plane**, not a browser for every
context-plane primitive. Most of what Context Commander stores is there for
MCP clients; a human does not need a tab for each table.

Navigation, as built (`web/src/App.tsx`):

```text
Dashboard          what is waiting on you

Projects
└── <namespace>/<project>
    ├── Overview       current handoff and what came before
    ├── Context        the assembled pack an agent receives, with its budget
    ├── Artifacts      versions, content, a versioned editor
    ├── Skills         the effective set: install, override, pin
    ├── Policies       gate bindings: bind, unbind
    ├── Secrets        names and references, never a value
    ├── Members        principals and roles
    ├── Conversations  what clients recorded, and lineage
    ├── Activity       audit and events on one timeline
    └── Settings       identity, how to connect an agent

Organizations      members and organization-scoped audit
Skill Registry     search, publish, per-skill detail and versions
Access tokens      your own credentials
Administration     admins, identity creation, credentials, security log
```

The tree is wider than the sketch it replaced because each entry answers a
question someone actually arrives with, not because a table exists. Context
is the one addition worth naming: it is the only screen that answers "why is
the agent behaving like that" without reading a transcript.

Artifacts, Handoffs, Events, Conversations, ExternalSessions and
HistoryObjects are domain primitives, **not automatically navigation items**.
Expose them where they answer a real management or diagnostic question —
Overview summarising recent activity and the latest handoff, an Activity
surface combining events with relevant audit entries, drill-down views for
version history or conversation lineage — rather than because a table exists.

Two rules that override any layout preference:

- **Never fake a backend capability in frontend state.** If the operation
  does not exist, hide it, disable it, or build the backend first. Section 4
  lists what does not exist.
- **`docs/brand/` is the source of truth for visual identity.** Read its
  README and UI-COLORS, use the provided SVGs and token files. Do not redraw
  the logo or invent a second identity.

---

## 7. Before a structural change

Read this file, the cookbook, and the relevant ADRs; then look at the code,
migrations and tests. Decide which of these the situation is — settled
architecture, current implementation fact, architecture drift, a documented
intentional exception, or a genuinely open decision — and only ask when it is
the last one.

Not every implementation task is an architecture workshop.
