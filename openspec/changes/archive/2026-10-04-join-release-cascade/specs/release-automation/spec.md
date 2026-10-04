## MODIFIED Requirements

### Requirement: Git tag and GitHub Release on merge
When the Release PR is merged to `main`, release-please SHALL create the git tag (e.g., `v0.2.0`) eagerly at the release commit and a GitHub Release in draft state with the changelog section as release notes. The release-please package SHALL be configured with `draft: true` and `force-tag-creation: true`. The draft SHALL become public only through the publish job `publish-release` defined in "Release published once after every release job".

#### Scenario: Release PR merged
- **WHEN** the Release PR is merged to `main`
- **THEN** release-please creates a git tag matching the version (prefixed with `v`) at the release commit and a draft GitHub Release with the changelog for that version as the body, and the release is not visible as published until every release job has succeeded

#### Scenario: Release PR closed without merge
- **WHEN** the Release PR is closed without merging
- **THEN** no tag or release SHALL be created; the next push to `main` re-opens or creates a new Release PR

#### Scenario: Config enables draft-first
- **WHEN** `release-please-config.json` is inspected on `main`
- **THEN** package `"."` SHALL set `"draft": true` and `"force-tag-creation": true`

### Requirement: Release published once after every release job
The release workflow SHALL contain a publish job (`publish-release`) that depends on every job producing a release artifact (the image job and the example publishing job), runs only when a release was cut, holds `contents: write` and no other write permission, checks out only the files it runs, confirms the required assets (`install.yaml`, `opm-examples.tar.gz`) are attached to the draft, and then publishes the draft. It SHALL leave the Pre-release flag set by release-please unchanged. When the release is already published it SHALL succeed without changing anything. The only job that SHALL depend on it is the cascade notify job, which produces no release artifact.

#### Scenario: All release jobs succeed
- **WHEN** the image and example jobs finish successfully for `v1.0.0-beta.3`
- **THEN** `publish-release` publishes the draft, and the GitHub Release is public, flagged Pre-release and carries every asset

#### Scenario: A release job fails
- **WHEN** the example publishing job fails for `v1.0.0-beta.3`
- **THEN** `publish-release` does not run, the release stays a draft, and "Re-run failed jobs" on the same workflow run completes and publishes it

#### Scenario: Required asset missing
- **WHEN** `publish-release` runs and the draft lacks `install.yaml`
- **THEN** the job fails and the release stays a draft

### Requirement: Dependabot leaves OPM Go modules to the release cascade
The `gomod` entry of `.github/dependabot.yml` SHALL ignore every dependency matching `github.com/open-platform-model/*`. Those pins move only through the release cascade, which titles a shipped bump `fix(deps)` so it releases (workspace RELEASING.md, section "Pin classes"). The `github-actions` entry SHALL ignore `open-platform-model/.github*`: the cascade references move together, one `.github` SHA for the repo, only through a `ci(deps): pin the cascade to .github <sha7>` pull request (Phase 3 wiring contract (version 3.1) §2.4, §10.1 item 7). Third-party Go modules and other GitHub Actions SHALL keep their Dependabot updates.

#### Scenario: Library release opens no Dependabot PR
- **WHEN** library publishes a new tag
- **THEN** Dependabot opens no PR bumping `github.com/open-platform-model/library` in opm-operator

#### Scenario: Third-party bumps continue
- **WHEN** a new `k8s.io/api` release exists
- **THEN** Dependabot still proposes the grouped Kubernetes bump

#### Scenario: A .github main commit opens no Dependabot PR
- **WHEN** a commit lands on `open-platform-model/.github` `main` after the repo's pinned SHA
- **THEN** Dependabot opens no PR moving any `open-platform-model/.github` reference, and the five references keep one SHA

## ADDED Requirements

### Requirement: Release notifies downstream after it is published
The release workflow SHALL contain a job `notify-downstream`, owned by this repo, that needs `release-please` and `publish-release`, runs on `ubuntu-latest`, declares `environment: cascade`, has a 20-minute timeout, grants `contents: read` and no other permission, and has exactly one step: the action `open-platform-model/.github/.github/actions/cascade-notify` at the repo's pinned `.github` `main` SHA, with the inputs `tag: ${{ needs.release-please.outputs.tag_name }}`, `client-id: ${{ vars.CASCADE_APP_CLIENT_ID }}` and `private-key: ${{ secrets.CASCADE_APP_PRIVATE_KEY }}` and no other input. It SHALL run only when `releases_created` is `true`, `publish-release` succeeded, and the repo variable `CASCADE_NOTIFY` is not `off`. It SHALL NOT wait for, or be skipped by, `publish-docs`. The key SHALL be read only in the caller-owned `notify-downstream` or `publish` job, which declares `environment: cascade` and passes `secrets.CASCADE_APP_PRIVATE_KEY` only as the `private-key` input of the SHA-pinned cascade action; that job SHALL have no checkout or `run:` of its own, and no `env:`, `container:` or `services:`; no reusable call SHALL pass `secrets:` or `secrets: inherit`. The action mints the App token and SHALL dispatch `upstream-released` only to `cli` (Phase 3 wiring contract (version 3.1) §3.1, §4.3, §4.6, §10.1 item 9; workspace RELEASING.md, "Notify after publish").

#### Scenario: Published release notifies the cli
- **WHEN** the release run for `v1.0.0-beta.9` publishes the draft with `install.yaml` attached
- **THEN** `notify-downstream` runs after `publish-release` in the `cascade` Environment, and the cli receives a `repository_dispatch` of type `upstream-released` with source `opm-operator` and tag `v1.0.0-beta.9`

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

#### Scenario: A step added beside the action is refused
- **WHEN** a pull request adds a checkout or a `run:` step to `notify-downstream`
- **THEN** the `Lint` job's "Verify the cascade wiring" step fails naming `release.yml:notify-downstream step count`
