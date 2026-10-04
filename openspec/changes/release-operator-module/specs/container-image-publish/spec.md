## MODIFIED Requirements

### Requirement: Release image build trigger gated on release-please

The release image job SHALL only execute when the `release-please` job in the same workflow run reports that the operator package `"."` created a release (`outputs.release_created == 'true'`), and SHALL take the release tag from that package's `outputs.tag_name`. A release created only by the operator module's package SHALL NOT run it. When a Release PR is opened or updated without a release being cut, the image job SHALL be skipped.

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
