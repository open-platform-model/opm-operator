## Why

The workspace rule (owner, 2026-10-01; enhancements 0021 D10) makes release tags immutable: no tag under `refs/tags/` is ever moved, deleted or re-created, and a version-named registry tag (`:vX.Y.Z` on GHCR) is never overwritten. A broken release is fixed by releasing the next version. The reason is the docs system: opmodel.dev pins a ref per site version, so a moved tag silently changes published docs.

The org enforces the git half with rulesets: `tags-immutable` (no tag update or deletion, empty bypass) and `tags-create-app-only` (only the opm-release-please App creates tags), plus `release-branches` for the later maintenance branches. Per repo, GitHub immutable releases follow. Because only the App can create a tag, a stale or hand-made tag cannot exist, so this change carries no tag-commit assertion.

The operator's release flow cannot run under immutable releases today. release-please publishes the GitHub Release first, then `image-release` and `publish-examples` upload `install.yaml` and the example manifests with `gh release upload --clobber`. GitHub refuses any asset change on a published immutable release, so every operator release would fail half-way. Two further gaps exist with or without that switch:

- The image job pushes `:vX.Y.Z` unconditionally, so a re-run or a second release run overwrites a published version tag.
- Nothing serializes release runs. With draft releases, release-please's duplicate-release guard (HTTP 422 on an existing published release) no longer fires, because GitHub accepts several drafts for one tag name.

This change is Phase 1 of the owner's plan (Revision 2, 2026-10-01). Release-branch support (`release/vX.Y` maintenance branches, the cut action, release runs on `release/**`) is Phase 2, delivered before GA by a separate change; design.md "Phase 2 inputs" records what that change must carry.

## What Changes

- **Draft-first release.** `release-please-config.json` sets `"draft": true` and `"force-tag-creation": true` on package `"."`: the App creates the tag eagerly at the release commit and the GitHub Release starts as a draft.
- **Serialized release runs.** `release.yml` gets a workflow-level concurrency group with `cancel-in-progress: false`, and the release guard refuses to act unless exactly one release carries the tag.
- **Uploads go to the draft only.** Every upload step first runs a small guard script that asserts exactly one release for the tag and that it is a draft. `--clobber` stays and only ever applies to a draft. Every `gh` call names the repository.
- **One final publish job.** A new `publish-release` job needs every release job, sets `GH_TOKEN` and `GH_REPO`, sparse-checks out the guard script at the tag, asserts the required assets are attached, then publishes the draft by release id. A failed run leaves a mutable draft, recovered with "Re-run failed jobs".
- **Version-tag overwrite guard.** Before building, `image-release` probes `:vX.Y.Z`. Absent: build and push, then confirm the tag resolves to the pushed digest. Present and built from the release commit: reuse that exact digest and push nothing under the version tag. Present with any other content, or the probe errors: fail. `:latest` and `:sha-<short>` stay mutable by design.
- **Image metadata follows the tag commit.** `docker/metadata-action` reads the checked-out tag (`context: git`), so the revision label and `:sha-<short>` name the release commit, not the pushed commit.
- **Docs.** ADR-018 records the flow. `AGENTS.md` gains one line pointing at the workspace rule.
- **Specs.** `release-automation` and `container-image-publish` get MODIFIED and ADDED deltas.

Release class: none, before and after GA. Every commit is `ci` or `chore`, which release-please hides, so merging cuts no release; the next natural release is the first draft-first one. No API type, CRD, controller or reconcile behavior changes. Complexity is justified by the platform rule (Principle VII): each mechanism closes a path by which a version tag could name different content, or by which a release would fail under immutable releases.

Gates: **G-sandbox** (the unknowns in design.md proven in `open-platform-model/release-flow-sandbox`) blocks section 1; **G-owner** (owner sign-off on the sandbox evidence) blocks the merge. Adding opm-operator to the org immutable-releases list happens only after one real draft-first release has shipped, and is an owner action outside this change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-automation`: the GitHub Release is created as a draft with an eagerly created tag; release runs are serialized; assets upload only to a draft; one final job publishes the release; tags are never moved, deleted or re-created by the workflow, and recovery rolls forward.
- `container-image-publish`: `:vX.Y.Z` is never overwritten (reuse the existing digest when it holds the release commit, refuse otherwise); image revision metadata follows the tag commit; `install.yaml` uploads to the draft release.

## Impact

- Code: `release-please-config.json`, `.github/workflows/release.yml`, new `.github/scripts/release-guard.sh` and `.github/scripts/image-tag-guard.sh`, `adr/018-draft-first-release-and-immutable-version-tags.md`, `AGENTS.md`.
- Release: none from this change. The next release is the first to run draft-first; a failure there leaves a draft (re-run failed jobs) or, after publish, is fixed by the next `1.0.0-beta.N`.
- Downstream: `opm operator install` in the cli uses a pinned operator tag (`PinnedOperatorVersion`), either from its embedded manifest or from `releases/download/<tag>/install.yaml`. A draft is invisible to anonymous readers, so during the window between tag and publish that URL returns not found for the new tag, where today it can return a release missing `install.yaml`. The cli pins the operator tag, so it only reaches a new tag after a cli bump, by which time the release is public.
- Platform (owner, outside this change): the org tag rulesets already target this repo; immutable releases for opm-operator follow one shipped draft-first release.
- Enhancement: 0021 D10 ("Release tags are immutable", enhancements PR 74, open). `enhancement.yaml` declares it; delivery is logged at archive only once D10 is on enhancements `main`.
