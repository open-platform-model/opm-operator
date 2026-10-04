## MODIFIED Requirements

### Requirement: Release image build trigger gated on release-please

The release image job SHALL only execute when the `release-please` job in the same workflow run reports that the operator package `"."` created a release (`outputs.release_created == 'true'`), and SHALL take the release tag from that package's `outputs.tag_name`. A release created only by the operator module's package SHALL NOT run it. When a Release PR is opened or updated without a release being cut, the image job SHALL be skipped. Source: 0028:D1:R9.

#### Scenario: Release cut after merge of Release PR
- **WHEN** a push to `main` causes release-please to tag version `v1.2.3` and create a GitHub release
- **THEN** the image-release job SHALL run and publish the image

#### Scenario: Push to main opens or updates a Release PR only
- **WHEN** a push to `main` causes release-please to open or update a Release PR (but not cut a release)
- **THEN** the image-release job SHALL be skipped and its status SHALL be reported as skipped (not failed) in the GitHub Actions UI

#### Scenario: Push to main with no releasable commits
- **WHEN** a push to `main` contains only `chore`, `docs`, `test`, `ci`, or `build` commits and release-please takes no action
- **THEN** the image-release job SHALL be skipped

#### Scenario: Module release only
- **WHEN** a push to `main` merges the module's release PR and release-please creates only `opm_operator-v0.2.0`
- **THEN** the image-release job SHALL be skipped, and no image tag is pushed

## REMOVED Requirements

### Requirement: Release install manifest with digest-pinned image

**Reason**: 0028:D2:R13. The install manifest is the operator module's render at default values, and it is published by the module's release (`operator-module-release`, "Every module release publishes the install manifest rendered from it"). An operator release no longer renders `config/default` into an asset. `task operator:installer` stays a development task.

**Migration**: Download `install.yaml` from the newest module release (`opm_operator-vX.Y.Z`), or install with `opm operator install`. A module release names the operator image by tag and digest, so the manifest still pulls the image by digest.
