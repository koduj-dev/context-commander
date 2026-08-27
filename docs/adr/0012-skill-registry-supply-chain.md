# 0012. Skill package vs. skill installation are separate concepts; `skill.update` never edits published content

## Status

Accepted

## Context

Round 1 established Skill Registry as a distribution/trust platform, not a CRUD table
(`docs/architecture-cookbook.md` §12.4): `SkillVersion` is immutable, with trust metadata
(origin, checksum, signature, `contains_executable_code`, ...). Round 2 (§7) found a gap: nothing
said what happens to a project using `skill@1.2` when `skill@1.3` is published — does the project
silently move forward? That's a supply-chain decision, not a UI detail. Round 2's implementation
addendum 4 additionally tightened the *API* boundary: without an explicit rule, `skill.update`
invites an implementation that does `UPDATE skill_versions SET content = ...`, quietly breaking the
immutability invariant without ever touching the schema.

## Decision

- `Skill` / `SkillVersion` is the immutable package: content, checksum, trust metadata. Publishing
  new content always inserts a new `SkillVersion` row.
- `SkillBinding` (a.k.a. installation) is separate: `scope` (installation/organization/project),
  `skill_id`, `version` or a version constraint, `pinned`, `managed`, `overridable`,
  `installed_by`/`installed_at`. This is what "project X uses skill Y" actually means.
- `skill.publish` creates a new `SkillVersion`. It **never** touches an existing `SkillBinding`
  that has `pinned = true`.
- `skill.update` is scoped to **draft/metadata only** — name, description, declared capabilities on
  an unpublished draft. It has no code path that can mutate a published `SkillVersion`'s content.
  New content is only ever reachable through `skill.publish`.
- `skill.list_effective` resolves each scope's binding to a concrete `SkillVersion` (respecting
  `pinned`/constraint), not "whatever is newest."
- Override (a project skill shadowing an organization/installation skill of the same name) is
  explicit and audited via the binding's `managed`/`overridable` flags — an
  `overridable = false` installation/organization skill cannot be shadowed at all (guards against
  e.g. a project quietly shadowing a mandated `security-review` skill).
- The server never writes to a client's filesystem. It exposes `skill.resolve_effective` /
  `skill.export` / `skill.package`; a client-side adapter (`ccd sync-skills`) does the actual
  `.claude/skills/`-shaped write, because a remote server has no access to a local client's disk.

## Consequences

- `internal/skills` has two top-level types, `SkillVersion` (immutable) and `SkillBinding`
  (mutable), never one merged "Skill" struct that conflates package and installation.
- Publishing a new `SkillVersion` is a pure insert; it is a bug for that code path to touch
  `skill_bindings` at all.
- Audit log records `install` / `update` / `override` / `unpin` / `remove` as distinct actions
  against `SkillBinding`, separate from `publish` against `SkillVersion`.
