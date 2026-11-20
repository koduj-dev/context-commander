# 0034. A project manifest: what an arriving agent must read

## Status

Accepted, 2026-11-16.

Replaces, for projects that have one, the `context/` convention that
`context.bootstrap` has used since Phase 1 (`internal/contextpack`).

## Context

`context.bootstrap` carries, after the artifacts the current handoff names,
every artifact under `context/`, as long as the byte budget lasts. That
convention has three limits the roadmap named:

- Required reading often lives elsewhere (`docs/architecture.md`,
  `docs/adr/`), and moving it under `context/` to be read is a write made for
  the tool's sake.
- Order is by path, not by importance, so a long file early in the alphabet
  can push out the one that mattered.
- A long file is either carried whole or left out whole. Often its first
  screen is what a newcomer needs.

## Decision

**The manifest is an artifact at `context/manifest.json`.** It is
versioned, written with `artifact.write` like any other file, readable by
everyone who can read the project, and changed by a write to the project
rather than to the server's configuration:

```json
{
  "version": 1,
  "budget": 200000,
  "read": [
    {"path": "context/overview.md", "why": "what the project is"},
    {"path": "docs/architecture.md", "max_bytes": 20000},
    {"prefix": "docs/adr/", "max_bytes": 4000}
  ]
}
```

- **`read`** lists paths or prefixes, in priority order, up to 100 entries.
  A prefix is listed in the query, like the convention, up to 100 artifacts.
- **`max_bytes`** caps one entry. A longer artifact is carried cut at that
  size, never inside a UTF-8 sequence, and marked `truncated`; `size_bytes`
  stays the whole file's, and `artifact.read` returns the rest.
- **`why`** travels with the artifact in the pack, so the agent knows why it
  is reading it.
- **`budget`** is the pack's default budget. A caller's own budget still
  wins.

**Selection order is unchanged in spirit.** A caller's explicit
`artifact_paths` replace everything. Otherwise the artifacts the current
handoff names come first, then the manifest's entries; with no manifest,
the `context/` convention as before. The manifest itself is never carried
as content.

**A manifest that cannot be used never costs the pack.** Invalid JSON, an
unknown field (a misspelled `max_byte`), a wrong version or an entry with
both or neither of `path` and `prefix` is reported in `budget.truncated`
under `context/manifest.json`, and the pack falls back to the convention.
An entry naming a missing file is reported like any missing path.

## Consequences

- A project chooses its required reading where it already keeps it, in the
  order that matters, at the length that matters.
- The Context tab shows each artifact's `why` and, for a cut one, how much
  was included.
- *Amended 2026-11-20:* a manifest is checked when it is written. The
  original concern, that a write-time check would put pack rules into the
  artifact service, is met by keeping the rule out of it: the artifact
  service takes a content check per exact path
  (`artifacts.Service.CheckContentAt`), and `ccd serve` and `ccd artifact
  write` register `contextpack.CheckManifest` for `context/manifest.json`.
  A refused write is `422 invalid_content` over HTTP and a tool error over
  MCP, both naming the problem; nothing is stored. A manifest written
  before this change, or brought in by `ccd project import`, is still
  reported by the next pack and falls back to the convention, as above.
