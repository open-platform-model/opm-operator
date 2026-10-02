## ADDED Requirements

### Requirement: Release PR opens for releasable commits on push to main
The release-please workflow SHALL run on every push to the `main` branch. It SHALL open a Release PR if releasable commits exist since the last release tag. Releasable commits are those whose type has a visible changelog section in `release-please-config.json` (`feat`, `fix`, `perf`, `revert`, `deps`, `refactor`) and any commit carrying a `Release-As:` footer. `docs`, `chore`, `test`, `ci` and `build` are hidden and SHALL NOT by themselves open a Release PR. If a Release PR already exists, it SHALL update the PR with the latest accumulated changes. Hiding `docs` keeps a doc-only commit from cutting an operator release that would cascade a pointless bump into the cli (owner decision 2026-10-01 (RELEASING.md, Pin classes)).

#### Scenario: First feat commit after a release
- **WHEN** a `feat(scope): description` commit is pushed to `main` and no open Release PR exists
- **THEN** release-please opens a new Release PR proposing the next version per "Version bump determination per release line", with an updated CHANGELOG.md

#### Scenario: Subsequent fix commit with open Release PR
- **WHEN** a `fix(scope): description` commit is pushed to `main` and a Release PR already exists
- **THEN** release-please updates the existing Release PR to include the new fix in the changelog, adjusting the proposed version if needed

#### Scenario: Only non-releasable commits
- **WHEN** only `docs`, `chore`, `test`, `ci` or `build` commits, none carrying a `Release-As:` footer, are pushed since the last release
- **THEN** release-please SHALL NOT open a Release PR (no version bump warranted)

#### Scenario: Docs-only commits do not cut a release
- **WHEN** only `docs` commits are pushed since the last release
- **THEN** release-please SHALL NOT open a Release PR, and `release-please-config.json` SHALL declare the `docs` section with `"hidden": true`

#### Scenario: Refactor commits still cut a release
- **WHEN** only `refactor` commits are pushed since the last release
- **THEN** release-please opens a Release PR listing them under Code Refactoring

### Requirement: Release PR passes the release-pin gate
On every Release PR, the release-pin gate (G1, workspace RELEASING.md, section "Gates") SHALL run as a step inside an existing CI job that runs on every pull request, never as a job of its own, so a skipped job can never report a pass. A PR is a Release PR when `${{ github.head_ref || github.ref_name }}` starts with `release-please--`. The gate SHALL fail the job when any of the following holds on the PR head:
- `go.mod` carries a `replace` directive;
- a required module under `github.com/open-platform-model/` is pinned to a Go pseudo-version, or to a version that is not an existing tag of that module's repository;
- a published fixture's `cue.mod/module.cue` (`test/fixtures/modules/*`, `test/fixtures/modulepackages/*`) pins a dependency version containing `-0.dev.`;
- any `cue.mod/local-module.cue` is tracked by git.

Each failure SHALL name the offending file and pin. On pull requests that are not Release PRs the gate step SHALL be skipped. The same check SHALL be runnable locally as one task. The job carrying the gate SHALL report a check name no other workflow job in the repo uses (`Lint`), so a ruleset can require exactly that check.

#### Scenario: Replace directive blocks the release
- **WHEN** a Release PR head has `replace github.com/open-platform-model/library => ../library` in `go.mod`
- **THEN** the gate step fails and its output names the replace directive

#### Scenario: Pseudo-version blocks the release
- **WHEN** a Release PR head pins `github.com/open-platform-model/library v1.0.0-beta.2.0.20261001120000-abcdef123456`
- **THEN** the gate step fails and names the module and version

#### Scenario: Untagged version blocks the release
- **WHEN** a Release PR head pins `github.com/open-platform-model/library v1.0.0-beta.9` and the library repository has no tag `v1.0.0-beta.9`
- **THEN** the gate step fails and names the module and version

#### Scenario: Dev CUE pin in a published fixture blocks the release
- **WHEN** a Release PR head has `test/fixtures/modules/hello/cue.mod/module.cue` pinning `v: "v2.0.0-0.dev.20261001"`
- **THEN** the gate step fails and names the file

#### Scenario: Tracked local-module.cue blocks the release
- **WHEN** a Release PR head tracks `test/fixtures/modules/hello/cue.mod/local-module.cue`
- **THEN** the gate step fails and names the file

#### Scenario: Clean release PR passes
- **WHEN** a Release PR head has no replace directive, pins `github.com/open-platform-model/library` to an existing tag, and no published fixture carries a dev pin or a tracked `local-module.cue`
- **THEN** the gate step passes

#### Scenario: The gate's check name is unique
- **WHEN** the `name:` of every job under `.github/workflows/` is listed
- **THEN** the job carrying the release-pin gate reports `Lint`, and no other job reports that name

#### Scenario: Ordinary PRs skip the gate
- **WHEN** a pull request's head branch does not start with `release-please--`
- **THEN** the gate step is skipped and the job's result depends only on its other steps

### Requirement: Dependabot leaves OPM Go modules to the release cascade
The `gomod` entry of `.github/dependabot.yml` SHALL ignore every dependency matching `github.com/open-platform-model/*`. Those pins move only through the release cascade, which titles a shipped bump `fix(deps)` so it releases (workspace RELEASING.md, section "Pin classes"). Third-party Go modules and GitHub Actions SHALL keep their Dependabot updates.

#### Scenario: Library release opens no Dependabot PR
- **WHEN** library publishes a new tag
- **THEN** Dependabot opens no PR bumping `github.com/open-platform-model/library` in opm-operator

#### Scenario: Third-party bumps continue
- **WHEN** a new `k8s.io/api` release exists
- **THEN** Dependabot still proposes the grouped Kubernetes bump

### Requirement: The opm CLI version is pinned in one file
The opm CLI release that CI and the release workflow install SHALL be declared once, in a repo-root file `.opm-cli-version` holding exactly one line: the cli tag (for example `v1.0.0-beta.4`). Every workflow step that installs the opm CLI SHALL read the version from that file and SHALL fail before installing when the file is missing or its content is not a `v`-prefixed SemVer tag. No workflow file SHALL carry an opm CLI version literal. The pin is a release-tool pin: moving it is a `ci(deps)` commit and cuts no release (workspace RELEASING.md, section "Pin classes").

#### Scenario: Every install reads the file
- **WHEN** the workflows under `.github/workflows/` are searched for `cli/cmd/opm@v`
- **THEN** there is no match, and each `go install` of `github.com/open-platform-model/cli/cmd/opm` takes its version from `.opm-cli-version`

#### Scenario: Malformed pin fails fast
- **WHEN** `.opm-cli-version` contains `latest` or is empty
- **THEN** the install step fails with a message naming `.opm-cli-version`, before any `go install` runs

#### Scenario: Release job installs the pin of the release tag
- **WHEN** the release workflow publishes example modules for tag `vX`
- **THEN** it installs the opm CLI version recorded in `.opm-cli-version` at tag `vX`

## MODIFIED Requirements

### Requirement: Changelog generation
The workflow SHALL generate and maintain a `CHANGELOG.md` file at the repository root. Entries SHALL be grouped under the visible sections that `release-please-config.json` declares (Features, Bug Fixes, Performance Improvements, Reverts, Dependencies, Code Refactoring). Commits of a hidden type (`docs`, `chore`, `test`, `ci`, `build`) SHALL NOT appear. Documentation entries already in CHANGELOG.md from earlier releases SHALL stay as they are.

#### Scenario: Changelog includes all commit types
- **WHEN** the Release PR is created or updated
- **THEN** `CHANGELOG.md` SHALL list every commit since the last release whose type has a visible section in `release-please-config.json`, grouped under that section, with commit messages as entries, and SHALL list no commit of a hidden type

#### Scenario: Changelog preserves history
- **WHEN** a new release is cut
- **THEN** the new changelog section SHALL be prepended to existing content, preserving prior release entries

### Requirement: Version bump determination per release line
The workflow SHALL determine the proposed version from the commits since the last release tag using Conventional Commits v1 semantics, applied per release line. On the prerelease line (`versioning: prerelease`, `prerelease: true`, current version `X.0.0-beta.N`), every releasable commit, breaking or not, SHALL propose `X.0.0-beta.(N+1)`: the bump never changes the label and never moves the major. On a stable line (after GA, no prerelease suffix), `feat` SHALL propose a MINOR bump, a breaking change a MAJOR bump, and any other releasable commit (`fix`, `perf`, `deps`, `refactor`, `revert`) a PATCH bump. A `docs` commit is not releasable and proposes no bump.

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


## REMOVED Requirements

### Requirement: Release PR creation on push to main
**Reason**: Its scenario "Docs-only commits cut a release" asserts the opposite of the new rule; `docs` is now a hidden section so a doc-only commit no longer releases and cascades into the cli.
**Migration**: Replaced by "Release PR opens for releasable commits on push to main", which keeps the first two scenarios unchanged, adds `docs` to "Only non-releasable commits", drops `docs` from the releasable types, and adds "Docs-only commits do not cut a release" and "Refactor commits still cut a release".
