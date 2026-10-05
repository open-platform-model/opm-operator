## Why

`.github/CODEOWNERS` names two code owners on every line. Release-please and Dependabot PRs touch
owned paths, so each one requests a review from both maintainers. The owner wants to be the only
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
