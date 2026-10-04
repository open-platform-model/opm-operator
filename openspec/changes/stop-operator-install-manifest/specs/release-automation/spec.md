## MODIFIED Requirements

### Requirement: Release assets upload only to a draft release
Every step that uploads an asset to the GitHub Release (`install.yaml` of a module release, `opm-examples.tar.gz`, the example manifests) SHALL first confirm that the single release for the tag is a draft and SHALL fail without uploading when it is published. Replacing an existing asset (`--clobber`) SHALL be used only on a draft. Every GitHub CLI call in the release workflow SHALL name the repository explicitly. Source: 0021:D10:R8.

#### Scenario: Upload to the draft
- **WHEN** the example publishing job uploads `opm-examples.tar.gz` for a release that is still a draft
- **THEN** the upload succeeds, and re-running the job replaces the asset on the draft

#### Scenario: Module manifest upload to the draft
- **WHEN** the module publish job uploads `install.yaml` for `opm_operator-v0.2.0` while its release is a draft
- **THEN** the upload succeeds, and re-running the job replaces the asset on the draft

#### Scenario: Upload to a published release refused
- **WHEN** an upload step runs for a tag whose release is already published
- **THEN** the step fails before uploading and tells the operator to release the next version instead

### Requirement: Release published once after every release job
The release workflow SHALL contain a final job that depends on every job producing a release artifact (the image job and the example publishing job), runs only when the operator package `"."` created a release (a module-only release does not run it), holds `contents: write` and no other write permission, checks out only the files it runs, confirms the required asset `opm-examples.tar.gz` is attached to the draft, and then publishes the draft. It SHALL leave the Pre-release flag set by release-please unchanged. When the release is already published it SHALL succeed without changing anything. An operator release SHALL NOT carry `install.yaml`: the install manifest is an asset of the module's release (`operator-module-release`).

#### Scenario: All release jobs succeed
- **WHEN** the image and example jobs finish successfully for `v1.0.0-beta.3`
- **THEN** the final job publishes the draft, and the GitHub Release is public, flagged Pre-release and carries every asset

#### Scenario: A release job fails
- **WHEN** the example publishing job fails for `v1.0.0-beta.3`
- **THEN** the final job does not run, the release stays a draft, and "Re-run failed jobs" on the same workflow run completes and publishes it

#### Scenario: Required asset missing
- **WHEN** the final job runs and the draft lacks `opm-examples.tar.gz`
- **THEN** the job fails and the release stays a draft

#### Scenario: Module-only release
- **WHEN** a push to `main` creates only a module release
- **THEN** the operator's final job does not run

#### Scenario: No operator manifest
- **WHEN** an operator release `v1.0.0-beta.9` is published
- **THEN** its GitHub Release carries `opm-examples.tar.gz` and no `install.yaml`

### Requirement: Beta prerelease line
The release-please package `"."` SHALL be configured with `versioning: prerelease`, `prerelease: true` and `prerelease-type: beta` while the operator is on its beta line. From its first beta, a prerelease line (opmodel.dev/core@v2, library, cli, opm-operator) is on the path to GA. A breaking change is still allowed during beta, but only as a `!` in the PR title (`feat!:`), which becomes the CHANGELOG entry; its migration note goes in the PR body, which the entry links and which never reaches `main`. It advances the `-beta.N` counter and never moves the module path to a new major. Stable lines (opmodel.dev/catalogs/opm@v4 and the module fleets) keep the normal SemVer rule: a break is a new major. The operator module's own package follows its 0.x rule instead (`operator-module-release`). A core beta break that would force a catalogs/opm major needs owner sign-off. An operator `feat!` that the released cli cannot drive SHALL merge only after the cli release that can drive it, and no `release-as` value SHALL hop the operator to a new minor or major (such as `1.1.0-beta.1`) during beta. Every beta GitHub Release SHALL be flagged Pre-release. Changing `prerelease-type` alone SHALL NOT be relied on to change the label of a version that already carries a suffix; a label change needs a `release-as` value (see "Forced version via the release-as config key"). GA drops the suffix: `prerelease: false` plus a visible carrier commit per package, in dependency order.

#### Scenario: Beta releases are flagged Pre-release
- **WHEN** the Release PR for `1.0.0-beta.N` is merged
- **THEN** release-please creates tag `v1.0.0-beta.N` and a GitHub Release marked Pre-release, and the image and the example assets are published under that tag

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
