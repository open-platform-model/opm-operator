## MODIFIED Requirements

### Requirement: Changelog generation
The workflow SHALL generate and maintain a `CHANGELOG.md` file at the repository root for the operator package. Entries SHALL be grouped under the visible sections that `release-please-config.json` declares (Features, Bug Fixes, Performance Improvements, Reverts, Dependencies, Code Refactoring). Commits of a hidden type (`docs`, `chore`, `test`, `ci`, `build`) SHALL NOT appear. A commit that changes only files under the operator module's directory `modules/opm_operator/` belongs to the module's own changelog (see `operator-module-release`) and SHALL NOT appear in the root `CHANGELOG.md`. Documentation entries already in CHANGELOG.md from earlier releases SHALL stay as they are.

#### Scenario: Changelog includes all commit types
- **WHEN** the Release PR is created or updated
- **THEN** `CHANGELOG.md` SHALL list every commit since the last release whose type has a visible section in `release-please-config.json` and that changes a file outside `modules/opm_operator/`, grouped under that section, with commit messages as entries, and SHALL list no commit of a hidden type

#### Scenario: Changelog preserves history
- **WHEN** a new release is cut
- **THEN** the new changelog section SHALL be prepended to existing content, preserving prior release entries

#### Scenario: Module-only commit stays out of the operator changelog
- **WHEN** `fix(deps): deploy operator v1.0.0-beta.6 from the operator module`, which changes only `modules/opm_operator/`, is merged
- **THEN** the operator's Release PR and root `CHANGELOG.md` do not list it

### Requirement: Git tag and GitHub Release on merge
When a Release PR is merged to `main`, release-please SHALL create the git tag eagerly at the release commit and a GitHub Release in draft state with the changelog section as release notes. The operator package `"."` tags `vX.Y.Z` (e.g. `v0.2.0`, or `v1.0.0-beta.6` on the beta line); the operator module's package tags `opm_operator-vX.Y.Z` (see `operator-module-release`). Each release-please package SHALL be configured with `draft: true` and `force-tag-creation: true`. An operator release's draft SHALL become public only through the final publish step defined in "Release published once after every release job"; a module release's draft only through the module's final publish job.

#### Scenario: Release PR merged
- **WHEN** the Release PR is merged to `main`
- **THEN** release-please creates a git tag matching the version (prefixed with `v` for the operator, with `opm_operator-v` for the module) at the release commit and a draft GitHub Release with the changelog for that version as the body, and the release is not visible as published until every release job has succeeded

#### Scenario: Release PR closed without merge
- **WHEN** the Release PR is closed without merging
- **THEN** no tag or release SHALL be created; the next push to `main` re-opens or creates a new Release PR

#### Scenario: Config enables draft-first
- **WHEN** `release-please-config.json` is inspected on `main`
- **THEN** package `"."` and package `"modules/opm_operator"` SHALL each set `"draft": true` and `"force-tag-creation": true`

### Requirement: Release assets upload only to a draft release
Every step that uploads an asset to the GitHub Release (`install.yaml` of an operator or a module release, `opm-examples.tar.gz`, the example manifests) SHALL first confirm that the single release for the tag is a draft and SHALL fail without uploading when it is published. Replacing an existing asset (`--clobber`) SHALL be used only on a draft. Every GitHub CLI call in the release workflow SHALL name the repository explicitly. Source: 0021:D10:R8.

#### Scenario: Upload to the draft
- **WHEN** the image job uploads `install.yaml` for a release that is still a draft
- **THEN** the upload succeeds, and re-running the job replaces the asset on the draft

#### Scenario: Module manifest upload to the draft
- **WHEN** the module publish job uploads `install.yaml` for `opm_operator-v0.2.0` while its release is a draft
- **THEN** the upload succeeds, and re-running the job replaces the asset on the draft

#### Scenario: Upload to a published release refused
- **WHEN** an upload step runs for a tag whose release is already published
- **THEN** the step fails before uploading and tells the operator to release the next version instead

### Requirement: Release published once after every release job
The release workflow SHALL contain a final job that depends on every job producing a release artifact (the image job and the example publishing job), runs only when the operator package `"."` created a release (a module-only release does not run it), holds `contents: write` and no other write permission, checks out only the files it runs, confirms the required assets (`install.yaml`, `opm-examples.tar.gz`) are attached to the draft, and then publishes the draft. It SHALL leave the Pre-release flag set by release-please unchanged. When the release is already published it SHALL succeed without changing anything.

#### Scenario: All release jobs succeed
- **WHEN** the image and example jobs finish successfully for `v1.0.0-beta.3`
- **THEN** the final job publishes the draft, and the GitHub Release is public, flagged Pre-release and carries every asset

#### Scenario: A release job fails
- **WHEN** the example publishing job fails for `v1.0.0-beta.3`
- **THEN** the final job does not run, the release stays a draft, and "Re-run failed jobs" on the same workflow run completes and publishes it

#### Scenario: Required asset missing
- **WHEN** the final job runs and the draft lacks `install.yaml`
- **THEN** the job fails and the release stays a draft

#### Scenario: Module-only release
- **WHEN** a push to `main` creates only a module release
- **THEN** the operator's final job does not run

### Requirement: Beta prerelease line
The release-please package `"."` SHALL be configured with `versioning: prerelease`, `prerelease: true` and `prerelease-type: beta` while the operator is on its beta line. From its first beta, a prerelease line (opmodel.dev/core@v2, library, cli, opm-operator) is on the path to GA. A breaking change is still allowed during beta, but only as a `!` in the PR title (`feat!:`), which becomes the CHANGELOG entry; its migration note goes in the PR body, which the entry links and which never reaches `main`. It advances the `-beta.N` counter and never moves the module path to a new major. Stable lines (opmodel.dev/catalogs/opm@v4 and the module fleets) keep the normal SemVer rule: a break is a new major. The operator module's own package follows its 0.x rule instead (`operator-module-release`). A core beta break that would force a catalogs/opm major needs owner sign-off. An operator `feat!` that the released cli cannot drive SHALL merge only after the cli release that can drive it, and no `release-as` value SHALL hop the operator to a new minor or major (such as `1.1.0-beta.1`) during beta. Every beta GitHub Release SHALL be flagged Pre-release. Changing `prerelease-type` alone SHALL NOT be relied on to change the label of a version that already carries a suffix; a label change needs a `release-as` value (see "Forced version via the release-as config key"). GA drops the suffix: `prerelease: false` plus a visible carrier commit per package, in dependency order.

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
