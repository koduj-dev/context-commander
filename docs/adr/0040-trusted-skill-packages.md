# 0040. Trusted skill packages: signatures and approvals

## Status

Accepted, 2026-11-17. Phase 3's "signed/trusted skill packages and an
approval workflow". `trust_status` has existed on `SkillVersion` since ADR
0012 and was always `unverified` (ADR 0018).

## Context

A skill is an instruction an agent follows. Anyone allowed to publish into
a scope can put one on its shelf, and a browsable catalog (ADR 0018) makes
"which of these do I trust" a question an agent and its operator actually
ask. Nothing answered it.

## Decision

A skill version is **trusted** when it is either:

**Signed.** An installation administrator registers Ed25519 public keys
(`POST /v1/installation/skill-signers`; listed at
`GET …/skill-signers`, revoked with `DELETE …/{id}`). A publisher signs

```text
context-commander/skill/v1
<skill name>
<SHA-256 of the content, hex>
```

and passes the signature with `skill.publish`, `POST /v1/skills` or
`ccd skill publish --sign-key`. The server verifies it against the active
keys when the version is inserted and records `trust_status = 'signed'` and
`signed_by`. A signature that verifies against no active key is a refusal
(`400 bad_signature`), never a silently unsigned version. The version
number is not signed, because the server assigns it. `ccd skill keygen`
and `ccd skill sign` work offline, so the private key never needs to be on
the server.

Revoking a key stops it from signing new versions. Versions it signed stay
signed: the signature was valid when it was checked, and a version is
immutable.

**Approved.** A person reviews an unsigned version and approves it
(`POST /v1/skills/versions/{id}/approve`, or **Approve this version** on the
skill's page). It has to be a person signed in to the interface, not an
agent's token; not the person who published it; and someone allowed for
the skill's scope: an installation administrator for installation and
organization skills, an organization manager for organization skills, a
project ADMIN or OWNER for project skills. An approval is a row of its own,
because `skill_versions` stays immutable, and it is not undone: a bad
version is replaced by a new one.

Every read reports the effective trust: `signed`, `approved` or
`unverified`.

**The policy.** `ccd serve --skills-require-trust`
(`CC_SKILLS_REQUIRE_TRUST=true`) makes installing or overriding with an
untrusted version a refusal (`409 untrusted_skill`), and `context.bootstrap`
leaves out an untrusted version already bound, with a note in
`budget.truncated`. Off by default: an installation that has not set up
signing should not lose its skills on upgrade.

## Consequences

- An operator can require that every instruction handed to an agent was
  signed by a known key or read by a responsible person.
- Agents see the trust of every skill they load and can weigh it.
- Not done: signing keys bound to an organization rather than the whole
  installation, and transparency-log style publication of signatures.
