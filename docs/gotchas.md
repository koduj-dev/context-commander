# Gotchas worth remembering

Things that cost real debugging time here, kept so they cost it once. This is
engineering knowledge about *this* codebase — not a style guide, and not a list
of everything that could go wrong. If something bit you and the cause was
non-obvious, it belongs here.

Nothing is filed yet — most of the sharp edges found so far (the RLS bootstrap
gap for project creation, the `SET LOCAL`-vs-pooled-connection principal
context, the `''::uuid` cast on an untouched GUC) turned out to be design
decisions worth their own rationale, so they live as ADRs instead:
[`docs/adr/0002`](adr/0002-non-recursive-rls.md),
[`docs/adr/0003`](adr/0003-set-local-transaction-context.md),
[`docs/adr/0015`](adr/0015-project-creation-rls-bootstrap.md). Use this file
for the smaller stuff that doesn't rise to an ADR — a tooling surprise, a test
fixture trap, a footgun in a dependency — the way
[docker-commander's gotchas.md](https://github.com/koduj-dev/docker-commander/blob/main/docs/gotchas.md)
does.
