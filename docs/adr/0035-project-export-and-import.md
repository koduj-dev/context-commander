# 0035. Project export and import

## Status

Accepted, 2026-11-16.

## Context

A backup (docs/backup-restore.md) is the whole installation: one database,
one object store, one root key. Nothing could move a single project to
another installation, hand it to someone who runs their own, or keep a
copy of one project outside the server. The roadmap's Phase 2 names
"project export/import for backup and migration".

The maintainer decided the scope on 2026-11-16: content without secrets,
with authors mapped by email where the target installation knows them.

## Decision

### The archive

A `.tar.gz` with one JSON file per kind of content and a `blobs/`
directory, every content blob named by its SHA-256 and stored once:

| File | What |
|---|---|
| `artifacts.json` | every artifact that is not deleted, with every version, oldest first |
| `memories.json` | every memory, whatever its status, with its body |
| `plans.json` | phases, and every plan with its items, abandoned plans included |
| `ideas.json` | every idea with its proposal or rejection |
| `handoffs.json` | every handoff with its body; referenced artifacts by path |
| `history.json` | conversations, their external sessions, objects (bodies in `blobs/`) and lineage |
| `people.json` | every principal the records name: kind, email, name |
| `manifest.json` | format, version, project, counts, what is excluded, `complete: true` |

The archive is **plaintext**. Encryption at rest protects data inside the
server under keys that never leave it; an archive is the deliberate way out,
and re-encrypting it would need a key the operator then has to keep next to
the file anyway.

**Never in an archive:** secrets, skills and policies, members and
invitations, the audit trail, events, usage and presence, messages. The
manifest lists them, so a reader does not have to infer them from absence.

**The manifest is written last.** An export that fails half way, or a
download cut off, has no manifest or no `complete: true`, and import
refuses it.

### Export

`GET /v1/projects/{ns}/{slug}/export` streams the archive, and the
project's Settings tab downloads it. It needs `project.export`, a new
capability held by OWNER and ADMIN: it reads everything a CONTRIBUTOR can,
at once, into a file that leaves the server.

`internal/portability` reads through the domain services as the caller, so
every read is authorized and decrypted by the package that owns the data.
Ids in the archive are the source installation's; they tie records together
(a memory's plan, an idea's resulting plan, an object's parent) and mean
nothing elsewhere.

### Import

Import is an operator command, `ccd project import`, connected as
`cc_owner`, into a new project whose OWNER the operator names by email:

```bash
ccd project import --database-url "$OWNER_DSN" --file work.ccproject.tar.gz \
  --project team/work --owner filip@example.com
```

The archive is read and checked first (format, version, `complete`, every
JSON file, every blob against its hash), so a broken archive is refused
before a project exists. Records are then written through the domain
packages as the owner, validated and encrypted under the new project's key,
with new ids and the archive's cross-references rewritten (a memory's plan
and step, what superseded it, an idea's resulting plan, a handoff's
artifacts, an object's parent). Plaintext metadata is restored afterwards in
one transaction: statuses, positions, who and when. A principal is mapped by
email to a user of the target installation, and otherwise to the owner.

Two things do not come back as they were: a claimed idea comes back open (a
claim is a lease held by a client of the old installation), and every write
is in the audit trail as the owner's, at the time of the import.

If an import stops part way, the project exists with what was written
before the error; `ccd project purge --project ns/slug --yes` removes it
(ADR 0036).

Restoring authorship is why import is an operator action and not an
endpoint: a member able to import could attribute content to anyone whose
email they know.

## Consequences

- A project can be kept, moved or handed over on its own, without the root
  key or the rest of the installation.
- An archive is as sensitive as the project's content. The Settings tab says
  so next to the button.
- Secrets have to be set again after an import.
