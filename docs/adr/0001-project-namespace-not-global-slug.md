# 0001. Project identity: UUID + namespace-scoped slug, not a global slug

## Status

Accepted

## Context

The original draft used `projects.slug TEXT UNIQUE`. Review round 1 (blocker 1) pointed out this
makes it impossible for two users, or a user and an organization, to both have a project called
`es-migration` — `filip/es-migration` and `acme/es-migration` must be able to coexist. A project's
*name* is not a global identity; only project membership defines who can act on it
(`docs/architecture-cookbook.md` §4: "Project is the security boundary").

## Decision

- `projects.id` (UUID) is the canonical identity, used in every internal reference.
- `namespaces` table holds human-readable identity: `kind` (`personal` | `organization`), a
  globally-unique `slug`, and exactly one of `owner_user_id` / `organization_id`.
- `projects.slug` is unique only within its `namespace_id` (`UNIQUE(namespace_id, slug)`).
- `project.resolve("filip/es-migration")` splits on `/`, resolves the namespace by slug, then the
  project by `(namespace_id, slug)`.
- Personal and organization projects use the *same* `projects` table and the *same* membership
  model — no parallel "personal project" type (cookbook §5.2).
- Project namespace transfer (`filip/foo` → `acme/foo`) is explicitly **out of scope** for MVP
  (review round 2, §15) — it touches ownership, admin authority, and skill inheritance semantics
  that deserve their own ADR later. A project is created directly in its target namespace.

## Consequences

- Every `project.create` call must resolve or create a namespace first.
- `UNIQUE(namespace_id, slug)` plus `project.id` as the join key everywhere means no code should
  ever key off `projects.slug` alone.
