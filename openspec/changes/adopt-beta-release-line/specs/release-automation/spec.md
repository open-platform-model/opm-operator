## MODIFIED Requirements

### Requirement: Release PR creation on push to main
The release-please workflow SHALL run on every push to the `main` branch. It SHALL open a Release PR if releasable commits exist since the last release tag. Releasable commits are those whose type has a visible changelog section in `release-please-config.json` (`feat`, `fix`, `perf`, `revert`, `deps`, `docs`, `refactor`) and any commit carrying a `Release-As:` footer. `chore`, `test`, `ci` and `build` are hidden and SHALL NOT by themselves open a Release PR. If a Release PR already exists, it SHALL update the PR with the latest accumulated changes.

#### Scenario: First feat commit after a release
- **WHEN** a `feat(scope): description` commit is pushed to `main` and no open Release PR exists
- **THEN** release-please opens a new Release PR proposing the next version per "Version bump determination per release line", with an updated CHANGELOG.md

#### Scenario: Subsequent fix commit with open Release PR
- **WHEN** a `fix(scope): description` commit is pushed to `main` and a Release PR already exists
- **THEN** release-please updates the existing Release PR to include the new fix in the changelog, adjusting the proposed version if needed

#### Scenario: Only non-releasable commits
- **WHEN** only `chore`, `test`, `ci` or `build` commits, none carrying a `Release-As:` footer, are pushed since the last release
- **THEN** release-please SHALL NOT open a Release PR (no version bump warranted)

#### Scenario: Docs-only commits cut a release
- **WHEN** only `docs` commits are pushed since the last release
- **THEN** release-please opens a Release PR listing them under Documentation, because the Documentation section is visible

### Requirement: Manual version override via release-as
The workflow SHALL support explicit version overrides only through a one-shot `Release-As: <version>` footer in the final footer block of a commit message on `main`, which for a squash-merged PR is the squash commit message. The `release-as` key SHALL NOT appear in `release-please-config.json` at any level: it is re-applied on every run until removed and re-proposes an already-published version. The footer is the required mechanism to cross a boundary the automated bump cannot reach (historically 0.x to 1.0.0, now changing the prerelease label of a suffixed version from alpha to beta) and MAY pin an exact version at any crossing or override any automated bump decision. GA needs no footer: `prerelease: false` plus a visible carrier commit is enough. It is self-clearing: once the release tag exists, the carrying commit falls out of the commit window and later releases follow the automated bump. When several commits since the last release carry the footer, the newest one wins.

#### Scenario: Manual 1.0.0 cut via release-as
- **WHEN** a commit whose final footer block carries `Release-As: 1.0.0` is pushed to `main`
- **THEN** the Release PR SHALL propose version 1.0.0 regardless of commit types

#### Scenario: Beta line entered via footer
- **WHEN** the manifest reads `1.0.0-alpha.22`, `prerelease-type` is `beta`, and a squash commit carrying `Release-As: 1.0.0-beta.1` lands on `main`
- **THEN** the open Release PR is recomputed to `chore(main): release 1.0.0-beta.1`, and after it merges the next releasable commit proposes `1.0.0-beta.2` with no further config change

#### Scenario: Config carries no release-as
- **WHEN** `release-please-config.json` is inspected on `main`
- **THEN** it SHALL contain no `release-as` key at the root or in any package

### Requirement: Changelog generation
The workflow SHALL generate and maintain a `CHANGELOG.md` file at the repository root. Entries SHALL be grouped under the visible sections that `release-please-config.json` declares (Features, Bug Fixes, Performance Improvements, Reverts, Dependencies, Documentation, Code Refactoring). Commits of a hidden type (`chore`, `test`, `ci`, `build`) SHALL NOT appear.

#### Scenario: Changelog includes all commit types
- **WHEN** the Release PR is created or updated
- **THEN** `CHANGELOG.md` SHALL list every commit since the last release whose type has a visible section in `release-please-config.json`, grouped under that section, with commit messages as entries, and SHALL list no commit of a hidden type

#### Scenario: Changelog preserves history
- **WHEN** a new release is cut
- **THEN** the new changelog section SHALL be prepended to existing content, preserving prior release entries

### Requirement: Release PR bumps the annotated version constant
The release-please configuration SHALL list `internal/version/version.go` under the root package's `extra-files`, so every Release PR rewrites the `x-release-please-version`-annotated `Version` constant to the proposed version in the same commit the release tag will point at. The constant SHALL NOT be edited by hand; only the Release PR changes it.

#### Scenario: Release PR includes the constant bump
- **WHEN** release-please opens or updates a Release PR proposing version `X.Y.Z-beta.N` (or `X.Y.Z` once the line reaches GA)
- **THEN** the PR's diff sets `internal/version/version.go`'s `Version` constant to exactly that version

## REMOVED Requirements

### Requirement: Version bump determination from Conventional Commits
**Reason**: Its pre-1.0 scenario asserts that `bump-minor-pre-major` demotes a breaking change to MINOR, which `bump-minor-pre-major: false` in the config contradicts, and the operator is past 0.x. Its MINOR, PATCH and MAJOR scenarios describe a stable line and do not hold on the prerelease line, where every bump advances the counter. MODIFIED cannot retire a scenario name, so the requirement is replaced.
**Migration**: Superseded by "Version bump determination per release line", which states the prerelease-line and stable-line behavior separately. No config or code migration.

### Requirement: Initial version baseline
**Reason**: The manifest is past 0.x (`1.0.0-alpha.22` at planning time), so a first release from empty history can no longer happen, and "pre-v1 maturity" contradicts the beta line's promise that the operator is on the path to GA. The beta line supersedes it.
**Migration**: None.

## ADDED Requirements

### Requirement: Version bump determination per release line
The workflow SHALL determine the proposed version from the commits since the last release tag using Conventional Commits v1 semantics, applied per release line. On the prerelease line (`versioning: prerelease`, `prerelease: true`, current version `X.0.0-beta.N`), every releasable commit, breaking or not, SHALL propose `X.0.0-beta.(N+1)`: the bump never changes the label and never moves the major. On a stable line (after GA, no prerelease suffix), `feat` SHALL propose a MINOR bump, a breaking change a MAJOR bump, and any other releasable commit (`fix`, `perf`, `deps`, `docs`, `refactor`, `revert`) a PATCH bump.

#### Scenario: fix commit on the beta line advances the counter
- **WHEN** the current version is `1.0.0-beta.1` and only `fix` commits are releasable
- **THEN** the proposed version SHALL be `1.0.0-beta.2`

#### Scenario: feat commit on the beta line advances the counter
- **WHEN** the current version is `1.0.0-beta.2` and the releasable commits include a `feat`
- **THEN** the proposed version SHALL be `1.0.0-beta.3`, not `1.1.0-beta.1`

#### Scenario: Breaking-change commit on the beta line advances the counter
- **WHEN** the current version is `1.0.0-beta.3` and the releasable commits include a `feat!:` commit carrying a `BREAKING CHANGE:` footer
- **THEN** the proposed version SHALL be `1.0.0-beta.4`, never `2.0.0` or `2.0.0-beta.1`

#### Scenario: feat commit on a stable line triggers MINOR bump
- **WHEN** the current version is `1.2.3` and the releasable commits include a `feat`
- **THEN** the proposed version SHALL be `1.3.0`

#### Scenario: fix-only commits on a stable line trigger PATCH bump
- **WHEN** the current version is `1.2.3` and the releasable commits include `fix` or `perf` but no `feat`
- **THEN** the proposed version SHALL be `1.2.4`

#### Scenario: Breaking-change commit on a stable line triggers MAJOR bump
- **WHEN** the current version is `1.2.3` and the releasable commits include a `feat!:` prefix or a `BREAKING CHANGE:` footer
- **THEN** the proposed version SHALL be `2.0.0`

### Requirement: Beta prerelease line
The release-please package SHALL be configured with `versioning: prerelease`, `prerelease: true` and `prerelease-type: beta` while the operator is on its beta line. From its first beta the operator is on the path to GA: a breaking change is still allowed during beta, but only as a `feat!` commit whose `BREAKING CHANGE:` footer is the migration note the CHANGELOG shows; it advances the `-beta.N` counter and never moves the version to a new major. Every beta GitHub Release SHALL be flagged Pre-release. Changing `prerelease-type` alone SHALL NOT be relied on to change the label of a version that already carries a suffix; a label change needs a `Release-As:` footer. GA drops the suffix: `prerelease: false` plus a visible carrier commit, landed in dependency order after the upstream prerelease lines.

#### Scenario: Beta releases are flagged Pre-release
- **WHEN** the Release PR for `1.0.0-beta.N` is merged
- **THEN** release-please creates tag `v1.0.0-beta.N` and a GitHub Release marked Pre-release, and the image and `install.yaml` asset are published under that tag

#### Scenario: Breaking change during beta carries its migration note
- **WHEN** a `feat!:` commit whose `BREAKING CHANGE:` footer describes the migration merges on the beta line
- **THEN** the next Release PR proposes the next `-beta.N` and its CHANGELOG section shows the footer text as a breaking-change entry

#### Scenario: Label flip alone keeps the old label
- **WHEN** only `prerelease-type` changes from `alpha` to `beta` while the manifest reads `1.0.0-alpha.22` and no commit carries a `Release-As:` footer
- **THEN** the next Release PR proposes `1.0.0-alpha.23`, which is why the crossing commit carries `Release-As: 1.0.0-beta.1`

#### Scenario: GA drops the suffix
- **WHEN** the config sets `prerelease: false` and a visible carrier commit lands on `main` while the manifest reads `1.0.0-beta.N`
- **THEN** the Release PR proposes `1.0.0` and the GitHub Release is not marked Pre-release
