# 0042. Deleting a user account

## Status

Accepted, 2026-11-18. Closes the open point of CRA Annex I (2)(m) in
`docs/cra/essential-requirements.md`.

## Context

A project could be deleted and a member could leave one, but an account
could not be deleted at all. The person's email, name, password, directory
links and tokens stayed forever. Their content cannot simply go with them:
memories, plans, artifact versions and audit rows name their author by
principal, and those references keep the project's history readable.

## Decision

**Deleting an account removes the person, not their work.** One SECURITY
DEFINER function, `cc_delete_user` (migration 000050), in one transaction:

- replaces the email with `deleted-<id>@deleted.invalid` and the name with
  "Deleted user", and stamps `users.deleted_at`;
- removes every project and organization membership, the installation
  administrator role, the password and every directory link;
- revokes every session and token;
- revokes pending invitations to the old address, so whoever signs up with
  it next does not walk into those projects;
- leaves the users and principals rows, so every record still names an
  author.

**Who and how.** The person themselves (`DELETE /v1/me`, and **Delete
account** in Preferences) or an installation administrator
(`DELETE /v1/installation/users/{id}`), signed in to the interface (a token
cannot), confirming with the account's email address.

**What blocks it.** Being the last OWNER of a project (one not already
scheduled for deletion) or of an organization, or the last installation
administrator. The refusal names the projects and organizations, so the
person knows whom to promote first. Nothing is ever left without an owner.

A deleted account disappears from the user directory. Its email is free: a
later sign-in with it, through OIDC or a new invitation, is a new account
with nothing of the old one.

## Consequences

- A person can leave the installation completely, and an administrator can
  remove someone who left the team.
- Their contributions stay, attributed to "Deleted user". Deleting the
  content too is the project's decision, through project deletion (ADR
  0036).
- Backups taken before the deletion still hold the old email.
