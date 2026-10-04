# ADR-018: Draft-First Releases and Immutable Version Tags

## Status

Accepted

## Context

The workspace rule "Release Tags Are Immutable" (root `AGENTS.md`; enhancement 0021 D10) says no
git tag is ever moved, deleted or re-created, by anyone, and a version-named registry tag such as
`:v1.0.0-beta.3` always names the content first pushed under it. A wrong or broken release is fixed
by releasing the next version. The documentation site pins a ref per site version, so a moved tag
would silently change published docs, and a re-pointed image tag would change what a pinned
consumer pulls.

The organization enforces the git half with rulesets the owner manages: `tags-immutable` refuses
update and deletion of every tag with an empty bypass list, and `tags-create-app-only` lets only the
`opm-release-please` App create a tag. GitHub immutable releases go further: once a release is
published, its assets can no longer be added, replaced or removed. The operator joins immutable
releases only after it has shipped one release that was created as a draft, received every asset
while a draft and was published last (0021:D10:R8).

Before this decision the release workflow did the opposite. release-please published the GitHub
Release first, then `image-release` and `publish-examples` uploaded `install.yaml` and the example
manifests with `gh release upload --clobber`. Under immutable releases every such upload is refused,
leaving a public, asset-less release that can never be repaired. The image job also pushed
`:vX.Y.Z` unconditionally, so a re-run overwrote a published version tag, and nothing serialized
release runs. Draft releases make that last gap worse: GitHub accepts several drafts for one tag
name, so release-please's duplicate-release guard no longer fires.

## Decision

The GitHub Release is a draft until everything is attached, and one job makes it public.
`release-please-config.json` sets `"draft": true` and `"force-tag-creation": true`, so the App
creates the tag eagerly at the release commit and the downstream jobs can check it out while the
release is still a draft. Every upload step first runs `.github/scripts/release-guard.sh
assert-draft`, which refuses unless exactly one release carries the tag and it is a draft; `--clobber`
therefore only ever replaces an asset on a draft. A final `publish-release` job needs every release
job, holds `contents: write` only, checks out just `.github/scripts` at the tag and runs
`release-guard.sh publish`, which confirms `install.yaml` and `opm-examples.tar.gz` are attached and
publishes the draft by release id. It sends neither the prerelease flag nor `make_latest`, so
release-please's Pre-release flag and GitHub's Latest selection behave as before. Every `gh` call
names the repository.

Release runs are serialized with a workflow-level concurrency group keyed by ref,
`cancel-in-progress: false`. The release guard's exactly-one check turns any remaining duplicate
into a loud failure instead of an ambiguous by-tag lookup.

The version image tag is written only when absent. Before building, `image-release` runs
`.github/scripts/image-tag-guard.sh probe`. When `:vX.Y.Z` is absent the job builds, pushes and then
verifies that the tag resolves to the pushed digest. When it exists and its
`org.opencontainers.image.revision` label names the release commit, a previous attempt of this
release pushed it: the job pushes nothing under the version tag, reuses that digest for signing,
attestation and `install.yaml`, and re-points only the mutable `:sha-<short>` and `:latest`. Any
other content, or any probe error other than "not found", fails the job before it builds. Image
metadata reads the checked-out tag (`context: git`), so the revision label and `:sha-<short>` name
the release commit rather than the pushed commit.

The release-please job keeps the App token as its only token. release-please swallows every HTTP 422
on tag creation, not only "already exists"; with any other token the ruleset refuses the tag, the
release is drafted without one, and the downstream checkout fails.

Two alternatives were set aside. A second release tool (goreleaser or a hand-written `gh release
create`) would add a second release actor beside the App, the only identity allowed to create a tag.
Comparing a fresh build's digest with the existing version tag would fail every legitimate re-run,
because multi-arch builds with unpinned bases are not byte-reproducible.

The flow was proven in `open-platform-model/release-flow-sandbox` on 2026-10-01 with both tag
rulesets and immutable releases on; the evidence is in the archived change
`adopt-draft-first-release`.

## Consequences

**Positive:** A release is public only once it carries every asset, so the flow survives immutable
releases and the operator can join them after its first draft-first release. No workflow in this
repository can move, delete or re-create a tag, or make `:vX.Y.Z` name different content.

**Recovery rolls forward.** A run that fails before publish leaves a mutable draft. Recover it with
"Re-run failed jobs" on the same workflow run, never "Re-run all jobs": the latter re-runs
release-please, which finds the release PR already labelled `autorelease: tagged`, reports no
release, skips every downstream job and ends green with the draft stranded and no failed job left
to re-run. A stranded draft burns its version: the draft stays (or the owner deletes it in the UI,
never the tag) and the next release ships. A re-run replays the workflow definition of the original
run but reads the current repository variables, so a workflow fix on `main` cannot reach an old run.
A published release that is wrong is fixed by the next `1.0.0-beta.N` (after GA, the next patch),
and the broken release and its tag stay as they are.

**Duplicate draft.** If the release-please job fails after creating the release but before it
relabels the release PR, and is re-run, release-please swallows the 422 on the existing tag and
creates a second draft for it. From then on every upload and the publish fail with "expected
exactly one release, found 2". Recovery: the owner deletes the extra draft (the one without assets,
or the newer one) in the GitHub UI, never the tag; agents cannot, because the workspace hook blocks
`gh release delete`. Then "Re-run failed jobs" publishes the remaining draft with its tag
unchanged.

**Trade-off:** The reuse path identifies "this release's image" by its revision label, which only
this workflow writes and only this workflow pushes under a version tag. Anyone able to push a forged
image to `ghcr.io/open-platform-model/opm-operator` already defeats the rule; byte-reproducible
builds would remove the trust in the label at a cost this repository does not pay.

**Negative:** Between tag creation and publish the tag exists without a public release, so
`releases/download/<tag>/install.yaml` returns not found for that tag. The cli pins the operator
version, so it only reaches a new tag after a cli bump, by which time the release is public. The
human-run push paths (`.tasks/docker.yaml`, `Makefile` `docker-buildx`) can still overwrite a
version tag if `IMG` names one; they run in no workflow and are left to an owner decision.

**Deferred:** Release branches (`release/vX.Y`, the cut action, release runs on `release/**`) are
Phase 2 of 0021 D10, delivered before GA by a separate change. Until then a fix for a released
version goes forward on `main`.

**Addendum (2026-10-04):** the operator module (`modules/opm_operator`) releases on its own train
with tags `opm_operator-vX.Y.Z`, through the same draft-first flow (`module-publish`,
`module-manifest`, `module-publish-release`). For those tags only, `release-guard.sh publish`
requires `install.yaml` alone and sends `make_latest=false`, so a module release never takes the
repository's Latest mark (0021:D11:R12). Operator tags still send neither flag. A module draft
stranded by a broken script at its tag is recovered by `release.yml`'s `workflow_dispatch`
(`module_tag`), which runs only the module jobs, with `main`'s scripts against the tag's tree; the
tag and the published version stay as they are.
