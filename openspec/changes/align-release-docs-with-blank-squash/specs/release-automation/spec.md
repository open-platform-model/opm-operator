## ADDED Requirements

### Requirement: Forced version via the release-as config key
A forced version SHALL be a `release-as` value on package `"."` of `release-please-config.json`, set by a normal pull request and removed by the next pull request once the GitHub Release for that version is cut. While the value stays, release-please proposes exactly that version on every Release PR, so a value left in place after its release re-proposes an already-published version. The key is the required mechanism to cross a boundary the automated bump cannot reach (changing the prerelease label of a suffixed version, for example alpha to beta) and MAY pin an exact version at any crossing. A `Release-As:` commit footer SHALL NOT be relied on: the squash commit on `main` carries only the PR title (`squash_merge_commit_message: BLANK`, workspace RELEASING.md, section "Owner settings"), so a footer in the PR body or a branch commit never reaches `main`. GA needs no forced version: `prerelease: false` plus a visible carrier commit is enough. A `release-as` value sets the proposed version but does not by itself open a Release PR; one opens only once a commit of a visible type has landed since the last release.

#### Scenario: Forced version set by a normal PR
- **WHEN** a pull request that sets `"release-as": "1.0.0-beta.1"` on package `"."` merges while the manifest reads `1.0.0-alpha.22`, and a commit of a visible type has landed since the last release
- **THEN** the Release PR proposes `1.0.0-beta.1` regardless of the commit types since the last release

#### Scenario: The next PR removes the value once the release is cut
- **WHEN** the GitHub Release `v1.0.0-beta.1` has been cut while `release-please-config.json` still sets `"release-as": "1.0.0-beta.1"`
- **THEN** the next pull request removes the key, and after it merges the next releasable commit proposes `1.0.0-beta.2` per "Version bump determination per release line"

#### Scenario: Config carries release-as only while a forced release is pending
- **WHEN** `release-please-config.json` is inspected on `main` and the version its `release-as` would force is already a release tag, or no forced version is pending
- **THEN** it SHALL contain no `release-as` key at the root or in any package

#### Scenario: A footer forces nothing
- **WHEN** a pull request whose body or branch commits carry `Release-As: 1.1.0-beta.1` is squash-merged, and `release-please-config.json` carries no `release-as`
- **THEN** the squash commit on `main` is the PR title alone, and the next Release PR proposes the automated bump, not `1.1.0-beta.1`

#### Scenario: A hidden-type carrier waits for a visible commit
- **WHEN** the only commit since the last release is the `ci:` pull request that set `release-as`
- **THEN** release-please opens no Release PR; the forced version is proposed once a commit of a visible type lands

## MODIFIED Requirements

### Requirement: Version bump determination per release line
The workflow SHALL determine the proposed version from the commits since the last release tag using Conventional Commits v1 semantics, applied per release line. On the prerelease line (`versioning: prerelease`, `prerelease: true`, current version `X.0.0-beta.N`), every releasable commit, breaking or not, SHALL propose `X.0.0-beta.(N+1)`: the bump never changes the label and never moves the major. On a stable line (after GA, no prerelease suffix), `feat` SHALL propose a MINOR bump, a breaking change a MAJOR bump, and any other releasable commit (`fix`, `perf`, `deps`, `refactor`, `revert`) a PATCH bump. A breaking change is a `!` after the type or scope in the commit subject, which for a squash-merged PR is the PR title, the only text the squash commit carries (`squash_merge_commit_message: BLANK`). A `docs` commit is not releasable and proposes no bump.

#### Scenario: fix commit on the beta line advances the counter
- **WHEN** the current version is `1.0.0-beta.1` and only `fix` commits are releasable
- **THEN** the proposed version SHALL be `1.0.0-beta.2`

#### Scenario: feat commit on the beta line advances the counter
- **WHEN** the current version is `1.0.0-beta.2` and the releasable commits include a `feat`
- **THEN** the proposed version SHALL be `1.0.0-beta.3`, not `1.1.0-beta.1`

#### Scenario: Breaking-change commit on the beta line advances the counter
- **WHEN** the current version is `1.0.0-beta.3` and the releasable commits include a squash commit whose subject (the PR title) is `feat(api)!: ...`
- **THEN** the proposed version SHALL be `1.0.0-beta.4`, never `2.0.0` or `2.0.0-beta.1`

#### Scenario: feat commit on a stable line triggers MINOR bump
- **WHEN** the current version is `1.2.3` and the releasable commits include a `feat`
- **THEN** the proposed version SHALL be `1.3.0`

#### Scenario: fix-only commits on a stable line trigger PATCH bump
- **WHEN** the current version is `1.2.3` and the releasable commits include `fix` or `perf` but no `feat`
- **THEN** the proposed version SHALL be `1.2.4`

#### Scenario: Breaking-change commit on a stable line triggers MAJOR bump
- **WHEN** the current version is `1.2.3` and the releasable commits include a squash commit whose subject (the PR title) carries `!`, such as `feat!:` or `fix(deps)!:`
- **THEN** the proposed version SHALL be `2.0.0`

### Requirement: Beta prerelease line
The release-please package SHALL be configured with `versioning: prerelease`, `prerelease: true` and `prerelease-type: beta` while the operator is on its beta line. From its first beta, a prerelease line (opmodel.dev/core@v2, library, cli, opm-operator) is on the path to GA. A breaking change is still allowed during beta, but only as a `!` in the PR title (`feat!:`), which becomes the CHANGELOG entry; its migration note goes in the PR body, which the entry links and which never reaches `main`. It advances the `-beta.N` counter and never moves the module path to a new major. Stable lines (opmodel.dev/catalogs/opm@v4 and the module fleets) keep the normal SemVer rule: a break is a new major. A core beta break that would force a catalogs/opm major needs owner sign-off. An operator `feat!` that the released cli cannot drive SHALL merge only after the cli release that can drive it, and no `release-as` value SHALL hop the operator to a new minor or major (such as `1.1.0-beta.1`) during beta. Every beta GitHub Release SHALL be flagged Pre-release. Changing `prerelease-type` alone SHALL NOT be relied on to change the label of a version that already carries a suffix; a label change needs a `release-as` value (see "Forced version via the release-as config key"). GA drops the suffix: `prerelease: false` plus a visible carrier commit per package, in dependency order.

#### Scenario: Beta releases are flagged Pre-release
- **WHEN** the Release PR for `1.0.0-beta.N` is merged
- **THEN** release-please creates tag `v1.0.0-beta.N` and a GitHub Release marked Pre-release, and the image and `install.yaml` asset are published under that tag

#### Scenario: Breaking change during beta carries its migration note
- **WHEN** a pull request titled `feat(api)!: ...`, whose body describes the migration, is squash-merged on the beta line
- **THEN** the next Release PR proposes the next `-beta.N`, its CHANGELOG section lists the PR title as a breaking-change entry linking the pull request, and the migration note is read from that pull request

#### Scenario: Operator break waits for a cli that can drive it
- **WHEN** an operator `feat!` changes behavior the newest released cli cannot drive
- **THEN** it merges only after a cli release that can drive it, ships as the next `1.0.0-beta.N`, and no `release-as` value moves the operator to `1.1.0-beta.1` or any other minor or major

#### Scenario: Label flip alone keeps the old label
- **WHEN** only `prerelease-type` changes from `alpha` to `beta` while the manifest reads `1.0.0-alpha.22` and `release-please-config.json` carries no `release-as`
- **THEN** the next Release PR proposes `1.0.0-alpha.23`, which is why the crossing sets `"release-as": "1.0.0-beta.1"` in `release-please-config.json`

#### Scenario: GA drops the suffix
- **WHEN** the config sets `prerelease: false` and a visible carrier commit lands on `main` while the manifest reads `1.0.0-beta.N`
- **THEN** the Release PR proposes `1.0.0` and the GitHub Release is not marked Pre-release

### Requirement: Release PR opens for releasable commits on push to main
The release-please workflow SHALL run on every push to the `main` branch. It SHALL open a Release PR if releasable commits exist since the last release tag. Releasable commits are those whose type has a visible changelog section in `release-please-config.json` (`feat`, `fix`, `perf`, `revert`, `deps`, `refactor`). `docs`, `chore`, `test`, `ci` and `build` are hidden and SHALL NOT by themselves open a Release PR, and a `release-as` value in `release-please-config.json` does not make them releasable. If a Release PR already exists, it SHALL update the PR with the latest accumulated changes. Hiding `docs` keeps a doc-only commit from cutting an operator release that would cascade a pointless bump into the cli (owner decision 2026-10-01 (RELEASING.md, Pin classes)).

#### Scenario: First feat commit after a release
- **WHEN** a `feat(scope): description` commit is pushed to `main` and no open Release PR exists
- **THEN** release-please opens a new Release PR proposing the next version per "Version bump determination per release line", with an updated CHANGELOG.md

#### Scenario: Subsequent fix commit with open Release PR
- **WHEN** a `fix(scope): description` commit is pushed to `main` and a Release PR already exists
- **THEN** release-please updates the existing Release PR to include the new fix in the changelog, adjusting the proposed version if needed

#### Scenario: Only non-releasable commits
- **WHEN** only `docs`, `chore`, `test`, `ci` or `build` commits are pushed since the last release, whether or not `release-please-config.json` carries a `release-as` value
- **THEN** release-please SHALL NOT open a Release PR (no version bump warranted)

#### Scenario: Docs-only commits do not cut a release
- **WHEN** only `docs` commits are pushed since the last release
- **THEN** release-please SHALL NOT open a Release PR, and `release-please-config.json` SHALL declare the `docs` section with `"hidden": true`

#### Scenario: Refactor commits still cut a release
- **WHEN** only `refactor` commits are pushed since the last release
- **THEN** release-please opens a Release PR listing them under Code Refactoring

## REMOVED Requirements

### Requirement: Manual version override via release-as
**Reason**: It made a one-shot `Release-As:` footer in the squash commit the only forced-version mechanism and forbade the `release-as` key in `release-please-config.json`. Under the `BLANK` squash message (owner decision 2026-10-02, workspace RELEASING.md, section "Owner settings") the squash commit carries only the PR title, so the footer never reaches `main` and the forbidden key is the only mechanism left.
**Migration**: Use "Forced version via the release-as config key": set `release-as` on package `"."` of `release-please-config.json` in a normal PR, and remove it in the next PR once that release is cut.
