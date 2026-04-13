## ADDED Requirements

### Requirement: Release PR creation on push to main
The release-please workflow SHALL run on every push to the `main` branch. It SHALL open a Release PR if releasable commits (`feat`, `fix`, `perf`) exist since the last release tag. If a Release PR already exists, it SHALL update the PR with the latest accumulated changes.

#### Scenario: First feat commit after a release
- **WHEN** a `feat(scope): description` commit is pushed to `main` and no open Release PR exists
- **THEN** release-please opens a new Release PR proposing a MINOR version bump with an updated CHANGELOG.md

#### Scenario: Subsequent fix commit with open Release PR
- **WHEN** a `fix(scope): description` commit is pushed to `main` and a Release PR already exists
- **THEN** release-please updates the existing Release PR to include the new fix in the changelog, adjusting the proposed version if needed

#### Scenario: Only non-releasable commits
- **WHEN** only `chore`, `docs`, `test`, `ci`, or `refactor` commits are pushed since the last release
- **THEN** release-please SHALL NOT open a Release PR (no version bump warranted)

### Requirement: Version bump determination from Conventional Commits
The workflow SHALL determine the version bump level by analyzing commit messages since the last release tag using Conventional Commits v1 semantics.

#### Scenario: feat commit triggers MINOR bump
- **WHEN** commits since last release include at least one `feat` type
- **THEN** the proposed version SHALL be a MINOR bump (e.g., 0.1.0 → 0.2.0)

#### Scenario: fix-only commits trigger PATCH bump
- **WHEN** commits since last release include `fix` or `perf` types but no `feat`
- **THEN** the proposed version SHALL be a PATCH bump (e.g., 0.1.0 → 0.1.1)

#### Scenario: MAJOR bump is never automated
- **WHEN** commits include `feat!:` prefix or `BREAKING CHANGE:` footer
- **THEN** the workflow SHALL treat them as MINOR bumps (not MAJOR), because the team policy prohibits automated MAJOR bumps

### Requirement: Manual MAJOR version override
The workflow SHALL support explicit version overrides for MAJOR bumps via the `release-as` configuration option.

#### Scenario: Manual major bump via release-as
- **WHEN** a maintainer sets `release-as: 1.0.0` in the release-please config and pushes to `main`
- **THEN** the Release PR SHALL propose version 1.0.0 regardless of commit types

### Requirement: Changelog generation
The workflow SHALL generate and maintain a `CHANGELOG.md` file at the repository root. Entries SHALL be grouped by commit type (Features, Bug Fixes, Performance Improvements, Miscellaneous Chores).

#### Scenario: Changelog includes all commit types
- **WHEN** the Release PR is created or updated
- **THEN** `CHANGELOG.md` SHALL list all commits since the last release, grouped by type, with commit messages as entries

#### Scenario: Changelog preserves history
- **WHEN** a new release is cut
- **THEN** the new changelog section SHALL be prepended to existing content, preserving prior release entries

### Requirement: Git tag and GitHub Release on merge
When the Release PR is merged to `main`, release-please SHALL create a git tag (e.g., `v0.2.0`) and a GitHub Release with the changelog section as release notes.

#### Scenario: Release PR merged
- **WHEN** the Release PR is merged to `main`
- **THEN** release-please creates a git tag matching the version (prefixed with `v`) and a GitHub Release with the changelog for that version as the body

#### Scenario: Release PR closed without merge
- **WHEN** the Release PR is closed without merging
- **THEN** no tag or release SHALL be created; the next push to `main` re-opens or creates a new Release PR

### Requirement: Initial version baseline
The release-please manifest SHALL set the initial version to `0.1.0`, reflecting the pre-v1 maturity of the controller.

#### Scenario: First release from empty history
- **WHEN** no prior release tags exist and releasable commits are present
- **THEN** the first Release PR SHALL propose version `0.1.0` (or the next increment from it based on commit types)
