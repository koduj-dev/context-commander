# Context Commander — UI Specification

> **Status:** implementation guidance / UI contract  
> **Audience:** frontend implementers and coding agents  
> **Scope:** Context Commander web UI  
> **Important:** the UI is a frontend over the existing Context Commander domain/application API.  
> It must not become a second source of business logic.

---

# 1. Core rule

Context Commander is API/domain-first.

The web UI is a management frontend over the same application APIs used by MCP, HTTP and CLI.

**Do not implement functionality only in the UI.**

A user or agent must be able to perform core operations without opening a browser.

Examples:

- create and resolve a project,
- manage project membership,
- read/write artifacts,
- create/read handoffs,
- inspect effective skills,
- manage secrets by reference,
- consume events,
- manage credentials where the API allows it.

The UI exists to make the platform understandable, inspectable and manageable by humans.

---

# 2. Brand source of truth — MANDATORY

The repository already contains the Context Commander brand package under:

```text
docs/brand/
```

**Use those files. Do not invent a second visual identity.**

Expected contents include assets such as:

```text
docs/brand/
├── README.md
├── UI-COLORS.md
├── context-commander-brand-manual.pdf
│
├── svg/
│   ├── cc-mark-on-light.svg
│   ├── cc-mark-on-dark.svg
│   ├── cc-mark-mono-black.svg
│   ├── cc-mark-mono-white.svg
│   ├── cc-favicon.svg
│   ├── cc-app-icon-dark.svg
│   ├── cc-app-icon-light.svg
│   ├── cc-logo-horizontal-on-light.svg
│   ├── cc-logo-horizontal-on-dark.svg
│   ├── cc-logo-horizontal-duotone-on-light.svg
│   ├── cc-logo-horizontal-duotone-on-dark.svg
│   ├── cc-logo-stacked-on-light.svg
│   ├── cc-logo-stacked-on-dark.svg
│   ├── cc-logo-stacked-duotone-on-light.svg
│   └── cc-logo-stacked-duotone-on-dark.svg
│
├── png/
│   └── ...
│
└── tokens/
    ├── brand-colors.css
    ├── brand-colors.json
    ├── ui-colors.css
    └── ui-colors.json
```

If filenames differ slightly because the package was reorganized, inspect:

```text
docs/brand/README.md
docs/brand/UI-COLORS.md
```

before writing UI code.

## 2.1 Do not redraw the logo

Use the supplied SVG assets.

Do **not**:

- recreate the logo in CSS,
- redraw it from a screenshot,
- replace it with a generic AI/chat icon,
- add gradients, glow or shadows to the canonical mark,
- swap the pink and violet sides,
- recolor AI vendors using Context Commander brand colors.

## 2.2 Typography

Use a clean modern sans-serif UI font.

Preferred:

```text
Inter
```

No novelty cyberpunk / terminal / “AI” display font is needed.

The brand identity is carried primarily by the symbol. The wordmark and UI typography should remain readable and durable.

## 2.3 Brand colors

Canonical colors:

```text
Context Pink    #FF2D8D
Relay Violet    #7A35F5
Void            #0B0B12
Paper           #FFFFFF
Mist            #F4F3F8
```

Use semantic tokens from:

```text
docs/brand/tokens/ui-colors.css
docs/brand/tokens/ui-colors.json
```

Do not scatter random near-equivalent pink/purple values throughout the frontend.

---

# 3. UI visual principle

The UI should be mostly neutral.

Brand colors have meaning:

```text
Pink     = active/current context
Violet   = shared/relay/inherited context
Green    = success/healthy
Amber    = warning/degraded
Red      = error/denied/destructive
Blue     = informational
Gray     = neutral/inactive/metadata
```

Do not turn the application into a full-screen pink-purple gradient.

Use the brand mainly for:

- logo,
- active navigation state,
- active context,
- shared/relay relationships,
- selected elements,
- important contextual accents.

---

# 4. Theme support

Support:

```text
System
Dark
Light
```

Persist theme preference client-side.

Dark mode is the strongest/default brand expression.

Recommended dark foundation:

```text
Background        #0B0B12
Surface 1         #14141D
Surface 2         #1B1B26
Surface 3         #242431
Border            #2F2E3A
Border strong     #444150

Primary text      #F8F7FB
Secondary text    #C3C0CB
Muted text        #8D8997

Primary accent    #FF2D8D
Secondary accent  #7A35F5
```

For the exact light/dark semantic palette, use:

```text
docs/brand/UI-COLORS.md
```

---

# 5. Application shell

Recommended layout:

```text
┌───────────────────────────────────────────────────────────────┐
│ Top bar                                                       │
├─────────────────┬─────────────────────────────────────────────┤
│                 │                                             │
│ Sidebar         │ Main content                                │
│                 │                                             │
│                 │                                             │
└─────────────────┴─────────────────────────────────────────────┘
```

## 5.1 Sidebar

Sidebar should be collapsible.

Expanded:

- icon,
- label,
- optional badge/count.

Collapsed:

- icon only,
- tooltip on hover/focus.

Recommended top-level navigation:

```text
Context Commander

Dashboard
Projects
Organizations
Skill Registry

Administration
```

Administration entries are permission-dependent.

## 5.2 Project navigation

When a project is selected:

```text
<namespace>/<project>

Overview
Artifacts
Handoffs
Conversations
Events
Skills
Gates
Secrets
Integrations
Members
Audit
Settings
```

Do not flatten all project-specific pages into the global sidebar.

The active project must always be visually obvious.

---

# 6. Dashboard

Dashboard is a convenience surface, not a separate domain.

Recommended content:

- recently accessed projects,
- recent project activity,
- latest active handoffs,
- meaningful warnings,
- failed/denied gate events worth human attention,
- recent changes in shared projects.

The dashboard should answer:

> What changed, where was I working, and is anything waiting for my attention?

Avoid vanity charts unless real product data justifies them.

---

# 7. Projects

## 7.1 Project list

Display:

```text
namespace/slug
display name, if separate
personal vs organization namespace
shared/private state
current user's role
member count
last activity
repository/integration indicator
```

Useful filters:

```text
All
Personal
Shared
Organization
Recently active
```

## 7.2 Create project

Project creation in the UI is a convenience only.

It must call the same project creation API used by agents/CLI.

Do not create frontend-only semantics.

The creator becomes owner according to the domain rules.

---

# 8. Project overview

The overview represents the resolved project context.

Display prominently:

```text
namespace / project slug
canonical project ID
shared/private state
current principal role
member count
organization / personal namespace
```

Also show:

- repositories,
- integrations,
- effective skills summary,
- active gates summary,
- latest active handoff,
- recent artifact updates,
- recent events.

The page should make it immediately obvious:

- which project is active,
- whether it is shared,
- who the user acts as,
- what role/capabilities apply,
- what inherited/project context is active.

---

# 9. Artifacts

Artifacts are first-class versioned project resources.

## 9.1 Artifact browser

Recommended list columns:

```text
Path / name
Type
Size
Version
Updated
Actor
Client
```

Support:

- search/filter,
- logical path navigation where applicable,
- sorting,
- upload/create where permitted.

## 9.2 Artifact detail

Show:

- logical path/name,
- content type,
- current version,
- size,
- timestamps,
- creator principal,
- client provenance,
- source/provenance links,
- related handoff/session where available.

Actions:

- read/preview,
- download,
- write/update,
- delete/tombstone,
- version history.

## 9.3 Version history

Show:

```text
Version
Timestamp
Principal
Client
Parent version
Relevant provenance
```

Do not expose crypto implementation details as normal user-facing metadata.

Do not treat an object-store ETag as the canonical content identity.

---

# 10. Handoffs

Handoffs are first-class structured continuation state.

They are not generic Tasks and they are not hidden reasoning.

For v1:

**Do not introduce a Task domain because an older draft mentioned `referenced_tasks`.**

## 10.1 Handoff list

Show:

- status,
- summary/title,
- creator principal,
- creator client,
- source session/run if known,
- referenced artifact count,
- created time.

Provide an obvious workflow for:

```text
Latest active handoff
```

## 10.2 Handoff detail

Render structured/human-readable content:

```text
Summary
Completed work
Next actions
Referenced artifacts
Source session/run
Status
Creator principal
Creator client
Created at
```

No chain-of-thought UI.

Handoffs contain explicit working state only.

---

# 11. Conversations and history lineage

Context Commander owns stable conversation identity.

Provider session IDs are opaque metadata.

## 11.1 Conversation list

Show:

- stable CC conversation ID,
- project,
- created time,
- latest provider/client,
- number of external sessions/history objects,
- recent activity.

## 11.2 Conversation detail

Recommended visualization:

```text
Conversation
│
├── ExternalSession A
│      └── raw history
│
├── ExternalSession B
│      relation: COMPACTED_FROM A
│      └── compacted history
│
└── ExternalSession C
       relation: CONTINUES B
```

Supported lineage relationships may include:

```text
CONTINUES
COMPACTED_FROM
FORKED_FROM
IMPORTED_FROM
SUMMARIZED_FROM
```

A compacted history object must not visually replace its raw source.

Make lineage explicit.

Unknown is better than a guessed relationship.

---

# 12. Events

Events are collaboration/activity infrastructure.

They are not an orchestration promise.

## 12.1 Event feed

Show chronological events such as:

```text
project.created
project.member_added
artifact.created
artifact.updated
artifact.deleted
handoff.created
skill.installed
skill.updated
```

Display:

```text
event type
timestamp
principal
client
resource
result/state
```

Filters may include:

```text
event type
actor
client
resource type
time range
```

Do not claim that an event automatically woke a passive AI chat.

---

# 13. Skills

Skills and Gates are separate concepts.

A Skill describes:

> what an agent can know or do.

A Gate describes:

> what must or must not happen around an operation.

Do not merge their UI.

## 13.1 Project Skills

Project Skills page should show the effective resolved set from:

```text
installation
+ organization
+ project
```

For each resolved skill display:

- name,
- resolved version,
- source scope,
- pinned/unpinned state,
- managed state,
- override state,
- trust/provenance indicators.

Overrides must be visible.

Never silently hide an installation/org skill because a project skill has the same name.

## 13.2 Skill Registry / Marketplace

Marketplace is the UI/distribution experience over the Skill Registry.

Support:

- browse/search,
- detail,
- versions,
- install/bind,
- pin,
- update,
- edit,
- publish where permitted.

Skill detail should show:

```text
Name
Version
Author
Origin
Scope
Checksum
Trust status
Declared capabilities
Files/resources
Executable content
Network/tool requirements
```

Important indicators:

```text
✓ authored locally
✓ signed
✓ approved by organization
⚠ contains executable scripts
⚠ requests network/tool access
```

A skill is a software supply-chain object, not merely Markdown.

---

# 14. Gates

Gates are server-side enforcement.

Do not present them as “AI instructions”.

Recommended project Gate page:

```text
Gate
Enabled
Scope
Hooks
Configuration
Managed
Overridable
Last decision / status
```

Example built-in gates:

```text
secret-scanner
require-handoff
protected-artifacts
approval-required
PII-policy
source/provenance-required
```

Decision states:

```text
ALLOW
DENY
ALLOW_WITH_GUIDANCE
```

Use semantic colors:

```text
ALLOW                success
DENY                 danger
ALLOW_WITH_GUIDANCE  warning/info
```

Do not make every Gate red simply because it is a policy mechanism.

---

# 15. Secrets

Secrets are **not Artifacts**.

The UI works primarily with stable references:

```text
secret://...
```

## 15.1 Secret list

Show metadata only:

```text
Reference/name
Scope/project
Version
Updated at
Updated by
Backend/provider metadata, if safe
```

## 15.2 Secret operations

Possible actions:

- create,
- replace,
- rotate,
- delete/revoke,
- copy the reference identifier.

Do not expose secret plaintext in:

- list views,
- search results,
- event payloads,
- audit rendering,
- ordinary detail responses,
- previews.

If a privileged reveal/resolve operation exists, it must be explicit, capability-controlled and audited.

No default “eye icon” plaintext reveal.

---

# 16. Integrations

Integrations belong to project context.

Possible bindings:

- Git repository/repositories,
- issue tracker,
- CI,
- documentation space,
- object-store resource,
- external MCP/API endpoints,
- other project resources.

Display:

```text
Provider/type
Canonical identifier / URL
Default branch where relevant
Status
Secret reference
Metadata
```

Credentials must not live directly inside integration metadata.

Use secret references, e.g.:

```text
secret://project/.../github
```

---

# 17. Members and RBAC

Project is the primary authorization boundary.

Initial project roles:

```text
OWNER
ADMIN
CONTRIBUTOR
READER
```

Members page must support Principals, not just human users.

Possible principal types:

```text
User
ServicePrincipal
```

Client credentials acting on behalf of a User are not separate project members.

Display:

```text
Principal
Type
Role
Added at
Added by
```

Actions:

- add principal,
- change role,
- remove member,
- inspect role/capability explanation.

Organization membership must not be displayed as if it automatically grants project content access.

---

# 18. Audit

Audit must preserve both:

```text
Principal
Via client
```

Example:

```text
Filip
via Claude Code
artifact.write
project: acme/example
result: success
```

Do not collapse this into:

```text
Claude Code modified file
```

or only:

```text
Filip modified file
```

Recommended columns:

```text
Timestamp
Principal
Client
Action
Resource
Result
Request/Event ID
```

Audit UI must never dump arbitrary sensitive payloads.

---

# 19. Organizations

Organizations extend the model.

They do not redefine standalone/local Context Commander.

Recommended organization navigation:

```text
Overview
Projects
Members
Skills
Policies / Gates where supported
Settings
```

Organization overview may show:

- namespace,
- project count,
- member count,
- organization-level skills,
- managed policies/gates.

Organization role must not imply project-content access unless an explicit policy/capability says so.

---

# 20. Administration

Administration is installation-scoped and permission-controlled.

Possible sections:

```text
Users
Client Credentials
Service Principals
Installation Skills
Installation Gates / Policies
System Settings
```

## 20.1 Users

Show principals/users without exposing credentials.

## 20.2 Client Credentials

Typical clients may include:

```text
ChatGPT
Claude Chat
Claude Code
Codex
custom clients
```

Display:

- credential label,
- associated principal,
- created time,
- last used,
- active/revoked state.

Allow credential revocation independently from project membership.

## 20.3 Service Principals

Examples:

```text
ci-agent
release-agent
nightly-review-agent
research-daemon
```

Do not model these as fake human users.

---

# 21. Provider/client visual identity

Do not use Context Commander Pink/Violet as vendor identity colors.

Example:

```text
Claude Code   neutral label + provider icon
ChatGPT       neutral label + provider icon
Codex         neutral label + provider icon

Context state / relationships:
Pink / Violet
```

Context Commander colors describe Context Commander state and relationships, not the AI vendor.

---

# 22. Loading, empty and error states

Every major screen should intentionally support:

```text
loading
empty
permission denied
not found
conflict
backend unavailable
gate denied
```

Do not render a `403` as an empty list.

Do not turn a `409` optimistic-concurrency conflict into a generic “Save failed”.

Useful conflict message:

> This artifact changed since you loaded it. Read the newest version, merge and retry.

---

# 23. Security-sensitive UI rules

Never put secrets, tokens or arbitrary sensitive content into:

- toast messages,
- analytics,
- client-side console logs,
- audit rendering,
- route names,
- browser titles,
- breadcrumbs where sensitive paths would leak.

Project/artifact names and paths may themselves contain sensitive data.

Metadata is not automatically harmless.

---

# 24. Responsive behavior

Desktop-first management UI is acceptable.

Still support reasonable smaller layouts:

- collapsible sidebar,
- horizontally scrollable or card-based tables where needed,
- viewport-safe dialogs,
- no layout requiring a fixed 1920 px viewport.

Do not spend disproportionate v1 effort on mobile-first polish.

---

# 25. Reusable components

Prefer shared components for recurring domain concepts:

```text
PageHeader
ProjectContextHeader
DataTable
StatusBadge
ScopeBadge
PrincipalBadge
ClientBadge
EmptyState
ConfirmDialog
DangerZone
VersionTimeline
EventFeed
TrustBadge
GateDecisionBadge
```

Do not create page-specific copies of the same domain representation.

---

# 26. Suggested status badges

Examples:

```text
Shared               violet
Active context       pink
Healthy              green
Pending              blue / neutral
Warning              amber
Denied               red
Revoked              red
Managed              neutral / violet
Pinned               neutral
Override             warning
Executable skill     warning
Signed / verified    green
Unknown trust        amber
```

Always combine color with text and/or icon.

Never rely on color alone.

---

# 27. UI non-goals for v1

Do not block the release on:

- autonomous multi-agent orchestration UI,
- a full Task engine,
- public marketplace federation,
- enterprise IAM suite,
- organization groups,
- semantic/vector search UI,
- graphical workflow designers,
- fancy animated topology graphs,
- “AI is thinking” theater,
- model-to-model chat UI,
- automatic waking of passive AI chats,
- excessive visual polish before core workflows work.

---

# 28. Minimum useful UI workflow

The initial UI should prove this flow:

```text
1. User authenticates.
2. User sees accessible projects.
3. User opens a shared project.
4. Overview shows role, members, repositories/integrations and effective skills.
5. User opens Artifacts and reads an artifact.
6. User inspects artifact version/provenance.
7. User opens the latest Handoff.
8. User sees referenced artifacts and next actions.
9. User inspects Events/Audit and sees principal + client provenance.
10. User sees effective Skills and visible overrides.
11. User inspects active Gates.
12. User manages Secret references without exposing plaintext.
```

If these workflows are awkward, adding more screens is not the solution.

---

# 29. Frontend acceptance criteria

The implementation is acceptable when:

- [ ] it uses brand assets from `docs/brand/`,
- [ ] it reads `docs/brand/README.md`,
- [ ] it reads `docs/brand/UI-COLORS.md`,
- [ ] it uses `docs/brand/tokens/`,
- [ ] it reuses SVG files from `docs/brand/svg/`,
- [ ] dark/light/system themes work,
- [ ] navigation clearly distinguishes global and project scope,
- [ ] the active project context is always obvious,
- [ ] Project Overview exposes shared state, role, members and effective context,
- [ ] Artifacts support list/detail/version history,
- [ ] Handoffs support list/detail/latest-active flow,
- [ ] Conversations preserve visible lineage instead of flattening provider sessions,
- [ ] Events show principal + client provenance,
- [ ] Skills show scope, resolved version, trust and override state,
- [ ] Gates remain conceptually and visually separate from Skills,
- [ ] Secrets are represented by references/metadata, not plaintext,
- [ ] Integrations use secret references rather than embedded credentials,
- [ ] Members support both User and ServicePrincipal concepts,
- [ ] Audit preserves both principal and client,
- [ ] permission errors are not rendered as empty state,
- [ ] optimistic-concurrency conflicts have dedicated UX,
- [ ] brand colors are accents rather than a full-screen neon theme,
- [ ] provider identities remain visually neutral,
- [ ] core application behavior remains in backend/domain APIs rather than frontend-only state.

---

# 30. Mandatory implementation instruction

Before changing UI styling, layout or brand-related components:

```text
1. Read docs/brand/README.md
2. Read docs/brand/UI-COLORS.md
3. Inspect and reuse docs/brand/svg/*
4. Reuse docs/brand/tokens/*
5. Follow this UI specification
6. Follow the existing backend/domain model
```

If the backend does not expose an operation described in this document:

**do not fake it in frontend state.**

Mark the feature as unavailable/TODO and implement or request the corresponding backend/API capability.

The frontend must represent the real Context Commander domain model, not invent a parallel one.
