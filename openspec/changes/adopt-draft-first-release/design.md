## Context

See proposal.md (Why). State on `main` at planning time (2026-10-01, `27d9dfc`, `1.0.0-beta.2`):

- `release.yml` has three jobs. `release-please` (App token, action v5.0.0, which bundles release-please 17.6.0) exports `releases_created` and `tag_name` only. `image-release` checks out the tag, builds `linux/amd64,linux/arm64`, pushes `:sha-<short>`, `:<tag>` and `:latest`, signs, attests and then uploads `dist/install.yaml` with `gh release upload --clobber`. `publish-examples` (needs `image-release`) publishes the fixture modules through `opm module publish` (already refusal-tolerant, never overwrites) and uploads `opm-examples.tar.gz` and `dist/examples/*.yaml` with `--clobber`.
- release-please creates a published (non-draft) release flagged Pre-release, so assets land after publish.
- Verified on 2026-10-01 against GHCR: tag `v1.0.0-beta.2` is lightweight (no `^{}` line from `git ls-remote`) at `27d9dfc3c037`, and the `:v1.0.0-beta.2` image config label `org.opencontainers.image.revision` is `27d9dfc3c03777f218d69b3bbac435cc06b52af5`. `docker buildx imagetools inspect <ref> --format '{{json .Manifest.Digest}}'` and `--format '{{ (index .Image "linux/amd64").Config.Labels }}'` both work against it; a missing tag prints `ERROR: <ref>: not found`.

Platform state this change relies on (owner-managed, outside the repo): org ruleset `tags-immutable` (update, deletion, non_fast_forward on all tags, empty bypass, no `creation` rule) covers this repo. Immutable releases are NOT enabled for opm-operator and stay off until one draft-first release has shipped.

## Goals / Non-Goals

**Goals:**

- A GitHub Release becomes public only after every asset is attached, so the flow survives immutable releases.
- No step in this repo's workflows can move, delete or re-create a tag, or overwrite `:vX.Y.Z` on GHCR with a different image.
- A release whose tag points anywhere other than the release commit never produces an image, asset or published release.
- A failed release run is recovered by re-running failed jobs on the draft, never by touching the tag.

**Non-Goals:**

- No controller, API, CRD or reconcile change.
- No change to the PR image workflow (`:pr-N`, `:sha-<short>` stay mutable), to `publish-fixtures.yml`, or to the e2e fixture tags (`-e2e.g*`).
- No change to the `flux push artifact` default tag `v0.0.1` in `.tasks/release.yaml` (human-run only, not a release path).
- No org setting, ruleset or immutable-release toggle: owner actions.
- No reproducible-build work: the guard does not need byte-identical rebuilds (see "Adopt by revision, not by digest").

## Research & Decisions

### Draft-first through release-please, not a second release tool

**Context**: Immutable releases forbid asset changes after publish. The release must be a draft while assets are attached.
**Explored**: release-please CHANGELOG 17.2.0 (`force-tag-creation`, upstream PR 2627) and `docs/manifest-releaser.md`; at 17.3.0 `src/github.ts:1396-1410` runs `git.createRef refs/tags/<tag>` at `release.sha` before `createRelease`; the action v5.0.0 bundles 17.6.0); `src/manifest.ts` parses `draft` and `force-tag-creation` from the package config (`ReleaserConfigJson`), merged per path. GitHub: a draft release has no tag of its own until published, which is why the eager tag is required (the downstream jobs check out `tag_name`).
**Decision**: Package `"."` MUST set `"draft": true` and `"force-tag-creation": true`. Nothing else in the config changes.
**Rationale**: Keeps the single release actor (the App via release-please) and the existing outputs, and is the documented upstream answer to lazy tag creation for drafts.

### Tag-SHA assertion right after release-please

**Context**: `createRef` swallows HTTP 422 (tag exists). A pre-existing tag at another commit would be adopted silently, and `createRelease` attaches the release to it.
**Decision**: When `releases_created == 'true'`, the `release-please` job MUST run a step that resolves the tag's commit over HTTPS without a checkout and compares it to `steps.release.outputs.sha`; on mismatch or on an absent tag it MUST fail. The job also exports `sha`.

```bash
# TAG, WANT from step env; peeled line wins when the tag is annotated
refs=$(git ls-remote --tags "https://github.com/${GITHUB_REPOSITORY}" \
  "refs/tags/${TAG}" "refs/tags/${TAG}^{}")
got=$(awk -v t="refs/tags/${TAG}^{}" '$2==t{print $1}' <<<"$refs")
[ -n "$got" ] || got=$(awk -v t="refs/tags/${TAG}" '$2==t{print $1}' <<<"$refs")
[ -n "$got" ] || { echo "::error::tag ${TAG} not found"; exit 1; }
[ "$got" = "$WANT" ] || { echo "::error::tag ${TAG} is at ${got}, release commit is ${WANT}; roll forward, never move the tag"; exit 1; }
```

**Rationale**: Every downstream job `needs: release-please`, so a failing assertion stops image, assets and publish, leaving at most a draft. Placing it in the release-please job (not each consumer) checks once.

### Assets upload only to a draft

**Decision**: Each upload step MUST first read `gh release view "$TAG" --json isDraft -q .isDraft` and fail unless it prints `true`, with a message telling the operator to roll forward. `--clobber` stays: on a draft it makes "Re-run failed jobs" idempotent.
**Rationale**: Under immutable releases an upload to a published release fails anyway; the explicit check fails earlier with an actionable message and keeps the guarantee while immutable releases are still off.

### One final publish job

**Decision**: A new job `publish-release` MUST `needs: [release-please, image-release, publish-examples]`, run only when `releases_created == 'true'`, hold `contents: write` and nothing else, and:

1. read `isDraft`; when already `false`, exit 0 (a re-run after a successful publish is a no-op);
2. assert the draft carries `install.yaml` and `opm-examples.tar.gz` (`gh release view --json assets`);
3. run `gh release edit "$TAG" --draft=false`. The Pre-release flag set by release-please is left as is.

**Rationale**: A single publish point means a partial run is never public. The job does not checkout code.

### Adopt by revision, not by digest

**Context**: The canon asks the image push to refuse a different digest at an existing `:vX.Y.Z` and treat the same digest as a no-op. Multi-arch QEMU builds with unpinned bases are not byte-reproducible, so a rebuild on re-run yields a new digest and a digest comparison would make every legitimate re-run fail after the version tag was pushed.
**Explored**: live probe of `:v1.0.0-beta.2` (Context); `docker/metadata-action` sets `org.opencontainers.image.revision` from `github.sha`.
**Decision**: Before building, `image-release` MUST probe `:${TAG}`:

- not found: build and push `:sha-<short>`, `:${TAG}`, `:latest` as today;
- found, and the `linux/amd64` config label `org.opencontainers.image.revision` equals the release commit: skip the build, take the existing index digest as the job's digest, and continue (sign, attest, render `install.yaml`, upload). `:latest` is not re-pointed on this path;
- found with any other revision, or any probe output other than a digest or `not found`: fail.

The release commit is `needs.release-please.outputs.sha`. The metadata step MUST set the revision label and the `sha-<short>` tag from it (`labels: org.opencontainers.image.revision=<sha>`, `type=raw,value=sha-<short of sha>`), because `github.sha` is the pushed commit, which differs from the release commit when release-please cuts a release on a later push than the release PR merge.
**Rationale**: "Same digest" is unattainable for a rebuild; "same release commit" is the property the rule protects (a version tag never names other code). The existing image is then reused, so the digest is literally unchanged: the same-digest no-op holds.

### Recovery rolls forward

**Decision**: Workflows MUST NOT contain `git tag`, `git push` of tags, `gh release delete`, `gh release edit --tag/--target` or a GitHub refs API write. A failed run before publish is recovered with "Re-run failed jobs" on the same workflow run (it keeps the release-please outputs). A published release that is wrong, or a tag at the wrong commit, is fixed by the next `1.0.0-beta.N` (a `fix` carrier commit).

### G-sandbox: unknowns proven before section 1

Each item is proven in `open-platform-model/release-flow-sandbox` by the sandbox worker with this exact config and job shape, and the evidence (run URLs, outputs) is written into this section in task 1.1. Section 1 does not start without it.

- U1: with `draft: true` + `force-tag-creation: true`, the action still sets `releases_created`, `tag_name` and `sha` on the merge run, and the tag exists at `sha` when the job ends.
- U2: the next Release PR anchors on the forced tag while the previous release is still a draft (no re-proposal of the drafted version, correct changelog window).
- U3: `gh release view|upload|edit` resolve a draft by tag name with GITHUB_TOKEN (the REST "get release by tag" endpoint omits drafts; gh falls back to listing).
- U4: `gh release edit --draft=false` keeps the Pre-release flag and the forced tag, under the org `tags-immutable` ruleset and with immutable releases on.
- U5: an attempted upload after publish with immutable releases on fails (proves the draft check is the only path).
- U6: with the ruleset on, `createRef` by the App succeeds (no `creation` rule) and a pre-existing tag at another commit is adopted by `createRelease` (proves the assertion is needed and fires).

## Reconcile phase impact

None. Source, Render, Apply, Prune and Status are untouched: this change edits only release automation (`release-please-config.json`, `.github/workflows/release.yml`) and docs.

## Risks / Trade-offs

- [The first draft-first release is the first production run] → G-sandbox proves the shape; a failure before publish leaves a draft (re-run); a failure after publish costs one beta number.
- [A draft left behind (job failed, nobody re-ran)] → the tag exists without a public release; the cli sees no release for it. Recovery: re-run failed jobs. The tag is never deleted.
- [Adopt path trusts a label] → the label is written only by this workflow and the version tag is pushed only by it; anyone able to push a forged image to `ghcr.io/open-platform-model/opm-operator` already defeats the rule. Deliberate trade-off against byte-reproducible builds.
- [`:latest` not re-pointed on the adopt path] → `:latest` is mutable by design and already points at the image when the first attempt reached the push; a crash between the version push and `:latest` is impossible because build-push writes all tags in one push.
- [Concurrent release runs] → two pushes in quick succession can run release-please twice; release-please's own release-PR labels prevent a double release, and the probe plus `--clobber`-on-draft keep a second image or asset run idempotent. No concurrency group is added (YAGNI).
- [Spec drift] → `Release build architecture set` says exactly two platform descriptors, while buildx also adds `unknown/unknown` attestation entries; pre-existing and untouched here.
