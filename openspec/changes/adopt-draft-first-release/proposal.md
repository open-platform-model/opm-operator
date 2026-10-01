## Why

The workspace rule (owner, 2026-10-01) makes release tags immutable: no tag under `refs/tags/` is ever moved, deleted or re-created, and a version-named registry tag (`:vX.Y.Z` on GHCR) is never overwritten. A broken release is fixed by releasing the next version. The reason is the docs system: opmodel.dev pins a ref per site version, so a moved tag silently changes published docs.

The org enforces the git half with a tag ruleset and, per repo, GitHub immutable releases. The operator's release flow cannot run under immutable releases today. release-please publishes the GitHub Release first, then `image-release` and `publish-examples` upload `install.yaml` and the example manifests with `gh release upload --clobber`. GitHub refuses any asset change on a published immutable release, so every operator release would fail half-way. Two smaller gaps exist with or without that switch:

- release-please's `createRef` ignores an "already exists" error, so a stale tag at the wrong commit would be adopted silently and the release built from it.
- The image job pushes `:vX.Y.Z` unconditionally, so a re-run or a second release run overwrites a published version tag.

## What Changes

- **Draft-first release.** `release-please-config.json` sets `"draft": true` and `"force-tag-creation": true` on package `"."`: the tag is created eagerly at the release commit and the GitHub Release starts as a draft.
- **Uploads go to the draft only.** Both upload steps first assert `isDraft == true` and refuse a published release; `--clobber` stays, and is only ever applied to a draft.
- **One final publish job.** A new `publish-release` job needs `release-please`, `image-release` and `publish-examples`, asserts the required assets are on the draft, then runs `gh release edit "$TAG" --draft=false`. A failed run leaves a mutable draft, recovered with "Re-run failed jobs".
- **Tag-SHA assertion.** Right after release-please, the job compares the tag's commit from `git ls-remote` (peeled when annotated) with release-please's `sha` output and fails on any mismatch, before any image or asset is produced.
- **Version-tag overwrite guard.** Before building, `image-release` probes `:vX.Y.Z`. Absent: build and push as today. Present with an `org.opencontainers.image.revision` label equal to the tag commit: adopt that digest and skip the build (idempotent re-run). Present with any other revision, or the probe errors: fail. `:latest` and `:sha-<short>` stay mutable by design.
- **Image metadata follows the tag commit.** The revision label and the `:sha-<short>` tag come from release-please's `sha` output, not `github.sha`. The two differ when a release is cut on a later push than its merge.
- **Docs.** ADR-018 records the flow. `AGENTS.md` gains one line pointing at the workspace rule.
- **Specs.** `release-automation` and `container-image-publish` get MODIFIED and ADDED deltas.

Release class: none, before and after GA. Every commit is `ci` or `chore`, which release-please hides, so merging cuts no release; the next natural release is the first draft-first one. No API type, CRD, controller or reconcile behavior changes.

Gates: **G-sandbox** (the unknowns in design.md proven in `open-platform-model/release-flow-sandbox`) blocks section 1, and **G-owner** (owner sign-off on the sandbox evidence) blocks the merge. Adding opm-operator to the org immutable-releases list happens only after one real draft-first release has shipped, and is an owner action outside this change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-automation`: the GitHub Release is created as a draft with an eagerly created tag; the tag's commit is asserted; assets upload only to a draft; one final job publishes the release; tags are never moved, deleted or re-created by the workflow, and recovery rolls forward.
- `container-image-publish`: `:vX.Y.Z` is never overwritten (adopt on same revision, refuse otherwise); image revision metadata follows the tag commit; `install.yaml` uploads to the draft release.

## Impact

- Code: `release-please-config.json`, `.github/workflows/release.yml`, `adr/018-draft-first-release-and-immutable-version-tags.md`, `AGENTS.md`.
- Release: none from this change. The next release is the first to run draft-first; a failure there leaves a draft (re-run failed jobs) or, after publish, is fixed by the next `1.0.0-beta.N`.
- Downstream: `opm operator install` in the cli resolves release assets. A draft is invisible to anonymous readers, so during the window between tag and publish the cli sees no release for the new tag, where today it sees a release missing `install.yaml`. That is an improvement, not a regression.
- Platform (owner, outside this change): org ruleset `tags-immutable` already targets this repo; immutable releases for opm-operator follow one shipped draft-first release.
- Enhancement: enhancements 0021 is gaining a "Release tags are immutable" decision whose number is not assigned yet; `enhancement.yaml` is added once it is (task 4.1).
