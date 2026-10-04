## Context

See proposal.md, "Why". This change touches only release automation. No reconcile phase changes: Source, Render, Apply, Prune and Status behave the same. No API type or CRD changes.

What the repository has today, read 2026-10-04 at `origin/main` `40a2345`:

- **One release-please package.** `release-please-config.json` has the single package `"."`: release type `go`, `draft: true`, `force-tag-creation: true`, `versioning: prerelease`, beta, and `extra-files` for `internal/version/version.go` and the install page. At the top level it sets `"bump-minor-pre-major": false`, `"bump-patch-for-minor-pre-major": false` and `"include-component-in-tag": false`. `.release-please-manifest.json` is `{".": "1.0.0-beta.5"}`.
- **Every release job gates on `releases_created`.** `.github/workflows/release.yml` keys `image-release` (line 58), `publish-examples` (238), `publish-docs` (326) and `publish-release` (343) on `needs.release-please.outputs.releases_created == 'true'`, and reads `tag_name` (lines 35, 69 and others). release-please-action v5 (`release.yml:49`) sets `releases_created` when any package released. It sets `release_created` and `tag_name` for the root package and `<path>--release_created` and `<path>--tag_name` for any other package. With a second package, a module-only release would start `image-release` with an empty tag. The unmerged `join-release-cascade` adds `notify-downstream` with the same `if`.
- **The previous-tag lookup is unanchored.** `publish-examples` finds its previous tag with `git describe --tags --abbrev=0 "${TAG}^"` (`release.yml:290`). Once a module tag exists, it would return that tag and skip fixtures that changed since the last operator release.
- **The image job owns the install manifest.** `image-release` renders `dist/install.yaml` from `config/default` with the image digest (`release.yml:216-220`) and uploads it to the draft (224-231). `release-guard.sh publish` refuses a draft without `install.yaml` or `opm-examples.tar.gz` (`.github/scripts/release-guard.sh:15-18`).
- **G1 runs only on release PRs.** The `Lint` job runs `task deps:release-check` when the head ref starts with `release-please--` (`lint.yml:29-31`). That runs `hack/release-pin-check.sh`, which covers `go.mod` and the fixtures only.
- **Precedents.**
  - catalog_opm releases a module from a sub-path with `include-component-in-tag`, tags `opm-vX.Y.Z` and `separate-pull-requests: true`. Its release workflow pushes the identity-advance commit onto `release-please--branches--main--components--opm` with `opm catalog version set` (catalog_opm `.github/workflows/release.yml`, "Advance identity.Version on the release PR"). It publishes with `opm catalog publish ./src --version`, which asserts the version.
  - The image job's guard reuses a version tag only when its revision label names the release commit (`.github/scripts/image-tag-guard.sh`).
- **The resolver lists only v-tags.** The cascade resolver lists operator releases from `refs/tags/v*` and counts one as published when `install.yaml` downloads (`.github` `.github/scripts/cascade/lib/release.sh:19`, `lib/query.sh:88-90`).
- **The cli downloads the operator's manifest.** It fetches `releases/download/<operator tag>/install.yaml` for `opm operator install --version` (cli `internal/operator/fetch.go:13`) and for `task operator:sync` (cli `Taskfile.yml:497`).
- **The pinned cli has the commands this needs.** `.opm-cli-version` is `v1.0.0-beta.7`. That cli has `opm module version set` (exit 0 when set or already set) and `opm module publish --version`, which only asserts a declared version. Its first-party publish gate admits `opmodel.dev/modules/<leaf>` (cli `internal/publish/gates.go:23` at `v1.0.0-beta.7`). A second publish of a held version is refused with "<path> already holds <tag>" (cli `internal/publish/registry.go:56`).

**Interface with add-operator-module (assumed; G-dep confirms).** That change has no artifacts on disk yet. This design assumes:

- the module directory `module/`, whose `cue.mod` declares `opmodel.dev/modules/opm_operator@v0`;
- an `identity/identity.cue` package holding `Version`;
- one CUE file that holds the operator image tag and digest and nothing else, called the image file below;
- generated CRD and RBAC files;
- `task module:generate`, which regenerates them from a `config/` tree, with an override for the source directory;
- `task module:drift-check`.

Every name here is replaced by the real one in task G-dep.

## Goals / Non-Goals

**Goals:**

- Two release units in one repository that never trigger each other's release, except through the image-bump PR.
- The module published from its tagged tree byte for byte, signed and attested, with the install manifest beside it.
- Each step of the cascade (operator to module to cli) automated, and every version written by one writer.

**Non-Goals:**

- The cli's install, pins, G1 and docs pins, workspace `RELEASING.md`, the `.github` resolver and opmodel.dev reading the module bundle. Those are their own changes.
- Ownership transfer. Another session owns it.
- Release branches for either unit. None exist during beta (root `AGENTS.md`, "Release branches").
- An SBOM for the module (see "Signature and provenance").

## Decisions

### Tag shape `opm_operator-vX.Y.Z`

The module package sets `"component": "opm_operator"`, `"include-component-in-tag": true` and the default separator `-`. Tags are `opm_operator-v0.1.0`, and a future release branch would be `release/opm_operator-v0.1`.

Why this shape:

- An operator tag always starts with `v`, and an operator release branch with `release/v`, so neither can take this shape.
- The resolver's `refs/tags/v*` listing never sees module tags.
- It mirrors catalog_opm's `opm-vX.Y.Z`: the leaf of the module path, then the version.
- docs-kit reads the version with `prefix: "opm_operator-v"`.

**Alternatives:**

- `module-vX.Y.Z` does not name the module.
- `opm_operator/vX.Y.Z` (Go-style) reads as a Go submodule path, and this repository is a Go module.
- `opm-operator-module-vX.Y.Z` is long, and the leaf name is what users type.

### Separate release PRs and excluded paths

Top level: `separate-pull-requests: true`. Package `"."` gains `"exclude-paths": ["module"]`. Package `"module"` is release type `simple` with:

- `"component": "opm_operator"`, `"include-component-in-tag": true`, `"package-name": "opm_operator"`;
- `"bump-minor-pre-major": true` and `"bump-patch-for-minor-pre-major": true`, which override the top-level `false`;
- `"versioning": "default"`, `"prerelease": false`, `"initial-version": "0.1.0"`;
- `"draft": true`, `"force-tag-creation": true`;
- `"changelog-path": "CHANGELOG.md"` inside the module, so the changelog ships with it;
- `extra-files: [{type: generic, path: "RELEASE"}]`, catalog_opm's pattern, so `module/RELEASE` carries the version and `x-release-please-version`;
- the root package's changelog sections, `deps` included.

The two pre-major flags are exactly 0028:D1:R16: a breaking change in 0.x raises the minor, and `feat` raises the patch.

`exclude-paths` keeps the image-bump PR from cutting an operator release. Without it, an operator release would trigger an image PR, which would trigger another operator release, with no end.

Separate PRs give the module its own release PR, as 0028:D1 requires. The root package's release PR branch then becomes `release-please--branches--main--components--opm-operator` (U3).

**Alternative:** one combined release PR (the default). Not chosen: 0028:D1 asks for the module's own release PR, and one merge would release both units at once.

### Existing jobs key on the root package

`image-release`, `publish-examples`, `publish-docs` and `publish-release` (and `notify-downstream` if `join-release-cascade` has merged) change `if:` to `needs.release-please.outputs.release_created == 'true'`. They keep `tag_name`, which is the root package's. The `release-please` job exports `module_release_created: ${{ steps.release.outputs['module--release_created'] }}`, `module_tag_name` and `module_version`. `publish-examples` uses `git describe --tags --abbrev=0 --match 'v[0-9]*' "${TAG}^"`.

### Identity advance, the only writer of the module's version

A step after "Run release-please" in the `release-please` job does the following:

1. Install the opm CLI from `.opm-cli-version`, read at `main`, the same check as `release.yml:271-278`.
2. Fetch `release-please--branches--main--components--opm_operator`. If it does not exist, exit 0.
3. Read `VERSION=$(jq -r '.["module"]' .release-please-manifest.json)` from that branch. On the first release the manifest has no entry, so fall back to the version in the branch's `module/RELEASE` (U5).
4. Run `opm module version set "$VERSION" ./module`. If the diff is empty, exit 0.
5. Commit `chore: advance opm_operator identity.Version to ${VERSION}` and push to the branch with the App token checkout, so the release PR's CI runs.

The step never runs `opm module publish --version` to fill the version: the version is in the tagged tree before the tag exists, as 0028:D1:R10 requires. release-please writes only `CHANGELOG.md` and `RELEASE` in the module. That `RELEASE` file is release-please's own record and is not read as the identity, the same split catalog_opm uses.

### Module release gate inside `Lint`

The new `hack/module-release-check.sh` runs as `task module:release-check`. It runs in the `Lint` job when the head ref is the module's release branch, after G1. Running inside the existing required job matters: a skipped job reports as passing, a failing step does not (workspace `RELEASING.md`, "G1 placement"). The job keeps its name. The publish job runs the same script from the tag before pushing.

```bash
# module-release-check.sh <module-dir> <proposed-version>
img=$(cue export ./module/<image file> -e ref --out text)   # repo:tag@digest
[[ $img =~ ^ghcr\.io/open-platform-model/opm-operator:(v[^@]+)@(sha256:[0-9a-f]{64})$ ]] || bad ...
tag=${BASH_REMATCH[1]} want=${BASH_REMATCH[2]}
gh api repos/open-platform-model/opm-operator/releases/tags/$tag --jq '.draft' | grep -qx false || bad "$tag is not a published release"
got=$(.github/scripts/image-tag-guard.sh ... digest-of "ghcr.io/...:$tag")         # same imagetools call
[ "$got" = "$want" ] || bad "$tag: module names $want, GHCR serves $got"
grep -nE 'v: *"[^"]*-0\.dev\.' module/cue.mod/module.cue && bad ...
[ -e module/cue.mod/local-module.cue ] && bad ...
[ "$(cue export ./module/identity -e Version --out text)" = "$proposed" ] || bad ...
# v0 rules: major 0 on the v0 path; 1.0.0+ refused while $tag carries a prerelease suffix
git worktree add "$tmp" "$tag" && task module:drift-check SRC="$tmp/config" || bad ...
```

The drift check compares against the operator source at the deployed tag, not at HEAD, so the gate enforces 0028:D2:R1 and R2 for the version being released. The image tag guard gains a read-only `digest` mode, factored from its `manifest_digest`.

### Publish job

`module-publish` needs `release-please` and runs only when `module_release_created == 'true'`. It holds `contents: write`, `packages: write`, `id-token: write` and `attestations: write`. Steps:

1. Refuse unless `github.repository == 'open-platform-model/opm-operator'` and `MODULE_TAG` matches `^opm_operator-v[0-9]+\.[0-9]+\.[0-9]+$` (0028:D1:R5).
2. Check out the module tag with full history (the gate needs the operator tag).
3. Set up CUE and Go, install opm from `.opm-cli-version` at the tag, log in to GHCR.
4. Run `task module:release-check VERSION=<v>`.
5. Run `opm module publish ./module --version <v>`. If it refuses only with "already holds", resolve the held digest and compare it with a dry publish into a job-local registry (U7). Equal is a reuse; anything else fails.
6. Read the manifest digest of `ghcr.io/open-platform-model/modules/opm_operator:v<v>`.
7. Run `cosign sign --yes <ref>@<digest>`, then `actions/attest-build-provenance` with that subject name and digest and `push-to-registry: true`.
8. Run `opm module build opmodel.dev/modules/opm_operator --version <v> --name opm-operator -n opm-operator-system -f hack/module-defaults.cue > install.yaml`, with empty values (U6). Then run `hack/module-manifest-order.sh` to put the Namespace and CRDs first, if the render does not already.
9. Run `release-guard.sh assert-draft "$MODULE_TAG"`, then `gh release upload "$MODULE_TAG" install.yaml --repo "$GH_REPO" --clobber`.

`module-publish-release` needs `[release-please, module-publish]` and runs `release-guard.sh publish "$MODULE_TAG"`. `release-guard.sh` picks its required assets by tag shape: `install.yaml` for `opm_operator-v*`. For `v*`, it requires `install.yaml` and `opm-examples.tar.gz` until the last section, then only `opm-examples.tar.gz`.

`module-publish-docs` calls docs-kit `publish.yml` with `project: opm-operator-module`.

`module-notify` needs `module-publish-release` and calls `.github` `cascade-notify.yml` with the module tag.

### Signature and provenance

Like the image: a cosign keyless signature and a GitHub build provenance attestation pushed to the registry. 0028:D1:R4 names "signature and provenance attestation". The image also carries an SPDX SBOM attestation, but no SBOM generator reads CUE module dependencies. A hand-made SBOM listing core, the catalog and the Kubernetes schemas would duplicate `cue.mod/module.cue` without verifying anything. So the module carries no SBOM. Open question 2 asks whether R4's "same kinds" includes the SBOM. If it does, a syft-free SPDX document generated from `cue.mod/module.cue` is a small addition to step 7, with no other effect on the design.

### Image-bump PR

`module-image-pr` needs `[release-please, publish-release]` and runs only when `release_created == 'true'`. It mints the release App token, the same App as the release PR, so the PR's CI starts on its own. Then it runs `hack/module-image.sh set "$TAG"`:

```bash
# module-image.sh set <operator-tag>
release-guard.sh assert-published "$tag"        # new read-only mode: one release, draft == false
digest=$(image-tag-guard.sh digest "ghcr.io/open-platform-model/opm-operator:$tag")
prev=$(cue export ./module/<image file> -e tag --out text)
write <image file> tag=$tag digest=$digest        # the file holds only these two values
git worktree add "$tmp" "$tag"; task module:generate SRC="$tmp/config"
breaking=0
awk "/^## \[${tag#v}\]/,/^## \[${prev#v}\]/" CHANGELOG.md | grep -q '⚠ BREAKING CHANGES' && breaking=1
crd-served-versions-before vs after: a version dropped -> breaking=1
```

The workflow then checks `module/operator-image` out from `origin/main`. If the branch carries only commits by the App, it is rebuilt. If a human commit is on it, the workflow merges `origin/main` and adds a commit. It pushes with `--force-with-lease` and creates or edits the PR with the computed title. A `workflow_dispatch` input `tag` on a small `module-image.yml` workflow runs the same script for recovery.

The cascade receiver could have moved this pin as a shipped pin. That is not chosen: the cascade is still dry (Phase 3), and an operator release is this repository's own event. A dedicated job keeps 0028:D1:R10 working whether the cascade is live or not. It also keeps a library bump and an image bump out of one PR. Such a PR would release the operator and, in the same merge, a module naming the previous operator.

### Release-flow sandbox unknowns

Each unknown is proven in `open-platform-model/release-flow-sandbox` with this change's config shape, as the App, before section 2. The results are recorded under "G-sandbox evidence".

| # | Unknown | Pass condition |
| --- | --- | --- |
| U1 | A component-less root package `"."` beside a component package: release-please keeps finding the root's last release from `v*` tags, and the module's from `opm_operator-v*` | root proposes the next `beta.N`; module proposes `0.1.0`, then `0.1.1` |
| U2 | `exclude-paths: ["module"]` on `"."` | a module-only commit opens no root release PR |
| U3 | `separate-pull-requests: true` | both branch names as stated; an already open combined release PR is not merged into either (owner closes it) |
| U4 | per-package `bump-minor-pre-major` and `bump-patch-for-minor-pre-major` override the top-level `false` | `fix!` raises 0.y; `feat` raises 0.y.z |
| U5 | `initial-version: "0.1.0"` with no manifest entry, and the identity step's version source on the first PR | first PR proposes `0.1.0`; identity commit lands on that PR |
| U6 | `opm module build <published path> --version <v> -f <empty values>` renders the `#config` defaults, not `debugValues`, and orders Namespace and CRDs first | render matches the defaults render; order checked |
| U7 | re-run of `opm module publish` on a held version: identify "same content" (digest of a dry publish to a local registry equals the held digest) | equal on a true re-run, unequal after a source change |
| U8 | `cosign sign` and `attest-build-provenance` on a CUE module OCI artifact in GHCR; `opm` and `cue mod` resolution ignore the `sha256-*.sig`/`.att` tags; the resolver's GHCR tag listing ignores them | `cosign verify` and `gh attestation verify` pass; `opm module build` resolves the version; the resolver reports the right newest |
| U9 | a published non-prerelease module release becomes GitHub's Latest while operator releases stay Pre-release | Latest is the module release |

If any unknown fails, the change stops at section 1. design.md and the spec deltas are revised before section 2.

### Order with the cli (the last section)

0028:D2:R13 removes the operator release's `install.yaml`. Today the cli downloads that asset when a user selects an operator version (`cli/internal/operator/fetch.go:13`), and in `task operator:sync` (`cli/Taskfile.yml:497`). The resolver also counts an operator release as published only when the asset downloads (`.github` `lib/query.sh:88-90`). Removing the asset first would break `opm operator install --version` for every released cli. It would also stop the cli's cascade from ever seeing a new operator release. So the last section's gate G-cli requires:

- a released cli that installs from the module (0028:D3);
- a cli `deps:cascade` that no longer resolves the operator with `--asset install.yaml`;
- a cli G1 that keys on the module pin (0028:D7).

The operator's own `notify-downstream` then stops dispatching to the cli. Per 0028:D7, the operator release notifies the module release, which here is the image-bump PR, and the module notifies the cli.

## Risks / Trade-offs

- [The release-please multi-package behaviour differs from the docs] → U1 to U5 are proven in the sandbox before any config lands. A failure stops the change at section 1.
- [A module release merged before the operator it names has been published] → The gate refuses a draft operator tag on the release PR. The publish job reruns the gate before pushing. A stranded module tag rolls forward to the next version, as for every tag.
- [A CRD change on `main` blocks module releases until the next operator release] → This is intended (0028:D2:R1). The gate names the CRD. The image-bump PR of the next operator release regenerates from that tag and unblocks it. A module-only fix in that window waits for the next operator release, or for a revert of the CRD change in the module's generated files.
- [The App token's PR on `module/operator-image` rewrites a branch] → It rewrites only while every commit on the branch is the App's, and once a human commits it only adds commits. The lease refuses if the branch moved since it was fetched.
- [Two release PRs confuse the owner] → `AGENTS.md` names both. The module's release PR also carries the identity-advance commit, so wait for it before merging, as in catalog_opm.
- [GHCR package created with the wrong link or visibility on first publish] → G-owner runs after the first module release: link the package to this repository only, give Actions access only to it, and make it public. Until that is done, `opm` cannot pull it anonymously, so the cli's work waits.
- [Module releases become GitHub's Latest] → The kubectl path `releases/latest/download/install.yaml` then serves the module's manifest, which is the intended manifest after 0028:D2:R13. Before the last section, the install page still names the operator release's asset. After operator GA both units could be Latest. Open question 3.

## Migration Plan

Section by section, one PR each, under Delivery mode:

- Sections 1 to 3 change only CI. Merging section 2 opens the module's first release PR. The owner merges it after G-owner (approval of the tag shape and the first release). Afterwards the owner applies the GHCR package settings.
- Section 4 adds the docs bundle and the cascade edges.
- Section 5 waits for G-cli.

Rollback before section 5: revert the section's PR. Tags and published module versions stay, as for every release. Rollback after section 5: revert it and the operator release attaches `install.yaml` again from the next release on.

## Open Questions

1. Does the last section's removal of `install.yaml` from operator releases need a `!` (`ci(release)!:` cuts an operator `beta.N`, and the CHANGELOG then announces it)? The specs hold either way. Only the commit title changes. Ask the owner at that section's PR.
2. Does 0028:D1:R4's "same kinds of signature and provenance" include the image's SBOM attestation? This design reads R4's text as signature plus provenance.
3. After operator GA, which unit should be GitHub's Latest release? That waits for GA.

## Research & Decisions

### release-please outputs with two packages
**Context**: Every release job reads `releases_created`, which a second package also sets.
**Explored**: release-please-action v5 `outputReleases` (root outputs unprefixed, other paths `<path>--<key>`), `release.yml:34-35,58,238,326,343`, `join-release-cascade` notify job.
**Decision**: Root jobs use `release_created`; module jobs use `module--release_created`, exported as `module_release_created`.
**Rationale**: Without it, a module-only release starts the image job with an empty tag.

### Where the module's version is written
**Context**: 0028:D1:R10 forbids anything added at publish time; `opm module publish --version` can fill an open version.
**Explored**: catalog_opm's identity-advance step and its `RELEASE` extra file; cli `module version set` and `publish --version` help (`v1.0.0-beta.7`).
**Decision**: Release-workflow identity advance on the module's release PR; `--version` only asserts.
**Rationale**: One writer, the version is in the tagged tree, and the precedent is proven in production.

### How the image reference moves
**Context**: 0028:D1:R10 and D7 need the module to follow each operator release.
**Explored**: the cascade receiver (`join-release-cascade`, dry until Phase 4), a release.yml job, Renovate-style digest pinning.
**Decision**: A release.yml job after `publish-release` plus a dispatch workflow, both calling `hack/module-image.sh`.
**Rationale**: Independent of cascade phase, one PR per operator release, regenerates CRDs from the same tag.

## G-sandbox evidence

(Filled by task 1.1.)
