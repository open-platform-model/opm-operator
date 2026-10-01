## Context

See proposal.md (Why). State on `main` at planning time (2026-10-01, `27d9dfc`, `1.0.0-beta.2`):

- `release.yml` runs on pushes to `main` only and has three jobs. `release-please` (App token, action v5.0.0, which bundles release-please 17.6.0) exports `releases_created` and `tag_name`. `image-release` checks out the tag, builds `linux/amd64,linux/arm64`, pushes `:sha-<short>`, `:<tag>` and `:latest`, signs, attests and uploads `dist/install.yaml` with `gh release upload --clobber`. `publish-examples` (needs `image-release`) publishes the fixture modules through `opm module publish` (already refusal-tolerant, never overwrites) and uploads `opm-examples.tar.gz` and `dist/examples/*.yaml` with `--clobber`. There is no concurrency group.
- release-please creates a published (non-draft) release flagged Pre-release, so assets land after publish.
- Verified on 2026-10-01 against GHCR: tag `v1.0.0-beta.2` is lightweight at `27d9dfc3c037`; the `:v1.0.0-beta.2` index digest is `sha256:a130b542...`; its `linux/amd64` config label `org.opencontainers.image.revision` is `27d9dfc3c03777f218d69b3bbac435cc06b52af5` and `org.opencontainers.image.version` is `v1.0.0-beta.2`. `docker buildx imagetools inspect <ref> --format '{{json .Manifest.Digest}}'` and `--format '{{ (index .Image "linux/amd64").Config.Labels }}'` both work; a missing tag prints `ERROR: <ref>: not found`.
- `gh release view v1.0.0-beta.2 --json isDraft` outside a git checkout fails with `failed to run git: fatal: not a git repository`; with `--repo` or `GH_REPO` it works. Every `gh` call in this design therefore names the repository.

Platform state this change relies on (owner-managed, outside the repo, Revision 2026-10-01):

- `tags-immutable`: update, deletion and non_fast_forward refused on every tag, empty bypass.
- `tags-create-app-only`: tag creation refused for everyone except the opm-release-please App. A tag in this repo is therefore always one release-please created at its release commit; a stale or hand-made tag cannot exist. This is why the per-repo tag-commit assertion of the first draft of this plan is dropped.
- `release-branches`: `refs/heads/release/*` refuses deletion and non_fast_forward and requires a pull request (squash only), no bypass.
- Immutable releases are NOT enabled for opm-operator and stay off until one draft-first release has shipped.

## Goals / Non-Goals

**Goals:**

- A GitHub Release becomes public only after every asset is attached, so the flow survives immutable releases.
- No step in this repo's workflows can move, delete or re-create a git tag, or make `:vX.Y.Z` on GHCR name content other than what was first pushed under it (0021:D10:R1, 0021:D10:R3).
- At most one release run per branch at a time, and at most one release per tag.
- A failed release run is recovered by re-running failed jobs on the draft, never by touching the tag.
- A released minor can receive patch releases from a lazily cut `release/vX.Y` branch without disturbing `main`'s `:latest` or the repository's Latest release.

**Non-Goals:**

- No controller, API, CRD or reconcile change.
- No change to the PR image workflow (`:pr-N`, `:sha-<short>` stay mutable), to `publish-fixtures.yml`, or to the e2e fixture tags (`-e2e.g*`).
- No release branch is cut. None exists during beta; the first is cut at GA or when `main` starts work a released minor must not get.
- No org setting, ruleset or immutable-release toggle: owner actions.
- No reproducible-build work: the image guard does not need byte-identical rebuilds (see "Never overwrite a version image tag").
- Known gaps left for an owner decision, not guarded here: the human-run push paths `.tasks/docker.yaml` (`buildx build --push --tag {{.IMG}}`) and `Makefile` (`docker-buildx`) overwrite `:vX.Y.Z` if `IMG` names one, and `.tasks/release.yaml` `publish:ghcr` re-pushes the floating `v0.0.1` flux artifact tag under `testing.opmodel.dev/releases/operator/*`. None runs in a workflow.

## Research & Decisions

### Draft-first through release-please, not a second release tool

**Context**: Immutable releases forbid asset changes after publish. The release must be a draft while assets are attached.
**Explored**: release-please CHANGELOG 17.2.0 (`force-tag-creation`, upstream PR 2627) and `docs/manifest-releaser.md`; `src/github.ts` runs `git.createRef refs/tags/<tag>` at `release.sha` before `createRelease`; `src/manifest.ts` parses `draft` and `force-tag-creation` at the config root (line 1405) and per package (lines 1764-1765), merged per path, so the root placement the sandbox uses and the package placement here are equivalent. GitHub: a draft release has no tag of its own until published, which is why the eager tag is required (the downstream jobs check out `tag_name`).
**Decision**: Package `"."` MUST set `"draft": true` and `"force-tag-creation": true`. Nothing else in the config changes.
**Rationale**: Keeps the single release actor (the App via release-please, the only identity allowed to create a tag) and the existing outputs, and is the documented upstream answer to eager tag creation for drafts.

### Serialize release runs per branch

**Context**: release-please's only duplicate guard is `DuplicateReleaseError`, raised on HTTP 422 `already_exists` (`src/github-api.ts`), and the Release PR is relabelled `autorelease: tagged` only after `createRelease` returns (`src/manifest.ts` `createReleasesForPullRequest`). GitHub accepts several drafts with one `tag_name`. Two runs in quick succession (the Release PR merge, then another merge seconds later) can therefore both create a draft for the same tag, and both image jobs could probe `:vX.Y.Z` as absent and push.
**Decision**: `release.yml` MUST declare a workflow-level concurrency group:

```yaml
concurrency:
  group: release-${{ github.ref }}
  cancel-in-progress: false
```

A run on `main` and a run on `release/v1.0` do not share a group; they produce different tags. GitHub keeps at most one pending run per group and cancels an older pending one; that is safe because release-please derives its state from the branch head and the PR labels, so the newest pending run creates any release an older one would have. In addition, the release guard (next decision) MUST refuse to upload or publish unless exactly one release carries the tag.
**Rationale**: The group removes the race; the exactly-one check turns a residual duplicate (manual run, a future workflow) into a loud failure instead of an ambiguous by-tag lookup.

### One release guard script, every gh call names the repository

**Context**: The upload steps and the publish job all need the same checks, and must be testable on a laptop. `gh` needs a repository from git or `--repo`.
**Decision**: A new `.github/scripts/release-guard.sh` MUST implement two modes and require `GH_REPO` and `GH_TOKEN` in its environment:

```bash
#!/usr/bin/env bash
# release-guard.sh assert-draft|publish TAG
set -euo pipefail
mode=$1 tag=$2
: "${GH_REPO:?GH_REPO must name the repository}"
rels=$(gh api --paginate "repos/${GH_REPO}/releases?per_page=100" \
  --jq ".[] | select(.tag_name == \"${tag}\") | {id, draft, prerelease, assets: [.assets[].name]}" | jq -s .)
n=$(jq length <<<"$rels")
[ "$n" -eq 1 ] || { echo "::error::expected exactly one release for ${tag}, found ${n}"; exit 1; }
draft=$(jq -r '.[0].draft' <<<"$rels")
case $mode in
assert-draft)
  [ "$draft" = true ] || { echo "::error::release ${tag} is published; release the next version"; exit 1; } ;;
publish)
  [ "$draft" = true ] || { echo "release ${tag} already published, nothing to do"; exit 0; }
  for a in install.yaml opm-examples.tar.gz; do
    jq -e --arg a "$a" '.[0].assets | index($a) != null' <<<"$rels" >/dev/null \
      || { echo "::error::draft ${tag} lacks ${a}"; exit 1; }
  done
  id=$(jq -r '.[0].id' <<<"$rels")
  args=(-X PATCH "repos/${GH_REPO}/releases/${id}" -F draft=false)
  [ "${MAINTENANCE:-false}" = true ] && args+=(-f make_latest=false)
  gh api "${args[@]}" >/dev/null ;;
*) echo "::error::unknown mode ${mode}"; exit 2 ;;
esac
```

Publishing goes by release id, so it never depends on by-tag draft lookup. The prerelease flag is not sent, so release-please's Pre-release flag stays. Every upload step runs `release-guard.sh assert-draft "$TAG"` immediately before its `gh release upload "$TAG" --repo "$GH_REPO" ... --clobber`; every job that calls `gh` sets `GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}` and `GH_REPO: ${{ github.repository }}`.
**Rationale**: One script, one behavior, locally runnable against the live repository: `assert-draft v1.0.0-beta.2` fails (published), `publish v1.0.0-beta.2` exits 0 (already published path), `assert-draft v9.9.9` fails (zero releases).

### Assets upload only to a draft

**Decision**: The `install.yaml` upload in `image-release`, and the bundle upload and the manifests upload in `publish-examples`, MUST each be preceded in the same step by `release-guard.sh assert-draft`. `--clobber` stays: on a draft it makes "Re-run failed jobs" idempotent.
**Rationale**: Under immutable releases an upload to a published release fails anyway; the explicit check fails earlier with an actionable message and holds the guarantee while immutable releases are still off.

### One final publish job

**Decision**: A new job `publish-release` MUST `needs: [release-please, image-release, publish-examples]`, run only when `releases_created == 'true'`, hold `contents: write` and nothing else, and:

1. check out the tag with `sparse-checkout: .github/scripts` (the job needs only the guard script, at the release commit);
2. set `GH_TOKEN`, `GH_REPO: ${{ github.repository }}` and `MAINTENANCE: ${{ github.ref != 'refs/heads/main' }}`;
3. run `.github/scripts/release-guard.sh publish "$TAG"`.

**Rationale**: A single publish point means a partial run is never public. Re-runs replay the original workflow definition, so the job must be right on first ship; the local check against the live repo (task 3.3) is the substitute for a production rehearsal.

### Never overwrite a version image tag

**Context**: The rule: `:vX.Y.Z` always names the content first pushed under it (0021:D10:R3); a repeated push of the same content is a no-op, a different digest is refused. Multi-arch QEMU builds with unpinned bases are not byte-reproducible, so a rebuild on re-run yields a new digest: comparing a fresh build's digest with the existing one would make every legitimate re-run fail after a first attempt had pushed the tag, stranding the draft.
**Explored**: live probe of `:v1.0.0-beta.2` (Context).
**Decision**: Before building, `image-release` MUST run `.github/scripts/image-tag-guard.sh probe "$REF" "$(git rev-parse HEAD)"` (HEAD is the checked-out tag, the release commit):

```bash
# image-tag-guard.sh probe REF REV  -> state=absent | state=reuse + digest=<d>
# image-tag-guard.sh verify REF DIGEST
out=$(docker buildx imagetools inspect "$ref" --format '{{json .Manifest.Digest}}' 2>&1) || {
  case $out in *": not found"*) echo state=absent; exit 0 ;; esac
  echo "::error::probe of ${ref} failed: ${out}"; exit 1; }
digest=$(jq -er . <<<"$out")   # must be sha256:...
rev=$(docker buildx imagetools inspect "${ref%:*}@${digest}" \
  --format '{{ index (index .Image "linux/amd64").Config.Labels "org.opencontainers.image.revision" }}')
[ "$rev" = "$want" ] || { echo "::error::${ref} holds ${rev}, release commit is ${want}; release the next version"; exit 1; }
printf 'state=reuse\ndigest=%s\n' "$digest"
```

- `absent`: QEMU, build and push `:sha-<short>`, `:${TAG}` and (main only) `:latest`; then `image-tag-guard.sh verify` MUST confirm `:${TAG}` resolves to the pushed digest, failing otherwise.
- `reuse`: the version tag already holds the image this release built. Nothing is pushed under `:${TAG}`; the existing digest becomes the job's digest; `:sha-<short>` and (main only) `:latest` are re-pointed to it with `docker buildx imagetools create`, since they are mutable and a cancelled first attempt may have pushed the version tag without them. Signing, attestation and `install.yaml` then run against that digest.
- anything else (different revision, unreadable digest, any probe error other than not found): fail before building.

Every later reference to the digest goes through one step output (`steps.digest.outputs.digest`).
**Rationale**: The version tag is only ever written when absent, so it always names its first digest; the reuse path is the literal same-digest no-op. Identifying "this release's image" by revision label is the reproducibility-free way to tell a re-run from a foreign image.

### Image metadata from the checked-out tag

**Context**: `docker/metadata-action` defaults to `context: workflow`, taking the SHA from `github.sha`, the pushed commit. When release-please cuts a release on a later push than the Release PR merge, that is not the release commit.
**Explored**: metadata-action at the pinned SHA (v6.2.0) accepts `context: workflow|git`. Tag priorities: raw 200, sha 100, so `type=raw,value=${TAG}` declared first stays `version.main`, which feeds `org.opencontainers.image.version`.
**Decision**: The metadata step MUST set `context: git` and keep `type=sha,prefix=sha-,format=short`, `type=raw,value=${TAG}` and `type=raw,value=latest,enable=${{ github.ref == 'refs/heads/main' }}` in that order. The revision label and the `sha-<short>` tag then name the checked-out tag commit.
**Rationale**: Smaller than computing a short SHA by hand, and keeps the version label on the release tag.

### Release maintenance branches

**Context**: Owner branch model (2026-10-01): a released minor gets fixes from a lazily cut `release/vX.Y` branch, created from the newest `vX.Y.*` tag by one automated action that then opens a PR into the new branch setting branch-local release-please settings (`versioning: always-bump-patch`, `prerelease: false`). Backports land by PR. Branches are never deleted.
**Explored**: release-please-action v5.0.0 `action.yml` input `target-branch` ("detected by default", i.e. the repository default branch). The org reusable workflow `open-platform-model/.github/.github/workflows/cut-release-branch.yml` (written in parallel) takes `tag_prefix`, `minor` and the package path, creates `release/<tag_prefix><minor>` from the highest `<tag_prefix><minor>.*` tag and opens the settings PR, which also checks that the caller's release workflow trigger covers the branch.
**Decision**:

- `release.yml` `on.push.branches` MUST be `[main, 'release/**']`, and the release-please step MUST pass `target-branch: ${{ github.ref_name }}`.
- A release from a branch other than `main` MUST NOT move `:latest` (metadata `enable` above, and the reuse path) and MUST be published with `make_latest=false` (`MAINTENANCE` in the guard).
- A new `.github/workflows/cut-release-branch.yml` MUST be a thin `workflow_dispatch` caller:

```yaml
name: Cut release branch
on:
  workflow_dispatch:
    inputs:
      minor:
        description: Released minor to branch, as X.Y (for example 1.0)
        required: true
        type: string
permissions:
  contents: write
  pull-requests: write
jobs:
  cut:
    uses: open-platform-model/.github/.github/workflows/cut-release-branch.yml@<full commit SHA> # main, <date>
    with:
      tag_prefix: v
      minor: ${{ inputs.minor }}
      package: .
    secrets: inherit
```

Input names follow the reusable workflow as merged (G-reusable). Branch creation emits a push on the new branch; release-please then runs with main-era config, finds no commits after the tag and does nothing, until the settings PR merges.
**Rationale**: One cut path for all five repos; the caller only names this repo's tag prefix and package. Keeping `:latest` and the Latest release on `main` stops a patch to an old minor from becoming what `opm operator install` and `docker pull` resolve by default.

### Recovery rolls forward

**Decision**: Workflows MUST NOT contain `git tag`, a `git push` of a tag ref (`refs/tags/`, `--tags`, `--mirror`, a delete or force refspec), `gh release delete`, `gh release edit --tag`/`--target`, or a write to the git refs API. A failed run before publish is recovered with "Re-run failed jobs" on the same workflow run; a re-run of a failed `release-please` job itself creates nothing (the PR is already labelled tagged), so every check that can fail lives in a downstream job. A published release that is wrong is fixed by the next version (the next `1.0.0-beta.N`, or the next patch on a maintenance branch).

### G-sandbox: unknowns proven before section 1

Each item is proven in `open-platform-model/release-flow-sandbox`, and the evidence (run URL, observed output, and the token that acted) is written into this section in task 1.1. Section 1 does not start without it. Immutable releases MUST be on in the sandbox for U4 and U5, and the three org rulesets must cover it.

- U1: with `draft: true` + `force-tag-creation: true`, the action sets `releases_created`, `tag_name` and `sha` on the merge run, and the tag exists at `sha` when the job ends. Partly evidenced: sandbox run 36842600439 (GITHUB_TOKEN, before `tags-create-app-only`); repeat with the App.
- U2: the next Release PR anchors on the forced tag while the previous release is still a draft (no re-proposal of the drafted version, correct changelog window).
- U3: `gh release upload <tag> --repo ...` resolves a draft by tag with GITHUB_TOKEN.
- U4: PATCH `draft=false` by release id keeps the Pre-release flag and the forced tag, with all rulesets and immutable releases on.
- U5: an upload after publish with immutable releases on fails.
- U6: with `tags-create-app-only` on, release-please's `createRef` as the App succeeds, and the same call with GITHUB_TOKEN is refused. Must run as the App.
- U7: GitHub accepts a second draft release with an existing `tag_name`, and `release-guard.sh` then fails both modes with "found 2".
- U8: on a `release/vX.Y` branch with `always-bump-patch` and `prerelease: false`, release-please with `target-branch` proposes `vX.Y.(Z+1)`, the App tags the branch commit, `make_latest=false` leaves Latest on the `main` release, and the next Release PR on `main` is unaffected.

## Reconcile phase impact

None. Source, Render, Apply, Prune and Status are untouched: this change edits only release automation and docs.

## Risks / Trade-offs

- [The first draft-first release is the first production run] → G-sandbox proves the shape; the guard scripts are exercised locally against the live repo; a failure before publish leaves a draft (re-run); a failure after publish costs one beta number.
- [A draft left behind (job failed, nobody re-ran)] → the tag exists without a public release; the cli sees no release for it. Recovery: re-run failed jobs. The tag is never deleted. A workflow fix on `main` cannot reach the old run (re-runs replay the original definition); the version is then burned and the next release ships.
- [Reuse path trusts a label] → the label is written only by this workflow and the version tag is pushed only by it; anyone able to push a forged image to `ghcr.io/open-platform-model/opm-operator` already defeats the rule. Deliberate trade-off against byte-reproducible builds.
- [Partial tag push] → buildx writes each tag manifest separately, so a cancelled run can leave `:vX.Y.Z` without `:latest` or `:sha-<short>`. The reuse path re-points both mutable tags.
- [Pending run cancelled by the concurrency group] → safe: release-please is derived from branch state, and the newest run creates whatever an older pending one would have.
- [Reusable workflow interface drift] → the caller pins a full commit SHA of the merged reusable workflow (G-reusable), so a later change there cannot alter this repo's cut without a pin bump.
- [Spec drift] → `Release build architecture set` says exactly two platform descriptors, while buildx also adds `unknown/unknown` attestation entries; pre-existing and untouched here.
