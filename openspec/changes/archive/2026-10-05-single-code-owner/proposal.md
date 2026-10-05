## Why

`.github/CODEOWNERS` names two code owners on every line. Any PR that touches an owned path
requests a review from both maintainers: human PRs 241 and 243 already show a request to the
second maintainer. Release-please and Dependabot PRs touch owned paths too, so they would do the
same once opened after CODEOWNERS landed (PR 225). The owner wants to be the only
code owner, as `.github` (PR 16), core, library, cli and catalog_opm already do.

## What Changes

- `.github/CODEOWNERS` names only the owner on every line. The paths stay the same.
- The `workflow-hardening` spec says who the code owner is.

No ruleset change: `main` requires no approvals and no code-owner review while OPM is in beta.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `workflow-hardening`: the code-owners requirement names the single owner.

## Impact

- `.github/CODEOWNERS` only. No workflow, code or ruleset changes.
