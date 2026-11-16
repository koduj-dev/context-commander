# 0036. Project deletion, legal hold and purge

## Status

Accepted, 2026-11-16. The first part of Phase 3's "retention rules, legal
hold, controlled purge workflow", which the maintainer put first.

## Context

Nothing could be removed for good. A project could not be deleted at all;
a deleted artifact only got a `deleted_at`, and its versions stayed in the
database and the object store forever (`PurgeArtifact` existed, and nothing
called it). An import that stopped part way (ADR 0035) left a project nobody
could clean up. And there was no way to say that a project must not lose
anything, whatever its members do.

## Decision

### Deleting a project: scheduled, then purged

An OWNER deletes a project from its Settings tab, typing its path to
confirm: `DELETE /v1/projects/{ns}/{slug}` with `{"confirm": "ns/slug"}`.
It has to be a person signed in to the interface; an agent's token gets
`403 needs_person`, as for accepting an idea or sending a message.

**From that moment the project is gone for everyone.** Migration 000047
changes `cc_has_project_access`, which every project-scoped policy calls, to
answer false for a project scheduled for deletion. Every list, read and
write stops at once, through row-level security, without a change to any of
them. `AuthzService.Can` refuses too, because it reads the membership
through the same policies.

**Its OWNERs can bring it back for seven days.** Nothing else lists the
project any more, so `GET /v1/me/projects/pending-deletion` and a panel on
the home screen show an OWNER their scheduled projects, and
`POST …/pending-deletion/{id}/cancel` restores one. Scheduling, cancelling
and listing go through SECURITY DEFINER functions, because the policies
hide the project from its OWNERs too. Each checks itself that the caller is
an OWNER, and they raise their own SQLSTATEs (`CCD01` not an owner, `CCD02`
held, `CCD03` wrong state) so the application can name the refusal.

**After the grace period, an operator's `ccd purge` removes it.** Purging
runs as `cc_owner`: it deletes rows the application may not, artifact
versions above all. It deletes the project's rows from every table that has
a `project_id`, found in the catalogue rather than listed in code so a
table added later is not forgotten, in rounds until the foreign keys
between them allow each. Then it deletes the project's skill and gate
bindings, the project row, and every object under the project's prefix in
the store, including orphans an interrupted write left. A row in
`project_purges` records which project, who asked and when, with names
only. The security log keeps its rows; they hold no content.

`cc_app` loses UPDATE on `projects` altogether. Nothing in the application
updated a project row, and the deletion and hold columns must not be
writable by it.

### Deleted artifacts

`ccd purge` also removes the versions of artifacts deleted more than 30
days ago (`--deleted-artifacts-after`, 0 keeps them), database rows first
and stored objects after the commit, as ADR 0013 orders it.

### Legal hold

`ccd project hold --project ns/slug --reason …`, as an operator, places a
hold; `--release` lifts it. A held project cannot be scheduled for deletion
(`409 legal_hold`), placing a hold cancels a scheduled deletion, and every
purge skips the project, its deleted artifacts included. Only an operator
can place or lift one: a hold exists precisely so that the project's own
members cannot remove its content.

### Running it

`ccd purge` is an operator command, not something the server does by
itself, because purging needs the owner's privilege the server must never
hold (ADR 0002). The packages ship `ccd-purge.service` and a daily
`ccd-purge.timer`, not enabled: purging cannot be undone, so an operator
turns it on. `--dry-run` says what it would remove. `ccd project purge
--project ns/slug --yes` removes one project at once, whatever its state,
except under a hold.

## Consequences

- A project can be deleted by its OWNER, undone for a week, and is then
  removed for good, including from storage.
- Storage no longer only grows.
- Retention by age for memories, history and the audit trail is not part of
  this. Those are records a team may need to keep, and a rule that deletes
  them wants its own decision.
- Backups taken before a purge still contain the project. Backups are the
  operator's to rotate.
