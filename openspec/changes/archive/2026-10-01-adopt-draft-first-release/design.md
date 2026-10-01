## Context

See proposal.md (Why). State on `main` at planning time (2026-10-01, `27d9dfc`, `1.0.0-beta.2`):

- `release.yml` runs on pushes to `main` only and has three jobs. `release-please` (App token, action v5.0.0, which bundles release-please 17.6.0) exports `releases_created` and `tag_name`. `image-release` checks out the tag, builds `linux/amd64,linux/arm64`, pushes `:sha-<short>`, `:<tag>` and `:latest`, signs, attests and uploads `dist/install.yaml` with `gh release upload --clobber`. `publish-examples` (needs `image-release`) publishes the fixture modules through `opm module publish` (already refusal-tolerant, never overwrites) and uploads `opm-examples.tar.gz` and `dist/examples/*.yaml` with `--clobber`. There is no concurrency group.
- release-please creates a published (non-draft) release flagged Pre-release, so assets land after publish.
- Verified on 2026-10-01 against GHCR: tag `v1.0.0-beta.2` is lightweight at `27d9dfc3c037`; the `:v1.0.0-beta.2` index digest is `sha256:a130b542...`; its `linux/amd64` config label `org.opencontainers.image.revision` is `27d9dfc3c03777f218d69b3bbac435cc06b52af5` and `org.opencontainers.image.version` is `v1.0.0-beta.2`. `docker buildx imagetools inspect <ref> --format '{{json .Manifest.Digest}}'` and `--format '{{ (index .Image "linux/amd64").Config.Labels }}'` both work; a missing tag prints `ERROR: <ref>: not found`.
- `gh release view v1.0.0-beta.2 --json isDraft` outside a git checkout fails with `failed to run git: fatal: not a git repository`; with `--repo` or `GH_REPO` it works. Every `gh` call in this design therefore names the repository.

Platform state (owner-managed, outside the repo). Verified read-only on 2026-10-01 with `gh api repos/open-platform-model/opm-operator/immutable-releases` and `gh api 'repos/open-platform-model/opm-operator/rulesets?includes_parents=true'`; tasks.md "Gates" re-checks each item before the step it blocks:

- `tags-immutable`: ACTIVE. Update, deletion and non_fast_forward refused on every tag, empty bypass (0021:D10:R6).
- `tags-create-app-only`: NOT ACTIVE yet. Once active, tag creation is refused for everyone except the opm-release-please App (0021:D10:R7), so a tag here is always one release-please created at its release commit and a stale or hand-made tag cannot exist. That is why this change drops the tag-commit assertion of the first draft of this plan, and why G-platform blocks the merge until the ruleset is active. Without it, release-please's `force-tag-creation` path swallows the 422 from `createRef` on an existing tag and drafts a release on it, so a tag pushed early or by hand at the wrong commit would ship.
- `release-branches`: NOT ACTIVE yet; not a precondition. No `release/*` branch exists in this repo, and this change adds no support for one (Phase 2).
- `docs-branches-pinned`: still ACTIVE although retired by the owner's revision; it already pins this repo's `docs/archive-operator-platform-status-operator-version` and `docs/claude-coauthorship-trailer` topic branches. Irrelevant to the release flow; its removal is an owner action.
- Immutable releases: ON for opm-operator (`enabled: true, enforced_by_owner: true`), ahead of plan and against 0021:D10:R8. `v1.0.0-beta.2` (published 2026-09-30) predates the switch. With today's `release.yml` the next release is published by release-please and every later `gh release upload` is refused, leaving a public, asset-less, unrepairable release. G-owner therefore requires the owner to turn it OFF before any releasable merge to `main`; it goes back ON only after the first draft-first release has shipped.

Re-verified read-only on 2026-10-01T12:48Z, before section 1 (same two calls plus `gh api repos/open-platform-model/opm-operator/rulesets/<id>`): `tags-create-app-only` (24306688) is now ACTIVE, rules `[creation]`, bypass exactly `[Integration 5132303 always]`; `tags-immutable` (24307318) ACTIVE, rules `[update, deletion, non_fast_forward]`, bypass `[]`; `release-branches` (24306642) ACTIVE, rules `[deletion, non_fast_forward, pull_request]`, bypass `[]`; immutable releases OFF (`{"enabled":false,"enforced_by_owner":false}`). The bullets above record the planning-time state the gates were written against.

## Goals / Non-Goals

**Goals:**

- A GitHub Release becomes public only after every asset is attached, so the flow survives immutable releases.
- No step in this repo's workflows can move, delete or re-create a git tag, or make `:vX.Y.Z` on GHCR name content other than what was first pushed under it (0021:D10:R1, 0021:D10:R3).
- At most one release run at a time, and at most one release per tag.
- A failed release run is recovered by re-running failed jobs on the draft, never by touching the tag.

**Non-Goals:**

- No controller, API, CRD or reconcile change.
- No change to the PR image workflow (`:pr-N`, `:sha-<short>` stay mutable), to `publish-fixtures.yml`, or to the e2e fixture tags (`-e2e.g*`).
- No release-branch support (Phase 2, before GA): `release.yml` keeps triggering on `main` only, release-please keeps its default target branch, there is no cut workflow, and `:latest` and the Latest release keep today's behavior. See "Phase 2 inputs".
- No org setting, ruleset or immutable-release toggle: owner actions.
- No reproducible-build work: the image guard does not need byte-identical rebuilds (see "Never overwrite a version image tag").
- Known gaps left for an owner decision, not guarded here: the human-run push paths `.tasks/docker.yaml` (`buildx build --push --tag {{.IMG}}`) and `Makefile` (`docker-buildx`) overwrite `:vX.Y.Z` if `IMG` names one, and `.tasks/release.yaml` `publish:ghcr` re-pushes the floating `v0.0.1` flux artifact tag under `testing.opmodel.dev/releases/operator/*`. None runs in a workflow.

## Research & Decisions

### Draft-first through release-please, not a second release tool

**Context**: Immutable releases forbid asset changes after publish. The release must be a draft while assets are attached.
**Explored**: release-please CHANGELOG 17.2.0 (`force-tag-creation`, upstream PR 2627) and `docs/manifest-releaser.md`; `src/github.ts` runs `git.createRef refs/tags/<tag>` at `release.sha` before `createRelease`; `src/manifest.ts` parses `draft` and `force-tag-creation` at the config root (line 1405) and per package (lines 1764-1765), merged per path, so the root placement the sandbox uses and the package placement here are equivalent. GitHub: a draft release has no tag of its own until published, which is why the eager tag is required (the downstream jobs check out `tag_name`).
**Decision**: Package `"."` MUST set `"draft": true` and `"force-tag-creation": true`. Nothing else in the config changes.
**Rationale**: Keeps the single release actor (the App via release-please, the only identity allowed to create a tag) and the existing outputs, and is the documented upstream answer to eager tag creation for drafts.

### Serialize release runs

**Context**: release-please's only duplicate guard is `DuplicateReleaseError`, raised on HTTP 422 `already_exists` (`src/github-api.ts`), and the Release PR is relabelled `autorelease: tagged` only after `createRelease` returns (`src/manifest.ts` `createReleasesForPullRequest`). GitHub accepts several drafts with one `tag_name`. Two runs in quick succession (the Release PR merge, then another merge seconds later) can therefore both create a draft for the same tag, and both image jobs could probe `:vX.Y.Z` as absent and push.
**Decision**: `release.yml` MUST declare a workflow-level concurrency group:

```yaml
concurrency:
  group: release-${{ github.ref }}
  cancel-in-progress: false
```

The workflow runs on `main` only, so this is one group; keying it by ref keeps Phase 2's branch runs in their own groups without an edit. GitHub keeps at most one pending run per group and cancels an older pending one; that is safe because release-please derives its state from the branch head and the PR labels, so the newest pending run creates any release an older one would have. In addition, the release guard (next decision) MUST refuse to upload or publish unless exactly one release carries the tag.
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
  gh api -X PATCH "repos/${GH_REPO}/releases/${id}" -F draft=false >/dev/null ;;
*) echo "::error::unknown mode ${mode}"; exit 2 ;;
esac
```

Publishing goes by release id, so it never depends on by-tag draft lookup. Neither the prerelease flag nor `make_latest` is sent, so release-please's Pre-release flag and GitHub's default Latest selection stay as today. Every upload step runs `release-guard.sh assert-draft "$TAG"` immediately before its `gh release upload "$TAG" --repo "$GH_REPO" ... --clobber`; every job that calls `gh` sets `GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}` and `GH_REPO: ${{ github.repository }}`.
**Rationale**: One script, one behavior, locally runnable against the live repository: `assert-draft v1.0.0-beta.2` fails (published), `publish v1.0.0-beta.2` exits 0 (already published path), `assert-draft v9.9.9` fails (zero releases).

### Assets upload only to a draft

**Decision**: The `install.yaml` upload in `image-release`, and the bundle upload and the manifests upload in `publish-examples`, MUST each be preceded in the same step by `release-guard.sh assert-draft`. `--clobber` stays: on a draft it makes "Re-run failed jobs" idempotent.
**Rationale**: Under immutable releases an upload to a published release fails anyway; the explicit check fails earlier with an actionable message and holds the guarantee while immutable releases are still off.

### One final publish job

**Decision**: A new job `publish-release` MUST `needs: [release-please, image-release, publish-examples]`, run only when `releases_created == 'true'`, hold `contents: write` and nothing else, and:

1. check out the tag with `sparse-checkout: .github/scripts` (the job needs only the guard script, at the release commit);
2. set `GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}`, `GH_REPO: ${{ github.repository }}` and `TAG: ${{ needs.release-please.outputs.tag_name }}`;
3. run `.github/scripts/release-guard.sh publish "$TAG"`.

**Rationale**: A single publish point means a partial run is never public. Re-runs replay the original workflow definition, so the job must be right on first ship; the local check against the live repo (task 3.2) is the substitute for a production rehearsal.

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

- `absent`: QEMU, build and push `:sha-<short>`, `:${TAG}` and `:latest`; then `image-tag-guard.sh verify` MUST confirm `:${TAG}` resolves to the pushed digest, failing otherwise.
- `reuse`: the version tag already holds the image this release built. Nothing is pushed under `:${TAG}`; the existing digest becomes the job's digest; `:sha-<short>` and `:latest` are re-pointed to it with `docker buildx imagetools create`, since they are mutable and a cancelled first attempt may have pushed the version tag without them. Signing, attestation and `install.yaml` then run against that digest.
- anything else (different revision, unreadable digest, any probe error other than not found): fail before building.

Every later reference to the digest goes through one step output (`steps.digest.outputs.digest`).
**Rationale**: The version tag is only ever written when absent, so it always names its first digest; the reuse path is the literal same-digest no-op. Identifying "this release's image" by revision label is the reproducibility-free way to tell a re-run from a foreign image.

### Image metadata from the checked-out tag

**Context**: `docker/metadata-action` defaults to `context: workflow`, taking the SHA from `github.sha`, the pushed commit. When release-please cuts a release on a later push than the Release PR merge, that is not the release commit.
**Explored**: metadata-action at the pinned SHA (v6.2.0) accepts `context: workflow|git`. Tag priorities: raw 200, sha 100, so `type=raw,value=${TAG}` declared first stays `version.main`, which feeds `org.opencontainers.image.version`.
**Decision**: The metadata step MUST set `context: git` and keep `type=sha,prefix=sha-,format=short`, `type=raw,value=${TAG}` and `type=raw,value=latest` in that order (today's list; no branch gating in Phase 1). The revision label and the `sha-<short>` tag then name the checked-out tag commit.
**Rationale**: Smaller than computing a short SHA by hand, and keeps the version label on the release tag.

### Recovery rolls forward

**Decision**: Workflows MUST NOT contain `git tag`, a `git push` of a tag ref (`refs/tags/`, `--tags`, `--mirror`, a delete or force refspec), `gh release delete`, `gh release edit --tag`/`--target`, or a write to the git refs API. A failed run before publish is recovered with "Re-run failed jobs" on the same workflow run, never "Re-run all jobs": that re-runs `release-please`, which finds the Release PR already labelled `autorelease: tagged`, reports no release, skips every downstream job and ends green with the draft stranded and no failed job left to re-run. If that happens anyway, the version is burned: the stranded draft stays (or the owner deletes it in the UI, never the tag) and the next release ships. Whether release-please would re-propose the burned version after such a deletion is unproven, so recovery never relies on it. Every check that can fail lives in a downstream job, so the `release-please` job normally succeeds once and is never re-run. A published release that is wrong is fixed by the next version (the next `1.0.0-beta.N`; after GA the next patch).

**Duplicate draft.** One path still yields two drafts for a tag: the `release-please` job fails after `createRelease` but before it relabels the Release PR `autorelease: tagged`, and is re-run; `createRef` then fails with 422 (tag exists, swallowed by force-tag-creation) and `createRelease` adds a second draft. From then on `release-guard.sh` fails every upload and the publish with "found 2". Recovery: the owner deletes the extra draft (the one with no assets, or the newer one) in the GitHub UI, never the tag; agents cannot, since the workspace hook blocks `gh release delete`. Then "Re-run failed jobs". ADR-018 records this procedure.

### G-sandbox: unknowns proven before section 1

Each item is proven in `open-platform-model/release-flow-sandbox`, and the evidence (run URL, observed output, and the token that acted) is written into this section in task 1.1. Section 1 does not start without it. Immutable releases MUST be on in the sandbox for U4 and U5, and `tags-immutable` and `tags-create-app-only` must cover it (U6 needs the latter). Both preconditions held at test time (below), so G-sandbox is met.

- U1: with `draft: true` + `force-tag-creation: true`, the action sets `releases_created`, `tag_name` and `sha` on the merge run, and the tag exists at `sha` when the job ends. Partly evidenced: sandbox run 36842600439 (GITHUB_TOKEN, before `tags-create-app-only`); repeat with the App.
- U2: the next Release PR anchors on the forced tag while the previous release is still a draft (no re-proposal of the drafted version, correct changelog window).
- U3: `gh release upload <tag> --repo ...` resolves a draft by tag with GITHUB_TOKEN.
- U4: PATCH `draft=false` by release id keeps the Pre-release flag and the forced tag, with both tag rulesets and immutable releases on.
- U5: an upload after publish with immutable releases on fails.
- U6: with `tags-create-app-only` on, release-please's `createRef` as the App succeeds, and the same call with GITHUB_TOKEN is refused. Must run as the App.
- U7: GitHub accepts a second draft release with an existing `tag_name`, and `release-guard.sh` then fails both modes with "found 2"; deleting the extra draft in the UI and re-running failed jobs then publishes the remaining one with its tag unchanged.

**Evidence (sandbox phase 2, 2026-10-01).** Repository `open-platform-model/release-flow-sandbox`, PR 6 (merged as `09fd181`) carries this change's shape: release-please config with `draft: true` and `force-tag-creation: true`; a `release-please` job that mints the App token with `create-github-app-token` v3.2.0 and passes only that token (no GITHUB_TOKEN fallback); `image-release` and `publish-examples` upload through `release-guard.sh assert-draft` then `gh release upload ... --repo "$GH_REPO" --clobber`; `publish-release` needs all three jobs, holds `contents: write` only, sparse-checks out `.github/scripts` at the tag and runs `release-guard.sh publish "$TAG"`; `release-guard.sh` is verbatim from "One release guard script". Versions: release-please-action v5.0.0 (`45996ed`). Run URLs are `https://github.com/open-platform-model/release-flow-sandbox/actions/runs/<id>`.

Preconditions at test time: `tags-immutable` (24307318) active, bypass `[]`; `tags-create-app-only` (24306688) active, rule `creation`, bypass `Integration 5132303 always`; `release-branches` (24306642) active; immutable releases `{"enabled":true,"enforced_by_owner":true}`.

| Item | Run | Acting token | Observed |
| --- | --- | --- | --- |
| U1 | 36863184964 attempt 1 (also 36862828370, 36863653282) | App (release-please) | `releases_created=true`, `tag_name=v1.0.0-beta.4`, `sha=2234f355...`, `draft=true`; the observe step after release-please read `refs/tags/v1.0.0-beta.4` at `2234f355...` (lightweight) while the release was a draft. |
| U2 | release PRs 12 and 14 | App | PR 12 opened while `v1.0.0-beta.4` was a draft proposes `v1.0.0-beta.5` with only the new fix and a compare link from `v1.0.0-beta.4`; PR 14, opened while `v1.0.0-beta.5` was a draft, proposes `v1.0.0-beta.6` and is unchanged after `v1.0.0-beta.5` was published. No re-proposal of a drafted version. |
| U3 | 36863184964 attempts 1 and 3 | GITHUB_TOKEN | `gh release upload v1.0.0-beta.4 dist/install.yaml --repo ... --clobber` resolved the draft by tag; in attempt 3 `--clobber` replaced the existing `install.yaml` on the draft with immutable releases on. |
| U4 | 36863184964 attempt 3, job `publish-release` | GITHUB_TOKEN | PATCH `draft=false` by id 400954633; result `draft: false`, `prerelease: true`, `immutable: true`, assets `a.yaml, b.yaml, install.yaml, opm-examples.tar.gz`, tag unchanged at `2234f35`. `GET releases/latest` stayed 404 (prereleases only). |
| U5 | local, after publish | user token | new asset: `HTTP 422: Cannot upload assets to an immutable release.`; with `--clobber`: `HTTP 422: Validation Failed ... Cannot delete asset from an immutable release`, asset list unchanged. `release-guard.sh assert-draft v1.0.0-beta.4` exits 1 with `release v1.0.0-beta.4 is published; release the next version`; `publish` exits 0 with `already published, nothing to do`. |
| U6 | 36862651882 (`sandbox-probe.yml`) | App, then GITHUB_TOKEN | App `createRef refs/tags/probe-orphan-1` succeeded; GITHUB_TOKEN got `{"message":"Reference update failed","status":"422"}`. A user-token create is refused too (API: `422 Reference update failed`; `git push`: `GH013 ... creations being restricted`); move and delete are refused by `tags-immutable`. Every release tag in the run set was created by release-please as the App. |
| U7 | 36863184964 attempts 2 and 3 | App (attempt 2 release-please), GITHUB_TOKEN (guards) | With the release PR relabelled `autorelease: pending` and only `Release Please` re-run, release-please swallowed the 422 on `createRef` and created a second draft 400956088 for `v1.0.0-beta.4`; `image-release` failed with `expected exactly one release for v1.0.0-beta.4, found 2` (both modes fail the same way when run locally). The extra empty draft was deleted (API as the owner's account, standing in for the UI); the tag stayed at `2234f35`; "Re-run failed jobs" (attempt 3) re-ran `image-release`, `publish-examples` and `publish-release` and published the one remaining release with all four assets. The re-run read the current repository variables. |

Deviations adopted from the sandbox: none required; `release-guard.sh` (verbatim), the guarded `--clobber` uploads, publish by id with sparse checkout and the release concurrency group ran as written. This change keeps the unconditional `release-${{ github.ref }}` group (the sandbox keyed its group on a sandbox-only variable to keep two shapes apart). release-please swallows every 422 on `createRef`, not only "already exists": with a non-App token it would create a draft with no tag and `image-release` would fail at the tag checkout, so the App-only token (no GITHUB_TOKEN fallback) stays a hard rule. Not exercised in the sandbox: the image build, push, signing, attestation and `image-tag-guard.sh` (no image is built); those rest on the local checks against GHCR in task 2.2.

## Phase 2 inputs (not in this change)

Release-branch support lands before GA in a separate change, after the org cut action is proven in `release-flow-sandbox` (including a cut from a tag made before this change and the main-versus-branch collision case). Findings from the review of the first draft of this plan that the Phase 2 change must carry:

- **Version-line rule.** `release/vX.Y` is cut only when `main`'s next release is `X.(Y+1).0` or higher; after the cut, `main` never releases an `X.Y.*` version (0021:D10:R9). The operator's Release PR on `main` must not be allowed to propose one.
- **Reusable interface.** The org `cut-release-branch.yml` (open-platform-model/.github, commit 72f7d5d at review time) takes `package_path`, `release_app_client_id` (an input) and the secret `release_app_private_key`. A caller passing `package` and `secrets: inherit` fails at token selection. The caller must use the merged names, pass `release_app_client_id: ${{ vars.RELEASE_APP_CLIENT_ID }}` and the key secret explicitly, and set `permissions: {}`.
- **The reusable rewrites `release.yml`, it does not check it.** Its edit is not idempotent on a file that already carries `release/**` and `target-branch` (duplicate key, then a fallback rewrite that strips blank lines). Fix in the reusable: no-op when both edits are present.
- **App lacks `workflows` permission.** `opm-release-please` holds contents, issues and pull_requests write only, so a push changing `.github/workflows/*` is refused. The reusable creates `release/vX.Y` before pushing the setup branch, so a refused push leaves an undeletable branch that blocks a re-cut. Fix in the reusable: push the setup branch before `createRef`. Granting `workflows: write` is an owner decision.
- **Gate.** Proving input names is not enough: require an end-to-end cut in the sandbox run as the App, where the setup PR leaves an already-edited `release.yml` byte-identical.
- **Latest and `:latest`.** 0021:D10:R3 decides this: a release from a `release/*` branch never moves `:latest` or GitHub's Latest flag; both stay with `main`'s line. Phase 2 must make the branch release send `make_latest=false` and skip `:latest`. A SemVer-based policy (move both only for the highest final version) would need a D10 amendment first. The cli pins the operator tag, so "a cli resolving latest" is not a reason either way.
- **Patch-only bumps in the spec.** "Version bump determination per release line" says a `feat` proposes a minor; a branch requirement must state that every releasable commit on `release/*`, `feat` included, proposes a patch, with a scenario (`feat` on `release/v1.0` at 1.0.4 proposes 1.0.5), citing 0021:D10:R5.
- **Sandbox item (former U8).** On `release/vX.Y` with `always-bump-patch` and `prerelease: false`, release-please with `target-branch` proposes `vX.Y.(Z+1)`, the App tags the branch commit, neither Latest nor `:latest` moves (R3), and the next Release PR on `main` is unaffected.
- **Hook gap (workspace item).** `gh api -X POST .../git/refs -f ref=refs/heads/release/...` passes the agent hook because the ref is in the body; under the release-branches ruleset a mistaken branch squats the name permanently.

## Reconcile phase impact

None. Source, Render, Apply, Prune and Status are untouched: this change edits only release automation and docs.

## Risks / Trade-offs

- [Immutable releases already ON (2026-10-01)] → a releasable merge before the owner turns it OFF publishes a release that cannot receive `install.yaml` or the examples, and the version is burned. G-owner holds every releasable merge until `immutable-releases` reports `enabled: false`.
- [`tags-create-app-only` not active] → a hand-made or early tag would be adopted by the draft. G-platform holds the merge until the ruleset is active.
- [The first draft-first release is the first production run] → G-sandbox proves the shape; the guard scripts are exercised locally against the live repo; a failure before publish leaves a draft (re-run); a failure after publish costs one beta number.
- [A draft left behind (job failed, nobody re-ran)] → the tag exists without a public release; the cli sees no release for it. Recovery: re-run failed jobs. The tag is never deleted. A workflow fix on `main` cannot reach the old run (re-runs replay the original definition); the version is then burned and the next release ships.
- [Reuse path trusts a label] → the label is written only by this workflow and the version tag is pushed only by it; anyone able to push a forged image to `ghcr.io/open-platform-model/opm-operator` already defeats the rule. Deliberate trade-off against byte-reproducible builds.
- [Partial tag push] → buildx writes each tag manifest separately, so a cancelled run can leave `:vX.Y.Z` without `:latest` or `:sha-<short>`. The reuse path re-points both mutable tags.
- [Pending run cancelled by the concurrency group] → safe: release-please is derived from branch state, and the newest run creates whatever an older pending one would have.
- [Spec drift] → `Release build architecture set` says exactly two platform descriptors, while buildx also adds `unknown/unknown` attestation entries; pre-existing and untouched here.
