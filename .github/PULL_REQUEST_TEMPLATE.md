<!-- Thanks for contributing! Please fill this in and tick the checklist. -->

## Summary

<!-- What does this PR do and why? Link any related issue (e.g. "Closes #123"). -->

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Docs only
- [ ] Refactor / chore

## Checklist

- [ ] `make test` (`go test ./... -race`, needs Docker) passes
- [ ] `make lint` (`golangci-lint`) is clean
- [ ] New project-scoped table: same RLS pattern (`cc_has_project_access`), scoped `cc_app` grants,
      at least one negative cross-project test run *as `cc_app`*
- [ ] New domain service method touching project data: calls `AuthzService.Can()` itself
- [ ] Added/updated tests for the change — and confirmed they fail without the fix
- [ ] Updated `docs/architecture-cookbook.md` or added an ADR, if this changes a domain invariant

## Notes for reviewers

<!-- Anything worth calling out: trade-offs, follow-ups, screenshots if relevant. -->
