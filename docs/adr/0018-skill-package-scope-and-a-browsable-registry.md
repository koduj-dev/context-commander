# 0018. A skill package lives at a scope; the registry is browsable, with the installation shelf as its marketplace

## Status

Accepted

## Context

`docs/adr/0012` separated the immutable package (`Skill`/`SkillVersion`) from the installation
(`SkillBinding`), and gave the *binding* a scope: installation / organization / project. It said
nothing about the package. So "where does a published skill live, and who may see it" had no
answer in the model — only an accident of implementation: `skills` and `skill_versions` carried
no RLS, and nothing could reach a package's bytes at all until `skill.get` shipped in Phase 1.

That accident surfaced as a cross-project read. Migration 000020 recorded the publishing project
and narrowed reads to "a binding you can see, or the project you published from" as a stopgap,
explicitly deferring the product question.

Deferring it also left a second gap nobody had noticed: the registry had no **discovery**. A
package could only be reached through a UUID that came from somewhere else, so an agent could not
look for a skill it did not already know about. A registry you cannot browse is not a registry —
and an agent that cannot ask "what is here, and has it changed since I last looked" cannot keep
itself current.

Two candidate answers were on the table: one flat instance-wide catalog (simple, but publishing
becomes a public act whether or not the publisher meant it), or a package that carries its own
scope the way a binding does.

## Decision

**A package lives at a scope, and the scope vocabulary is the one `SkillBinding` already uses.**

- `Skill` carries `scope` (installation / organization / project) and `scope_ref`
  (migration 000021), the same shape and the same CHECK constraints as `skill_bindings`.
- A skill is **born in the project it was published from**. Publishing is therefore never
  accidentally a public act — the narrowest scope is the default, and no flag can widen it during
  publish.
- **`skill.promote` widens** the scope: project → organization → installation. It only ever
  widens; narrowing is refused, because anyone who could already see the skill may have installed
  it and pulling the shelf out from under them would break bindings that were legitimate when
  they were made. Promotion is authorized by `checkScopeTarget` — the same check `Install` uses to
  decide whether a caller may bind *into* a scope, because it is the same question: may you put
  something in front of everyone at this scope?
- **The installation scope is the marketplace shelf.** That is what "publish to the registry"
  means here, and it is a deliberate, authorized act (installation admin, docs/adr/0017) rather
  than a side effect of publishing.
- `SkillBinding`'s scope keeps its existing meaning, untouched: it says where a skill is
  *applied*, never who may *see* it. The two were only ever conflated because the package had no
  model of its own.
- **The catalog is browsable**: `skill.search` lists and searches by name, `skill.versions`
  enumerates a skill's versions. Both return exactly what the caller can see. Browsing requires
  `CapSkillRead` on an anchor project — a floor ("you are a member of something here"), not a
  filter: the catalog returned spans every scope the caller can see, not just the anchor.
- **Change detection is the checksum**, which `SkillVersion` already carries and which is computed
  over the plaintext package at publish time. A catalog entry reports its newest version's
  checksum, so a client remembers what it loaded and compares — never trusting a version number or
  a timestamp to decide whether to re-download.

## Consequences

- Visibility is enforced by RLS on `skills`, with `skill_versions` inheriting it through its
  parent (migration 000021), using the identical policy `skill_bindings` has had since
  migration 000007. This is a deliberate exception to "authorize in the domain service"
  (CONTRIBUTING rule 7): restating the predicate in Go would require resolving organization
  membership, which is keyed by user (docs/adr/0006) and not by the principal a read path carries.
  `internal/skills` tests the policy with RLS both on and off.
- Skill content stays unencrypted at rest (`docs/gotchas.md`). That remains a real caveat rather
  than a solved problem: a project-scoped skill is only as private as the database, and anything
  that must be confidential belongs in `internal/secrets` (project-key encrypted) or an artifact.
  The tool descriptions and CLI help say so where a caller can act on it.
- A *cross-instance* public marketplace is still out of scope and stays behind trust, signing and
  distribution (NEXT.md's "deliberately not doing"). This ADR is about one installation's own
  registry, not about federating between installations.
- Signed packages and an approval workflow (Phase 3) get more valuable, not less: once a catalog
  is browsable, "which of these do I trust" is a question an agent will actually ask.
  `trust_status` exists on `SkillVersion` for exactly that and is still always `unverified`.
