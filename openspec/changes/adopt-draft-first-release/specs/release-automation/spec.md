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

### Requirement: Release tag matches the release commit
When release-please reports `releases_created == 'true'`, the release workflow SHALL resolve the release tag's commit from the remote (the peeled commit when the tag is annotated) and compare it with release-please's `sha` output before any image is built, any asset is uploaded or the release is published. On a mismatch, or when the tag does not exist, the workflow SHALL fail and SHALL NOT move, delete or re-create the tag.

#### Scenario: Tag at the release commit
- **WHEN** release-please cuts `v1.0.0-beta.3` and the remote tag resolves to the same commit as the `sha` output
- **THEN** the assertion passes and the image, asset and publish jobs run

#### Scenario: Pre-existing tag at another commit
- **WHEN** a tag `v1.0.0-beta.3` already existed at a different commit and release-please adopted it while creating the draft
- **THEN** the release-please job fails with a message naming both commits, no image or asset is produced, the release stays a draft, and the fix is the next `1.0.0-beta.N`

### Requirement: Release assets upload only to a draft release
Every step that uploads an asset to the GitHub Release (`install.yaml`, `opm-examples.tar.gz`, the example manifests) SHALL first confirm that the release for the tag is a draft and SHALL fail without uploading when it is published. Replacing an existing asset (`--clobber`) SHALL be used only on a draft.

#### Scenario: Upload to the draft
- **WHEN** the image job uploads `install.yaml` for a release that is still a draft
- **THEN** the upload succeeds, and re-running the job replaces the asset on the draft

#### Scenario: Upload to a published release refused
- **WHEN** an upload step runs for a tag whose release is already published
- **THEN** the step fails before uploading and tells the operator to release the next version instead

### Requirement: Release published once after every release job
The release workflow SHALL contain a final job that depends on every job producing a release artifact (the image job and the example publishing job), runs only when a release was cut, holds `contents: write` and no other write permission, confirms the required assets (`install.yaml`, `opm-examples.tar.gz`) are attached to the draft, and then publishes the draft. It SHALL leave the Pre-release flag set by release-please unchanged. When the release is already published it SHALL succeed without changing anything.

#### Scenario: All release jobs succeed
- **WHEN** the image and example jobs finish successfully for `v1.0.0-beta.3`
- **THEN** the final job publishes the draft, and the GitHub Release is public, flagged Pre-release and carries every asset

#### Scenario: A release job fails
- **WHEN** the example publishing job fails for `v1.0.0-beta.3`
- **THEN** the final job does not run, the release stays a draft, and "Re-run failed jobs" on the same workflow run completes and publishes it

#### Scenario: Required asset missing
- **WHEN** the final job runs and the draft lacks `install.yaml`
- **THEN** the job fails and the release stays a draft

### Requirement: Release tags are never moved, deleted or re-created
No workflow, task or script in this repository SHALL move, delete or re-create a git tag or delete a GitHub Release. A failed run before publish SHALL be recovered by re-running its failed jobs; a wrong published release or a tag at the wrong commit SHALL be fixed by releasing the next version.

#### Scenario: Workflows carry no tag mutation
- **WHEN** `.github/workflows/`, `Taskfile.yml`, `.tasks/` and `hack/` are searched for `git tag`, a tag push, `gh release delete`, `gh release edit --tag`/`--target` or a write to the git refs API
- **THEN** no match exists

#### Scenario: Broken published release
- **WHEN** a published release turns out to ship a broken image or asset
- **THEN** a `fix` commit cuts the next `1.0.0-beta.N`, and the broken release and its tag stay as they are
