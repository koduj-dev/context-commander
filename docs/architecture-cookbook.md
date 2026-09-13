# Context Commander — bootstrap / architecture cookbook

> Working document for the first public push.
>
> **Status:** design draft  
> **Date:** 2026-08-27  
> **Working name:** Context Commander  
> **Purpose:** capture the current architectural decisions, their rationale, MVP boundaries, and the implementation direction for the first repository push.

---

## 1. One-sentence definition

**Context Commander is a self-hosted, vendor-neutral shared context and collaboration layer for humans and AI agents.**

It gives independent AI clients and agents a common project-scoped workspace where they can:

- store and exchange artifacts,
- preserve project context,
- hand work over to another agent,
- share project-specific skills,
- publish and consume project events,
- collaborate across users,
- enforce project-level access through RBAC,
- bind external systems such as Git repositories,
- and protect stored data independently of any single AI vendor.

The core idea is not “AI filesystem”.

> **Agents are clients. Context is the product.**

---

# 2. The problem

Today it is easy to use several AI tools, but difficult to make them cooperate.

Typical tools may include:

- ChatGPT,
- Codex,
- Claude Chat,
- Claude Code,
- custom CLI agents,
- local models,
- CI/background agents,
- future vendors and tools that do not exist yet.

These tools can be useful individually, but they generally do not share a neutral project state.

A normal workflow currently looks like this:

1. do research in one chat,
2. manually copy the result somewhere,
3. open a coding agent,
4. explain the context again,
5. create files,
6. ask another model for review,
7. repeat the context yet again,
8. manually move artifacts between products.

Even tools from the same vendor may use different storage, history, skill, context, and session models.

Desired workflow:

```text
                     Context Commander

 ChatGPT ───────────────┐
 Codex ─────────────────┤
 Claude Chat ───────────┼──── Project XYZ
 Claude Code ───────────┤        │
 custom agent ──────────┘        ├── artifacts
                                 ├── context
                                 ├── handoffs
                                 ├── skills
                                 ├── events
                                 ├── history
                                 └── integrations
```

No client needs to know how another vendor works.

All clients know only Context Commander.

---

# 3. Product boundary

## 3.1 Context Commander is not an agent orchestrator

CC should not initially decide:

- which model should perform a task,
- which agent should wake up next,
- how a multi-agent reasoning loop should work,
- how agents should negotiate,
- how LLM inference should be scheduled.

CC may expose events and handoff information that orchestrators or active agents can consume, but it should not require one orchestration model.

**Decision:** Context Commander provides collaboration infrastructure; orchestration is an optional consumer of that infrastructure.

### Why

Trying to become an orchestrator immediately would:

- explode the scope,
- couple CC to rapidly changing agent frameworks,
- duplicate existing ecosystems,
- weaken the main differentiator: neutral shared context.

---

## 3.2 Context Commander is not just an S3/MCP filesystem wrapper

Object storage is an implementation detail.

If the product exposes only:

```text
list
read
write
delete
```

then it is simply another filesystem/object-store MCP server.

The important abstraction is **project context**:

```text
Project
├── members / RBAC
├── artifacts
├── handoffs
├── skills
├── events
├── metadata
├── history
├── external sessions
└── integrations
```

**Decision:** Files are one type of project artifact, not the product itself.

---

## 3.3 Context Commander is not a Git replacement

Git remains the correct system for source code and source history.

CC may:

- bind a project to one or more repositories,
- expose repository metadata to agents,
- distribute project skills,
- store non-source artifacts related to development,
- maintain handoffs and project context independent of a repository.

Expected split:

```text
Git
├── source code
├── configuration
├── normal text documentation
└── source history

Context Commander
├── AI-generated artifacts
├── research
├── PDFs / images / datasets
├── handoffs
├── project context
├── skill registry
├── event history
├── agent provenance
└── cross-agent collaboration state
```

There may be overlap. That is acceptable.

---

## 3.4 MCP is an adapter, not the internal architecture

MCP is currently the obvious primary integration surface, but CC must not hard-wire its domain model to one protocol revision.

Recommended architecture:

```text
                Context Commander Core
                         │
       ┌─────────────────┼─────────────────┐
       │                 │                 │
      MCP              HTTP/API           CLI / UI
       │                 │                 │
       └─────────────────┴─────────────────┘
```

Internal services expose application/domain interfaces. MCP maps those interfaces into MCP tools/resources/prompts where useful.

**Decision:** Build the domain first; MCP is a transport/integration layer.

### Why

“May become part of MCP” is not the same as “is part of MCP now”.

CC should use current standards where they exist, but it must not block useful functionality while waiting for future protocol features, nor invent deep dependencies on roadmap promises.

If MCP later standardizes a capability already present in CC, the adapter should migrate to the standard representation while the internal domain remains stable.

---

# 4. Primary architectural invariant: Project is the security boundary

Authorization should primarily happen at the **project level**.

Do not begin with filesystem/path/object-level ACL complexity.

Example:

```text
User Filip
├── personal-a       OWNER
├── personal-b       OWNER
└── shared-xyz       CONTRIBUTOR

User Petr
├── personal-c       OWNER
└── shared-xyz       CONTRIBUTOR

User Jana
└── shared-xyz       READER
```

### Why

Project-level RBAC is deliberately simple:

- agents can reason about it,
- users understand it,
- authorization queries remain cheap,
- accidental cross-project leakage is easier to prevent,
- private/shared context becomes explicit,
- we avoid an ACL matrix for every path/object.

Fine-grained capabilities may exist behind roles, but project membership is the normal authorization boundary.

---

# 5. Projects

## 5.1 A user can create a project through an agent

Project creation must not depend on opening the web UI.

Desired interaction:

```text
User:
"Create project elastic-migration."

Agent:
project.create(name="elastic-migration")

Context Commander:
project created
owner = authenticated user
visibility = private
```

The creator becomes OWNER by default.

**Decision:** The UI is a management frontend over the same APIs, not a prerequisite for using CC.

---

## 5.2 Private projects are normal projects with restricted membership

Avoid separate personal and organization implementations.

Private project:

```text
Project A
members:
  Filip = OWNER
```

Shared project:

```text
Project XYZ
members:
  Filip = OWNER
  Petr  = CONTRIBUTOR
  Jana  = READER
```

This keeps the data model consistent.

A standalone/local installation does not need a fake company hierarchy to work.

---

## 5.3 Shared state must be explicit before work begins

If a user says:

> Work on project XYZ.

one of the first client operations should resolve the project context.

Example:

```text
project.resolve("XYZ")
```

Possible response:

```json
{
  "id": "prj_xyz",
  "name": "XYZ",
  "shared": true,
  "role": "contributor",
  "members": 8,
  "repositories": [
    "github.com/acme/xyz"
  ],
  "effectiveSkills": [
    "installation/coding-conventions",
    "company/security-policy",
    "project/xyz-architecture"
  ]
}
```

An agent should immediately know:

- which project it is in,
- whether it is shared,
- who it acts as,
- what permissions it has,
- which project/company/installation skills are active,
- which repositories and integrations belong to the project.

### Why

Cross-project data leakage is one of the easiest mistakes for an AI client to make.

Project context must be first-class state, not merely an implicit path prefix.

---

## 5.4 External integrations are project metadata

A project may be bound to resources such as:

- Git repository/repositories,
- issue tracker project,
- CI system,
- documentation space,
- object store prefix/bucket,
- secret references,
- other MCP/API endpoints.

Example conceptual API:

```text
project.bind_git(project="XYZ", repository="git@github.com:acme/xyz.git")
```

**Decision:** integrations belong to project context so agents do not repeatedly need users to rediscover or re-enter them.

---

# 6. Identity model

## 6.1 Human/user identity and client identity are different

Preserve both.

Example audit record:

```text
Actor user:    Filip
Via client:    Claude Code
Project:       XYZ
Action:        artifact.write
```

This is better than storing only:

```text
Claude Code modified file
```

or only:

```text
Filip modified file
```

Both facts matter.

Suggested concepts:

```text
User
ClientCredential
ServicePrincipal
RequestContext / Session
```

---

## 6.2 Normal interactive agents act on behalf of a user

Typical case:

```text
Filip
├── ChatGPT credential
├── Claude Chat credential
├── Claude Code credential
└── Codex credential
```

Project RBAC belongs to Filip.

Individual client credentials can be revoked independently without changing Filip's project membership.

---

## 6.3 Service principals are separate identities

Later we will need non-human actors such as:

```text
nightly-review-agent
ci-agent
release-agent
research-daemon
```

These should not pretend to be a human user.

They should be service principals with their own project roles/capabilities.

The first implementation does not need an elaborate IAM suite, but the schema must not make service principals impossible.

---

# 7. RBAC

## 7.1 Initial roles

Keep v1 small:

```text
OWNER
ADMIN
CONTRIBUTOR
READER
```

Suggested meaning:

### OWNER

- full project control,
- member management,
- ownership/security settings,
- integrations,
- skills,
- project deletion.

### ADMIN

- normal project administration,
- member management,
- project configuration,
- skills/integrations,
- no ownership transfer unless explicitly permitted — including **no granting
  of the OWNER role**. Member management and ownership transfer are separate
  capabilities (`member.manage` vs. `member.grant_owner`) precisely because
  collapsing them lets an ADMIN promote a principal they control and inherit
  ownership through it. The same split applies at organization scope
  (`org.manage_members` vs. `org.grant_owner`).

### CONTRIBUTOR

- read/write project artifacts,
- create handoffs/events,
- use project skills,
- create/update normal project context.

### READER

- read project context/artifacts,
- no mutations.

Internally roles may map to capabilities:

```text
project.read
project.update
project.delete

member.read
member.manage
member.grant_owner

artifact.read
artifact.write
artifact.delete

skill.read
skill.write
skill.install

event.read
event.publish

integration.read
integration.manage
```

**Decision:** expose understandable roles to users, retain capability mapping internally so future policies can become finer-grained without rewriting the model.

---

# 8. Artifacts and storage

## 8.1 Storage backend

Preferred initial storage architecture:

```text
Context Commander
        │
        ├── metadata DB
        │
        └── object storage
              └── S3-compatible backend
```

Possible backends:

- MinIO,
- AWS S3,
- Cloudflare R2,
- another S3-compatible service,
- local filesystem adapter for development/single-node use.

> **What is actually wired, as of 2026-09-11.** The running server only ever
> constructs the filesystem backend: `adapters/cli/serve.go` calls
> `filesystem.New(storageDir)` and has no code path that builds
> `internal/storage/s3`. That package is implemented and unit-tested against
> its own fake, but nothing in `ccd serve` can reach it and no integration
> test exercises it against a real S3/MinIO endpoint — so **S3 is
> experimental and unsupported**, not a configuration option a deployment
> can choose today. The filesystem backend is the supported one
> (`--storage-dir` / `$CC_STORAGE_DIR`), and the supported quickstart
> (`docs/quickstart.md`) ships no object-storage container at all. This
> paragraph goes away the day `serve` can construct either backend and an
> integration test proves the S3 one works.

### Why S3-compatible storage

It already gives useful primitives:

- scalable binary storage,
- MIME/content metadata,
- versioning options,
- checksums/ETags,
- lifecycle rules,
- presigned URLs,
- separation of database metadata from large payloads.

**Decision:** CC should not send giant files through MCP as base64 unless unavoidable.

For large/binary artifacts prefer upload/download mechanisms backed by object storage.

---

## 8.2 Artifact model

### What artifacts are for

Stated here because everything below describes the *shape* of an artifact
and nothing said what it is **for** — which left the name to carry the
meaning, and "artifact" carries the wrong one for anyone who has met the
word in an AI product recently.

An artifact is **shared storage for agent data**. Its job is to end the
manual hand-carrying of files between agents: today, moving a markdown file
from one agent's session to another's means a human copying text between two
windows. Here, one agent writes it and another reads it, both through the
same project, with provenance and versions kept.

Three shapes that follow from that, all of them ordinary uses rather than
special cases:

- **Producer/consumer.** One side fills, the other drains. The versioning
  and optimistic concurrency in §8.3 exist because both sides may be
  working at once.
- **Durable findings.** Whatever an LLM concluded, decided, or measured and
  should not have to keep in its context window — written down once,
  readable by whoever comes next. The handoff (§9) carries the *state of the
  work*; artifacts carry the material the work produced.
- **Anything with bytes and a content type.** The store is content-type
  aware and does not interpret what it holds, so a rendered document is as
  valid an artifact as a set of notes. What the server does NOT do is serve
  that content back as a web page — see NEXT.md, where that is recorded as
  an opportunity with a security caveat rather than a feature.

What an artifact is **not**: general "memory for AI" with relevance and
retrieval as the product (§3 non-goals, NEXT.md). The difference is that an
artifact is addressed by a logical path a caller chose, not found by a query
it hopes will match.

### Fields

An artifact is more than a file path.

Suggested metadata:

```text
Artifact
├── id
├── project_id
├── logical_path / name
├── content_type
├── size
├── current_version
├── created_by_user
├── created_by_client
├── created_at
├── updated_at
├── provenance
└── storage reference
```

Version metadata may additionally include:

```text
ArtifactVersion
├── version
├── content_hash
├── storage_object
├── parent_version
├── created_by_user
├── created_by_client
├── created_at
└── encrypted payload metadata
```

---

## 8.3 Optimistic concurrency

Multiple agents may write to the same artifact.

Do not silently overwrite by default.

Conceptual operation:

```text
artifact.write(
    id="...",
    expected_version=17,
    content=...
)
```

If current version is 18:

```text
409 conflict
```

Client can then read/merge/retry.

### Why

Without explicit concurrency control, multi-agent collaboration will destroy work unpredictably.

---

# 9. Handoffs

Handoff is a first-class concept, not just a convention around a file named `handoff.md`.

Goal:

> Agent A can leave enough structured state for Agent B to continue without sharing vendor-specific conversation history.

Suggested fields:

```text
Handoff
├── id
├── project_id
├── created_by_user
├── created_by_client
├── source_session / source_run
├── summary
├── completed_work
├── next_actions
├── referenced_artifacts
├── created_at
└── status
```

**No `referenced_tasks`.** An earlier draft of this list carried one, and it
had leaked into the domain type, both adapters and the context pack without
anything ever populating it. v1 has no Task domain (§3 non-goals), so the
field was removed before the API stabilised rather than carried forever.
Handoffs written before the removal keep the key inside their encrypted
body; `encoding/json` drops unknown fields on read, so nothing had to be
migrated.

A Markdown rendering may still be useful for human readability.

Example:

```markdown
# Current handoff

## Task
Migrate Elasticsearch 7.x to 8.x.

## Completed
- checked mapping compatibility
- identified deprecated client usage

## Artifacts
- /research/es-migration.md
- /artifacts/deprecations.json

## Next
Validate ingest pipelines.
```

**Decision:** Do not attempt to make agents share private chain-of-thought. Handoff stores explicit working state and artifacts, not hidden reasoning.

---

# 10. External AI session history and compaction

This is a critical design point discovered during initial analysis.

## 10.1 Provider session IDs are NOT stable conversation identities

Observed example: Claude Code may change its session ID when conversation history is compacted/summarized.

That means:

```text
original session ID != compacted session ID
```

although the compacted session is logically a continuation/derivative of the same working conversation.

Therefore:

> **Never use a provider session ID as Context Commander's canonical conversation identity.**

---

## 10.2 Context Commander needs its own stable lineage model

Recommended concepts:

```text
Conversation
├── id                    # stable CC identifier
├── project_id
├── created_by_user
├── created_at
└── ...

ExternalSession
├── id                    # CC record id
├── conversation_id       # stable logical conversation
├── provider              # anthropic/openai/etc.
├── client                # claude-code/chatgpt/codex/...
├── provider_session_id   # opaque external ID
├── parent_external_session_id?
├── relation              # started | continued | compacted_from | forked_from
├── started_at
└── ended_at?
```

Example lineage:

```text
CC Conversation: conv_42

Claude Code session A
        │
        │ compaction
        ▼
Claude Code session B
        │
        │ another compaction
        ▼
Claude Code session C
```

All three remain part of:

```text
conversation_id = conv_42
```

but retain their own external/provider session IDs.

---

## 10.3 Compaction must not overwrite original history

A compacted representation is **derived data**, not the same object as the original transcript/history.

If CC stores history snapshots, use immutable lineage:

```text
HistoryObject
├── id
├── conversation_id
├── external_session_id
├── type
│   ├── raw
│   ├── compacted
│   ├── summary
│   └── imported
├── derived_from?
├── content_hash
├── created_at
└── encrypted_payload
```

Example:

```text
raw_A ──compacted_to──> compact_B ──compacted_to──> compact_C
```

Do not replace `raw_A` with `compact_B` and pretend they are equivalent.

### Why

- compacted history may omit details,
- provider behavior may change,
- audit/provenance requires knowing what was actually available at a point in time,
- session IDs are provider implementation details,
- a future agent may need the compacted state while an administrator/audit flow may need original material.

---

## 10.4 Session lineage is not necessarily a tree of truth

Providers may support:

- forks,
- continuations,
- compactions,
- retries,
- imported sessions,
- session cloning.

Therefore model it as explicit edges/relations, not assumptions encoded in the identifier format.

Potential relations:

```text
CONTINUES
COMPACTED_FROM
FORKED_FROM
IMPORTED_FROM
SUMMARIZED_FROM
```

**Decision:** Provider IDs are opaque metadata. CC owns the stable identity and lineage.

---

# 11. Events and notifications

Artifact sharing alone is not enough.

Agents or external tools should be able to discover that something changed.

Conceptual events:

```text
project.created
project.member_added
artifact.created
artifact.updated
artifact.deleted
handoff.created
task.created
task.completed
skill.installed
skill.updated
review.requested
```

Example:

```json
{
  "type": "artifact.updated",
  "project": "prj_xyz",
  "artifact": "architecture.md",
  "actor": {
    "user": "filip",
    "client": "claude-code"
  },
  "version": 7
}
```

---

## 11.1 Transport is not the domain

Do not bake WebSocket into the core domain as the only solution.

Possible delivery mechanisms may include:

- MCP-native server notifications/subscriptions where available,
- SSE,
- WebSocket,
- webhook,
- polling/feed API,
- internal message bus.

**Decision:** the domain concept is an event/subscription system. WebSocket is one possible transport.

---

## 11.2 Notification does not imply agent wake-up

A passive chat client may be unable to autonomously start working after receiving an event.

Therefore distinguish:

```text
passive client
    event becomes visible/contextual on next interaction

active agent/daemon
    may react immediately
```

CC should not promise that every integrated chat can be “woken up”.

It only provides a standard place to publish and consume collaboration events.

---

# 12. Skill Registry and Marketplace

The project needs a built-in writable skill registry.

“Marketplace” should be considered the UI/distribution experience over the registry rather than the core storage concept.

## 12.1 Scope hierarchy

Skills should compose from multiple scopes:

```text
INSTALLATION
     +
COMPANY / ORGANIZATION
     +
PROJECT
     =
EFFECTIVE SKILLS
```

Example:

```text
INSTALLATION
  coding-conventions
  git-workflow

COMPANY: ACME
  security-policy
  jira-workflow
  company-architecture

PROJECT: XYZ
  xyz-api
  xyz-testing
  xyz-release-process
```

Effective set for Project XYZ:

```text
coding-conventions
git-workflow
security-policy
jira-workflow
company-architecture
xyz-api
xyz-testing
xyz-release-process
```

For a local standalone install:

```text
company scope = absent
```

Nothing breaks.

### Decision

Scopes **compose; they do not replace one another**.

---

## 12.2 Writable registry

Skills must be writable through the same platform.

Desired flow:

```text
Agent discovers a reusable project procedure
        │
        ▼
skill.create(scope=project:XYZ, ...)
        │
        ▼
UI / CLI can review and edit it
        │
        ▼
other agents receive it as effective project skill
```

This turns project knowledge discovered during work into reusable context.

---

## 12.3 Vendor adapters

Different clients expose skills differently.

Therefore CC should store a canonical skill representation and provide adapters/exporters where needed.

Possible targets:

```text
Claude Code project skills
Claude Code personal skills
MCP resources/prompts/tools
future Codex/OpenAI mechanisms
plain filesystem export
CLI sync
```

Do not make one vendor's directory structure the canonical internal model.

---

## 12.4 Skills are a software supply-chain problem

A skill may contain more than text. Depending on the target ecosystem it may include:

- instructions,
- resources,
- scripts,
- executable helpers,
- network/tool usage guidance.

Therefore a marketplace cannot treat all skills as harmless Markdown.

Suggested metadata:

```text
Skill
├── id
├── name
├── version
├── scope
├── author
├── origin
├── checksum
├── signature?
├── trust status
├── declared capabilities
├── files
└── created/updated timestamps
```

UI should eventually be able to show:

```text
✓ authored locally
✓ signed
✓ approved by organization
⚠ contains executable scripts
⚠ requests network/tool access
```

**Decision:** provenance, immutable versions/checksums, and trust metadata belong in the design from the beginning even if cryptographic signing is not part of the first MVP.

---

# 13. Skills vs Gates

Keep these concepts separate.

## 13.1 Skill

A skill describes **what an agent can know/do**.

Examples:

```text
GitHub workflow
Jira workflow
Research procedure
PDF extraction
Code review
Release procedure
Project architecture
```

---

## 13.2 Gate

A gate describes **what must/must not happen** around an operation.

Examples:

```text
shared-workspace-awareness
require-handoff
approval-required
protected-artifacts
secret-scanner
PII policy
source/provenance required
```

Possible lifecycle hooks:

```text
project.resolve
project.enter

artifact.before_read
artifact.after_read
artifact.before_write
artifact.after_write
artifact.before_delete

handoff.before_create
handoff.after_create

task.before_complete
task.after_complete
```

A gate may return:

```text
ALLOW
DENY
ALLOW_WITH_GUIDANCE
```

---

## 13.3 Guidance and enforcement are different

Never rely on prompt instructions for real authorization/security.

Example guidance:

```text
"This is a shared workspace. Inspect current artifacts before modifying them."
```

Example enforcement:

```text
artifact.delete(...)
→ authorization/gate denies request
→ operation is not executed
```

**Decision:** security-sensitive gates must be enforced server-side.

Agent-facing instructions are supplemental guidance.

---

# 14. Security and encryption

This is a primary subsystem, not a later checkbox.

Shared project servers will inevitably contain sensitive data such as:

- internal documents,
- conversation history,
- code snippets,
- customer/project information,
- accidentally copied `.env` contents,
- tokens,
- passwords,
- credentials,
- private URLs,
- proprietary research.

---

## 14.1 Threat model must be explicit

Different encryption strategies protect against different threats.

### Threats application-level encryption can help with

- stolen disks,
- stolen object-store contents,
- leaked backups,
- database dumps,
- storage administrator without encryption keys.

### Threats it does NOT inherently solve

- attacker fully controlling the running CC process,
- compromised host with access to decryption keys,
- malicious authorized user,
- data already sent to an external model/vendor,
- secret copied by an agent into a permitted artifact.

**Decision:** never market “encrypted at rest” as if it were zero-knowledge security.

---

## 14.2 Preferred standard mode: application-level envelope encryption

Suggested model:

```text
              KMS / Vault / master key provider
                         │
                        KEK
                         │
                         ▼
                    wrapped DEK
                         │
                         ▼
plaintext ── AEAD encryption ──> encrypted artifact
```

Prefer an authenticated encryption mode such as AES-256-GCM or another well-established AEAD primitive provided by a vetted crypto library.

Avoid implementing custom cryptography.

Possible structure per artifact version:

```text
ArtifactVersion
├── ciphertext
├── nonce
├── wrapped_dek
├── algorithm
├── key_version
└── authentication metadata
```

A separate data encryption key per artifact version is a reasonable model.

---

## 14.3 Project-level key boundary

A useful hierarchy may eventually be:

```text
Installation key authority
        │
        ├── Project A key context
        │        ├── artifact DEK
        │        └── artifact DEK
        │
        └── Project XYZ key context
                 ├── artifact DEK
                 └── artifact DEK
```

This supports stronger separation and future crypto-shredding/key rotation strategies.

Exact key hierarchy can evolve; the important decision is that keys must not simply live next to encrypted blobs in the same datastore without separation.

---

## 14.4 What should be encrypted

At minimum consider sensitive payload encryption for:

```text
artifact contents
handoffs
conversation/history payloads
task descriptions
private skill data
event payloads containing content
integration configuration containing sensitive fields
```

Audit/index metadata may remain plaintext where required for operation, but metadata itself can leak information.

Example dangerous plaintext path:

```text
/customers/acme/security-incident-password-leak.md
```

Future stricter modes may protect names/paths/metadata as well.

---

# 15. Secret management

Tokens and passwords should not be treated as ordinary artifacts.

Preferred model:

```text
Context Commander stores:
secret://project/xyz/github

Secret backend stores:
actual token
```

Potential secret backends:

- HashiCorp Vault,
- cloud secret managers,
- encrypted local secret store for standalone use.

CC should avoid exposing raw credentials to agents unless a specific integration absolutely requires it.

Where possible, CC should perform the privileged operation itself or mint short-lived credentials.

---

## 15.1 Accidental secret ingestion is inevitable

Agents will eventually write something like:

```text
DATABASE_PASSWORD=...
GITHUB_TOKEN=...
PRIVATE KEY...
```

into context/history/artifacts.

Therefore a future/default security gate should scan writes for likely secrets.

Conceptual pipeline:

```text
artifact.write
      │
      ▼
 Secret Scanner Gate
      │
      ├── clean → continue
      └── suspicious → warn / redact / block according to policy
```

Patterns may include:

- GitHub tokens,
- cloud access keys,
- private keys,
- JWTs,
- generic API tokens,
- password assignments.

**Decision:** secret detection belongs naturally in the Gate subsystem rather than being hard-coded into artifact storage.

---

# 16. Audit and history

Auditability is especially important in shared projects.

Store both actor dimensions:

```text
timestamp
project_id
user_id
client_id/service_principal_id
action
artifact/resource id
version/result
```

Content payload/diff may be encrypted separately.

Example:

```text
2026-08-27 11:42
user=filip
client=claude-code
project=xyz
action=artifact.write
artifact=abc123
version=7
```

Avoid logging secrets in plaintext.

---

# 17. Provenance

Provenance is a key differentiator from a dumb shared filesystem.

Useful metadata:

```text
created_by_user
created_by_client
created_by_model?        # optional / best-effort
source_artifacts
source_handoff
source_session
parent_version
created_at
```

Potential queries:

- Who created this artifact?
- Which client modified it last?
- Which source documents produced this report?
- What changed after a review?
- Which artifact resulted from this handoff?
- Which external AI session was responsible for this version?

Do not over-promise model identity if clients cannot reliably provide it.

---

# 18. Organization/company scope

A company/organization layer is useful, but it should not become a requirement for local installations.

Conceptual hierarchy:

```text
Installation
├── users
├── installation skills
│
├── Organization ACME
│   ├── members / groups
│   ├── organization skills
│   ├── Project X
│   └── Project Y
│
└── standalone/private projects
```

A local single-user install may use:

```text
installation scope
project scope
```

with no organization object at all.

**Decision:** company scope extends the model; it does not redefine the local model.

---

# 19. Groups

Groups will become useful for organization deployments:

```text
backend-team
qa-team
management
```

Then project access can be granted to groups instead of manually repeating memberships.

This is useful but does not need to be in the very first minimal implementation if it delays the basic project RBAC model.

Schema and authorization code should nevertheless avoid assumptions that every membership must always point directly to a user.

---

# 20. Suggested top-level domain model

```text
Installation
├── User
├── ClientCredential
├── ServicePrincipal
├── Organization?
├── Project
│   ├── Membership / RBAC
│   ├── Integration
│   ├── Artifact
│   │   └── ArtifactVersion
│   ├── Handoff
│   ├── Conversation
│   │   └── ExternalSession
│   │       └── HistoryObject
│   ├── Event
│   ├── SkillBinding
│   └── GateBinding
│
├── Skill
│   └── SkillVersion
│
└── Gate
    └── GateVersion
```

This is conceptual. Do not turn every noun into a microservice.

---

# 21. Suggested service/module boundaries

First implementation can be a modular monolith.

Recommended modules:

```text
context-commander/
├── cmd/
├── internal/
│   ├── auth/
│   ├── identity/
│   ├── projects/
│   ├── artifacts/
│   ├── history/
│   ├── handoffs/
│   ├── events/
│   ├── skills/
│   ├── gates/
│   ├── integrations/
│   ├── crypto/
│   └── storage/
│       ├── filesystem/
│       └── s3/
├── adapters/
│   ├── mcp/
│   ├── http/
│   └── cli/
└── web/                  # later / optional initial UI
```

**Decision:** start as a modular monolith, not microservices.

### Why

- easier self-hosting,
- simpler OSS deployment,
- lower operational complexity,
- domain boundaries can still be kept clean,
- services can be split later if real load/organizational reasons appear.

---

# 22. Initial API/MCP surface

A useful first vertical slice could expose:

## Project

```text
project.list
project.create
project.resolve
project.get
project.update
project.add_member
project.remove_member
```

## Artifact

```text
artifact.list
artifact.stat
artifact.read
artifact.write
artifact.delete
artifact.history
```

## Handoff

```text
handoff.list
handoff.get
handoff.create
```

## Skill

```text
skill.list_effective
skill.get
skill.create
skill.update
```

## Event

```text
event.list
event.publish
```

Do not implement every future concept in the first PR.

## 22.1 Pagination contract

Every list that grows without bound shares one contract, across HTTP, MCP and
the CLI: a caller passes `limit` and `cursor`, and gets back the rows plus a
`next_cursor` when — and only when — more remain.

**Keyset, never offset.** `OFFSET` re-scans and then skips, so rows shift under
a caller whenever the underlying set changes between pages. For an
append-heavy store that is the normal case, not an edge case. Each list orders
by a key that only moves one way and asks for the rows past a position in it.

**The cursor is opaque.** Callers echo `next_cursor` back and never construct
or parse one. The encodings differ underneath — `(timestamp, uuid)` for the
UUID-keyed tables, a `seq`/`id` integer for the bigserial ones, the name itself
for secrets — but a client sees one mechanism. Each cursor names its own kind,
so one pasted into a differently-ordered endpoint is rejected rather than
reinterpreted as a position it never meant.

**A timestamp alone is not a key.** Two rows can share a `created_at` to the
microsecond, and a `created_at < cursor` comparison then skips every row but
one of the colliding group. The comparison is a row value over
`(timestamp, id)` matching the `ORDER BY` exactly.

**"There is more" is a fact, not a guess.** Services query `limit + 1` rows and
return a cursor only if the extra one came back. Inferring it from a full page
is wrong in both directions: it hides rows behind an absent button when a page
is short, and offers a button that returns nothing when the row count happens
to be a multiple of the page size.

`internal/paging` holds the shared primitives. Not everything is paginated:
`skill.list_effective` composes project-over-organization-over-installation
bindings *after* reading them, so paging it would change which binding wins —
and the set is bounded by what a project has installed. Project and
organization member lists are bounded the same way.

---

# 23. MVP recommendation

The first usable release should prove one thing:

> Two different users and/or two different AI clients can safely work on the same project through Context Commander and exchange context without manual file shuffling.

## MVP must have

### Identity/auth

- authenticated user identity,
- client identity metadata,
- revocable credentials.

### Projects

- create project from API/MCP,
- owner assigned automatically,
- list/resolve project,
- shared membership,
- basic RBAC.

### Artifacts

- list/read/write,
- binary-safe object storage abstraction,
- versions,
- optimistic conflict detection.

### Handoff

- create/read handoff,
- references to artifacts.

### Skills

- registry,
- project scope,
- installation scope,
- effective skill listing,
- writable through API,
- editable representation.

Company scope may be implemented immediately if inexpensive, but should not distort the basic model.

### Security

- encrypted sensitive payloads at rest at the application layer or a clearly documented first-step implementation toward it,
- credentials/secrets not stored as normal artifacts,
- safe audit logging,
- strict project authorization checks.

### MCP

- remote integration sufficient for at least two real clients,
- domain model not coupled to MCP internals.

---

# 24. Things intentionally NOT required for first push

Do not block the repository on:

- autonomous agent orchestration,
- full task engine,
- WebSocket implementation,
- every possible event transport,
- organization groups,
- public marketplace federation,
- cryptographic skill signing,
- zero-knowledge mode,
- enterprise IdP provisioning,
- semantic/vector search,
- UI visual polish,
- plugin sandbox runtime,
- automatic Git operations,
- waking passive web chats,
- model-to-model direct messaging.

Document them; do not build them all at once.

---

# 25. Security mode roadmap

## Standard mode

Server can process plaintext after authorized decryption.

Benefits:

- search,
- previews,
- content processing,
- secret scanning,
- server-side transformations,
- easier integrations.

Expected security:

- TLS in transit,
- application-level encrypted payloads,
- separated key management,
- project RBAC,
- audit trail.

## Future zero-knowledge mode

Encryption/decryption happens on trusted client side.

Server stores ciphertext only.

Tradeoffs:

```text
server-side search        difficult/unavailable
server-side preview       unavailable
secret scanning           unavailable server-side
content transformation    unavailable server-side
key recovery              harder
multi-client UX            harder
```

**Decision:** do not make zero-knowledge mode the default architecture of v1, but avoid design choices that permanently prevent it.

---

# 26. Key design questions still open

These require implementation experiments rather than speculation.

1. **Canonical skill format**  
   How much should CC normalize vs store an opaque skill bundle plus metadata?

2. **Skill adaptation**  
   Which clients can consume skills dynamically over MCP and which need filesystem synchronization/export?

3. **MCP auth details**  
   Implement current standardized mechanisms where practical, but keep authorization/domain logic internal.

4. **Event delivery**  
   Which transport gives the best interoperability today without turning CC into an orchestrator?

5. **History ingestion**  
   Which clients expose enough session/history metadata for useful lineage without brittle scraping?

6. **Encryption/key provider interface**  
   Define a clean provider API that supports local self-hosting and external KMS/Vault systems.

7. **Organization model timing**  
   Add company scope early enough for shared deployments, but do not make it mandatory for single-user installations.

8. **Gate execution model**  
   Declarative policies first? Trusted in-process plugins? WASM sandbox? External webhook/service? This should be decided after the basic hook model is proven.

---

# 27. Design rules for contributors

These are intended to keep the project from drifting.

### Rule 1

**Project is the normal authorization boundary.**

Do not add per-file ACLs casually.

### Rule 2

**Provider IDs are opaque external metadata.**

Do not use Claude/OpenAI/etc. session identifiers as canonical CC identities.

### Rule 3

**Compacted/summarized history is derived data.**

Never overwrite the original and call it equivalent.

### Rule 4

**MCP is an adapter.**

Domain logic must remain usable from HTTP/CLI/other adapters.

### Rule 5

**UI is optional for core operations.**

If an agent can reasonably create/configure something through the API, do not force a browser workflow.

### Rule 6

**Skills add capability/knowledge; Gates constrain behavior.**

Do not merge those abstractions.

### Rule 7

**Security enforcement happens server-side.**

Prompt guidance is not authorization.

### Rule 8

**Secrets are not artifacts.**

Use secret references/providers.

### Rule 9

**Do not invent orchestration until context sharing works well.**

### Rule 10

**Do not wait for protocol roadmap promises.**

Adopt standardized features when they actually exist and are usable; preserve compatibility through adapters.

---

# 28. Candidate first demo

A good demo should be brutally simple and prove the differentiator.

## Setup

Two users:

```text
Filip
Petr
```

One shared project:

```text
elastic-migration
```

Clients:

```text
Filip → Claude Code
Petr  → ChatGPT / another MCP-capable client
```

## Flow

1. Filip creates `elastic-migration` directly through the agent.
2. Filip adds Petr as CONTRIBUTOR.
3. Project is bound to a Git repository.
4. Project has one project skill describing the repository/migration conventions.
5. Agent A writes research artifact `es8-compatibility.md`.
6. Agent A creates a handoff.
7. Agent B resolves the same project and immediately sees that it is shared.
8. Agent B receives effective skills and reads the handoff/artifact.
9. Agent B creates a second artifact/version.
10. Audit/history shows user + client provenance.
11. A conflicting write demonstrates version protection.

If this works cleanly across different AI products, the core thesis is proven.

---

# 29. Working pitch

Short version:

> **Context Commander is a self-hosted shared context platform for humans and AI agents. It gives different AI clients a common project workspace with RBAC, artifacts, handoffs, skills, events, provenance and encrypted storage without locking the workflow to one AI vendor.**

Developer-oriented version:

> **A vendor-neutral context plane for multi-agent and multi-user workflows.**

Important distinction:

> Context Commander does not need agents to talk directly to each other. They collaborate by reading and writing shared, project-scoped state through a neutral layer.

---

# 30. First-push checklist

## Repository/bootstrap

- [ ] Choose final repository/project name.
- [ ] Write README with problem statement and non-goals.
- [ ] Add architecture decision document based on this cookbook.
- [ ] Choose implementation language/runtime.
- [ ] Define module layout.
- [ ] Add license.
- [ ] Add contribution/security policy skeletons.

## Domain

- [ ] Define User.
- [ ] Define ClientCredential.
- [ ] Reserve ServicePrincipal concept.
- [ ] Define Project.
- [ ] Define Membership + role/capability mapping.
- [ ] Define Artifact + ArtifactVersion.
- [ ] Define Handoff.
- [ ] Define Conversation + ExternalSession lineage.
- [ ] Define Skill + SkillVersion.
- [ ] Define Event.

## Persistence

- [ ] Metadata database.
- [ ] Storage interface.
- [ ] Local filesystem backend for development.
- [ ] S3-compatible backend — implemented in `internal/storage/s3` but not
      reachable from `ccd serve`; experimental, see §8.1.
- [ ] Artifact version conflict detection.

## Security

- [ ] Authentication boundary.
- [ ] Project authorization middleware/service.
- [ ] Audit records with user + client identity.
- [ ] Crypto provider abstraction.
- [ ] Application-level payload encryption plan/implementation.
- [ ] Secret-provider abstraction or explicit non-support for raw secret artifacts.

## MCP/API

- [ ] MCP adapter.
- [ ] Project create/list/resolve.
- [ ] Artifact list/read/write.
- [ ] Handoff create/read.
- [ ] Effective skill discovery.
- [ ] Error model including authorization and write conflict.

## Skills

- [ ] Installation scope.
- [ ] Project scope.
- [ ] Company scope data model or extension point.
- [ ] Writable registry.
- [ ] Immutable skill versions/checksums.
- [ ] Effective scope composition.

## Verification

- [ ] Test private project isolation.
- [ ] Test shared project access.
- [ ] Test unauthorized cross-project access.
- [ ] Test two clients writing same artifact.
- [ ] Test handoff between different clients.
- [ ] Test provider session compaction lineage without losing canonical conversation identity.
- [ ] Test encrypted storage does not leak artifact plaintext at rest.

---

# 31. Current architectural summary

The current design can be reduced to four statements:

```text
1. PROJECTS DEFINE SECURITY AND SHARED CONTEXT.

2. SKILLS DISTRIBUTE REUSABLE KNOWLEDGE/CAPABILITY;
   GATES ENFORCE POLICY.

3. ARTIFACTS + HANDOFFS + EVENTS LET INDEPENDENT AGENTS COLLABORATE.

4. CONTEXT COMMANDER OWNS STABLE IDENTITIES AND HISTORY LINEAGE;
   VENDOR SESSION IDS AND MCP DETAILS ARE EXTERNAL ADAPTER CONCERNS.
```

Everything in the first implementation should reinforce those statements rather than expand the product sideways.

