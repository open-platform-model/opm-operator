## MODIFIED Requirements

### Requirement: Release published once after every release job
The release workflow SHALL contain a publish job (`publish-release`) that depends on every job producing a release artifact (the image job and the example publishing job), runs only when a release was cut, holds `contents: write` and no other write permission, checks out only the files it runs, confirms the required assets (`install.yaml`, `opm-examples.tar.gz`) are attached to the draft, and then publishes the draft. It SHALL leave the Pre-release flag set by release-please unchanged. When the release is already published it SHALL succeed without changing anything. The only job that SHALL depend on it is the cascade notify job, which produces no release artifact.

#### Scenario: All release jobs succeed
- **WHEN** the image and example jobs finish successfully for `v1.0.0-beta.3`
- **THEN** the final job publishes the draft, and the GitHub Release is public, flagged Pre-release and carries every asset

#### Scenario: A release job fails
- **WHEN** the example publishing job fails for `v1.0.0-beta.3`
- **THEN** the final job does not run, the release stays a draft, and "Re-run failed jobs" on the same workflow run completes and publishes it

#### Scenario: Required asset missing
- **WHEN** the final job runs and the draft lacks `install.yaml`
- **THEN** the job fails and the release stays a draft

## ADDED Requirements

### Requirement: Release notifies downstream after it is published
The release workflow SHALL contain a job `notify-downstream` that calls the shared `open-platform-model/.github/.github/workflows/cascade-notify.yml@main` with the release tag `needs.release-please.outputs.tag_name`. The job SHALL need `release-please` and `publish-release`, and SHALL run only when `releases_created` is `true`, `publish-release` succeeded, and the repo variable `CASCADE_NOTIFY` is not `off`. It SHALL NOT wait for, or be skipped by, `publish-docs`. The caller job SHALL grant `contents: read` and no other permission, and SHALL pass no secrets. The App token and the `cascade` Environment belong to the called workflow (workspace RELEASING.md, "Notify after publish"). The notify job SHALL dispatch `upstream-released` only to `cli` (Phase 3 wiring contract §3.1).

#### Scenario: Published release notifies the cli
- **WHEN** the release run for `v1.0.0-beta.9` publishes the draft with `install.yaml` attached
- **THEN** `notify-downstream` runs after `publish-release`, and the cli receives a `repository_dispatch` of type `upstream-released` with source `opm-operator` and tag `v1.0.0-beta.9`

#### Scenario: Release left a draft does not notify
- **WHEN** `publish-examples` fails, so `publish-release` does not run and the release stays a draft
- **THEN** `notify-downstream` does not run, and "Re-run failed jobs" that completes and publishes the release also runs `notify-downstream`

#### Scenario: Failed notify is recovered without republishing
- **WHEN** `publish-release` succeeded and `notify-downstream` failed, for example because the App installation was suspended
- **THEN** "Re-run failed jobs" re-runs only `notify-downstream`, and the release stays as it is

#### Scenario: Notify switched off
- **WHEN** the repo variable `CASCADE_NOTIFY` is `off` and a release is published
- **THEN** `notify-downstream` is skipped, the release is still published, and the cli's daily sweep picks the release up

#### Scenario: Docs failure does not hold notify
- **WHEN** `publish-docs` fails and `publish-release` succeeds
- **THEN** `notify-downstream` still runs

#### Scenario: Run without a release
- **WHEN** a push to `main` cuts no release
- **THEN** `notify-downstream` is skipped
