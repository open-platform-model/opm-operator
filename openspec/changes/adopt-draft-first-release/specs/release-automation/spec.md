## MODIFIED Requirements

### Requirement: Git tag and GitHub Release on merge
When the Release PR is merged to `main`, release-please SHALL create the git tag (e.g., `v0.2.0`) eagerly at the release commit and a GitHub Release in draft state with the changelog section as release notes. The release-please package SHALL be configured with `draft: true` and `force-tag-creation: true`. The draft SHALL become public only through the final publish step defined in "Release published once after every release job".

#### Scenario: Release PR merged
- **WHEN** the Release PR is merged to `main`
- **THEN** release-please creates a git tag matching the version (prefixed with `v`) at the release commit and a draft GitHub Release with the changelog for that version as the body, and the release is not visible as published until every release job has succeeded

#### Scenario: Release PR closed without merge
- **WHEN** the Release PR is closed without merging
- **THEN** no tag or release SHALL be created; the next push to `main` re-opens or creates a new Release PR

#### Scenario: Config enables draft-first
- **WHEN** `release-please-config.json` is inspected on `main`
- **THEN** package `"."` SHALL set `"draft": true` and `"force-tag-creation": true`

## ADDED Requirements

### Requirement: Release runs serialized per branch
The release workflow SHALL declare a workflow-level concurrency group keyed by the pushed branch with `cancel-in-progress: false`, so at most one release run per branch executes at a time. Before any asset upload or publication, the workflow SHALL confirm that exactly one GitHub Release carries the release tag, and SHALL fail without uploading or publishing otherwise.

#### Scenario: Two pushes in quick succession
- **WHEN** the Release PR for `v1.0.0-beta.3` merges and another commit lands on `main` seconds later
- **THEN** the second run waits for the first to finish, and exactly one draft release exists for `v1.0.0-beta.3`

#### Scenario: Duplicate release for a tag
- **WHEN** two releases carry the tag `v1.0.0-beta.3`
- **THEN** every upload step and the publish job fail with a message naming the count, and neither release is published

### Requirement: Release assets upload only to a draft release
Every step that uploads an asset to the GitHub Release (`install.yaml`, `opm-examples.tar.gz`, the example manifests) SHALL first confirm that the single release for the tag is a draft and SHALL fail without uploading when it is published. Replacing an existing asset (`--clobber`) SHALL be used only on a draft. Every GitHub CLI call in the release workflow SHALL name the repository explicitly. Source: 0021:D10:R7.

#### Scenario: Upload to the draft
- **WHEN** the image job uploads `install.yaml` for a release that is still a draft
- **THEN** the upload succeeds, and re-running the job replaces the asset on the draft

#### Scenario: Upload to a published release refused
- **WHEN** an upload step runs for a tag whose release is already published
- **THEN** the step fails before uploading and tells the operator to release the next version instead

### Requirement: Release published once after every release job
The release workflow SHALL contain a final job that depends on every job producing a release artifact (the image job and the example publishing job), runs only when a release was cut, holds `contents: write` and no other write permission, checks out only the files it runs, confirms the required assets (`install.yaml`, `opm-examples.tar.gz`) are attached to the draft, and then publishes the draft. It SHALL leave the Pre-release flag set by release-please unchanged. When the release is already published it SHALL succeed without changing anything.

#### Scenario: All release jobs succeed
- **WHEN** the image and example jobs finish successfully for `v1.0.0-beta.3`
- **THEN** the final job publishes the draft, and the GitHub Release is public, flagged Pre-release and carries every asset

#### Scenario: A release job fails
- **WHEN** the example publishing job fails for `v1.0.0-beta.3`
- **THEN** the final job does not run, the release stays a draft, and "Re-run failed jobs" on the same workflow run completes and publishes it

#### Scenario: Required asset missing
- **WHEN** the final job runs and the draft lacks `install.yaml`
- **THEN** the job fails and the release stays a draft

### Requirement: Release maintenance branches
The release workflow SHALL also run on pushes to `release/**` branches and SHALL run release-please against the pushed branch. A release cut from a branch other than `main` SHALL NOT be marked the repository's Latest release and SHALL NOT move the `:latest` image tag. The repository SHALL carry a manually dispatched `cut-release-branch` workflow taking a released minor `X.Y` that calls the organization's reusable cut-release-branch workflow, pinned by full commit SHA, with tag prefix `v` and package `.`. That reusable workflow creates `release/vX.Y` from the newest `vX.Y.*` tag and opens a pull request into it with the branch-local release-please settings. No workflow in this repository SHALL delete a `release/*` branch.

#### Scenario: Maintenance branch cut
- **WHEN** a maintainer dispatches `cut-release-branch` with minor `1.0` after `v1.0.4` and `v1.1.0` are released
- **THEN** branch `release/v1.0` is created at the `v1.0.4` commit and a pull request into it sets `versioning: always-bump-patch` and `prerelease: false` for package `.`

#### Scenario: Patch released from a maintenance branch
- **WHEN** a backported `fix` merges into `release/v1.0` and the resulting Release PR for `v1.0.5` merges
- **THEN** the same release jobs run for `v1.0.5`, the image is pushed as `:v1.0.5` and `:sha-<short>` without moving `:latest`, and the published release is not marked Latest

### Requirement: Release tags are never moved, deleted or re-created
No workflow, task or script in this repository SHALL move, delete or re-create a git tag or delete a GitHub Release. A failed run before publish SHALL be recovered by re-running its failed jobs; a wrong published release SHALL be fixed by releasing the next version. Source: 0021:D10:R1.

#### Scenario: Workflows carry no tag mutation
- **WHEN** `.github/workflows/`, `.github/scripts/`, `Taskfile.yml`, `.tasks/` and `hack/` are searched for `git tag`, a `git push` of a tag ref (`refs/tags/`, `--tags`, `--mirror`, a delete or force refspec), `gh release delete`, `gh release edit --tag`/`--target`, or a write to the git refs API
- **THEN** no match exists

#### Scenario: Broken published release
- **WHEN** a published release turns out to ship a broken image or asset
- **THEN** a `fix` commit cuts the next `1.0.0-beta.N`, and the broken release and its tag stay as they are
