# 0043. An update notice, off unless the operator turns it on

## Status

Accepted, 2026-11-18. Closes "no in-product notice that a newer version
exists" in `docs/cra/essential-requirements.md` (Annex I (2)(c)).

## Context

Security fixes reach an installation only when its operator upgrades, and
the operator learns about a release from GitHub or not at all. Annex I
(2)(c) of the CRA asks for a way to notify users of available updates. The
product's other stated rule is that it contacts nobody it was not told to
(Annex I (2)(i)).

## Decision

**`ccd serve --update-check`** (`CC_UPDATE_CHECK=true`), off by default,
asks GitHub's releases API a minute after start and then about once a day,
spread by up to an hour. It uses the same resolution as `ccd self-update`
(`internal/selfupdate`): a pre-release build follows pre-releases, a stable
build only stable releases. It sends nothing about the installation; the
request is the same one anyone makes to read the release list.

**`GET /v1/installation/version`** reports the running version and, with
the check on, the newest release, whether it is newer, and when it was
checked. The interface shows installation administrators a notice with a
link to the release notes; nobody else's browser asks. A newer release is
also written to the server log.

**Off by default, on purpose.** An installation that says nothing contacts
nobody. The packaged `ccd.env` template and `docs/install.md` offer the
switch.

## Consequences

- An operator who turns it on hears about a release, security fixes
  included, within a day, in the interface and the log.
- The notice does not upgrade anything: the package manager, the image or
  `ccd self-update` does.
