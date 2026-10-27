# 0021. Storage quotas per namespace, counted by the database

## Status

Accepted.

## Context

Nothing measured or limited what an installation accumulates. Artifact
versions are immutable and kept forever (ADR 0010), history objects are
append-only, and skill versions are never rewritten. An organization
deployment will want a ceiling, and wants it before the disk fills rather
than after (NEXT.md, Phase 3).

Three questions needed answers, and the maintainer decided the first two on
2026-10-27:

1. **What is limited.** A namespace: everything a user or an organization
   owns, across all of its projects. That is who pays for the disk, and it
   is one number an organization can watch, where a per-project limit gives
   a company with fifty projects fifty numbers.
2. **How the limit is set.** A server default (`--storage-quota`, none when
   unset, which is today's behaviour) plus a per-namespace exception an
   installation admin sets: a different limit, or no limit.
3. **What happens at the ceiling.** The write is refused. Silent eviction of
   a project's own context would be worse than the disk filling. Deletes and
   reads always work, so a namespace over its limit can always get back
   under it.

## Decision

**The database keeps the count.** `namespace_storage(namespace_id,
used_bytes, limit_bytes, unlimited)` (migrations/000030) is maintained by
triggers on `artifact_versions`, `history_objects` and `skill_versions`, on
insert and on delete. cc_app has no grant on the table. A write path added
later is counted whether or not its author knew the table exists, which a
counter maintained in Go could not promise.

**The same trigger refuses.** After charging the bytes it compares the
total against the effective limit, and raises SQLSTATE `CCQ01` if the total
went over. The insert and the charge are one transaction, so two concurrent
writes cannot both squeeze under the limit: the second one waits on the
first one's row lock and then sees its bytes. Only growth is checked. A
delete never fails because a namespace is over its limit, which can happen
when an admin lowers it.

**The server default reaches the trigger as a transaction setting.**
`db.WithPrincipal` sets `app.storage_quota_default` alongside
`app.current_principal_id`, in the same statement (ADR 0003). The flag stays
authoritative: nothing persists it, and a restart without it removes the
default. Two servers started with different defaults would enforce
different ones, which is a misconfiguration the same way two different root
keys would be.

**What is counted:**

- **Artifact versions** by their logical size (`size_bytes`), every version
  of every artifact that is not deleted. Deleting an artifact gives its bytes
  back at once, even though a soft delete keeps the versions (ADR 0013) until
  an operator purges them. Counting them until the purge would have been
  closer to the disk and useless to the user: a user cannot purge, so the
  quota would only ever go up, and a full namespace would stay full however
  much was deleted. What a soft delete leaves behind is the operator's
  storage, and the installation statistics still show it.
- **History objects** by the size of their ciphertext. That is what is
  stored, and differs from the plaintext by the 16-byte GCM tag. History is
  append-only and has no delete. Artifacts are what a full namespace can
  free.
- **Skill versions** by the size of the package (`size_bytes`, new in
  migration 000030), charged to the namespace that owns the skill's scope.
  An installation-scoped skill belongs to no namespace and is not counted.
  Promoting a skill moves its bytes to the new scope, and a promotion into
  a namespace without room is refused like any other write. Versions
  published before the migration were never measured. They count as zero
  rather than as a guess, because their size is only in the object store,
  which SQL cannot see.

Metadata is not counted: handoffs, events, audit, secrets, memberships. All
of it is bounded per request by the JSON body cap, and none of it is what
fills a disk.

**Large uploads are refused before they are uploaded.** Artifact writes and
skill publishes upload to the object store before the row that charges them
(ADR 0013). Both therefore ask `cc_check_*_storage` first. That check is
advisory, and the trigger stays authoritative: a write that passes the check
and then loses a race is refused at insert. Its object becomes an orphan
candidate, the case `SweepOrphanObjects` already exists for.

**Who sees what.** An installation admin sees every namespace's slug, kind,
usage and limit, and sets exceptions (`installation.storage_manage`). That is
counts per owner, never a path or a project inside a namespace, so it stays
on the side of the boundary migration 000024 drew. A user sees their own
namespace and those of their organizations (`GET /v1/me/storage`). A
refusal tells the writer that the namespace is full, and it does not give
the numbers. A project member who is not in the owning organization has no
other way to learn the organization's total.

## Consequences

- A refused write is `507 Insufficient Storage`, code `storage_quota_exceeded`,
  over HTTP. MCP gets the same sentence.
- Writes to one namespace serialize on its `namespace_storage` row for the
  rest of their transaction. Every one of those transactions is already
  short (ADR 0013 keeps uploads outside them), so this is a lock held for
  milliseconds, not for the length of an upload.
- The count is the logical size the database knows about, not disk usage.
  Object-store overhead, orphaned uploads and the database's own files are
  not in it, and the screen says so instead of calling it "disk".
- The default only reaches transactions opened by `db.WithPrincipal` on a pool
  that was given it, which is the server's. The CLI's `--database-url` commands
  are the operator tier, the same one that runs migrations, and they see only an
  admin's exceptions.
- An organization's namespace is created along with its first project. Until it
  exists, the organization's skills are not counted against anything.
- Purging, retention and legal hold (NEXT.md) are still separate work. Until
  they exist, a namespace frees quota only by deleting artifacts, and what
  those deletes leave behind on disk is reclaimed only by an operator's
  purge.
