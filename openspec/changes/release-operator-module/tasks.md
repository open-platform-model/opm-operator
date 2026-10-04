# Tasks: release-operator-module

Worktree `opm-operator/.claude/worktrees/release-operator-module`, branch `feat/release-operator-module`. Run every command inside that worktree.

Delivery is two PRs (proposal.md, revised 2026-10-04): this change's PR carries sections 1, 2, 4 and 5 and the archive; the carrier (section 3) is its own PR, opened only once the first is on `main`. The branch is never rebased once pushed: bring `origin/main` in with a merge commit.

Every section's commit task runs these checks:

- workflow lint: `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12` over `.github/workflows/`, with `shellcheck` on PATH;
- `shellcheck` over every script this change adds or edits;
- the validation gates `task dev:fmt dev:vet dev:lint dev:test`.

The local kind cluster `opm-dev` and the host's containers have no internet egress. Cluster e2e is proven only in CI, and each PR body says so.

## Gates

The supervisor ticks each gate after confirming it with the read-only check given. A worker never ticks these.

- [ ] G-dep **GATED: after add-operator-module is merged.** Check: `gh pr list --repo open-platform-model/opm-operator --state merged --search "head:feat/add-operator-module"` returns the PR, and `git ls-tree -r --name-only origin/main` lists `modules/opm_operator/cue.mod/module.cue`, `identity/identity.cue`, `operator/operator.cue`, `zz_generated_crds.cue` and `zz_generated_rbac.cue` under it. Check that `hack/operator-module/min-operator-version` is on `origin/main` and names a published operator release containing opm-operator PR #211 (`refuse-own-instance`): `git merge-base --is-ancestor <PR #211 squash commit> <that tag>` succeeds; if not, stop and report to the supervisor. Compare every name in design.md "Interface with add-operator-module" with the merged tree, including the fields of `operator/operator.cue`, the generate and drift-check scripts and tasks, the drift check's `--ref` mode, and that the per-PR drift check compares with `config/` at `HEAD`. Record the result in that section and substitute any name that changed everywhere in this change. If the drift check cannot compare against an operator tag, stop and report to the supervisor: the release gate needs a check against the deployed operator release, not `HEAD`. Blocks section 1.
- [ ] G-sandbox Unknowns U1 to U8 (design.md, "Release-flow sandbox unknowns") are proven in `open-platform-model/release-flow-sandbox` with this change's config and job shape, U1 to U5 and U8 run as the App. Preconditions: `gh api repos/open-platform-model/release-flow-sandbox/immutable-releases` returns `"enabled":true`, and the sandbox's `includes_parents` ruleset listing shows `tags-immutable` and `tags-create-app-only` active. Record run URLs, outputs and the acting token for each item. Blocks section 2.
- [ ] G-owner (a) The owner approves the tag shape `opm_operator-vX.Y.Z`, the `0.1.0` start and the 0.x bump rule (design.md, open question 1). Blocks merging section 2.
- [ ] G-owner (b) After the first module release, the owner sets the GHCR package `open-platform-model/modules/opm_operator`: linked to opm-operator only, Actions access for opm-operator only, visibility public. Check: `gh api orgs/open-platform-model/packages/container/modules%2Fopm_operator --jq '[.visibility, .repository.full_name]'` prints `["public","open-platform-model/opm-operator"]`. Blocks no section of this change; it blocks the cli's `install-operator-from-module`.
- [ ] G-cascade `.github` `cascade-notify.yml` is on `main` and opm-operator `join-release-cascade` has merged. Check: `gh api repos/open-platform-model/.github/contents/.github/workflows/cascade-notify.yml --jq .path` succeeds, and `notify-downstream` is in `origin/main:.github/workflows/release.yml`. Blocks task 5.5 only. Without it, 5.5 is skipped, and the skip is recorded in design.md. (Reconciled 2026-10-04: join-release-cascade merged as #217, but notify is the `cascade-notify` action in a key-holding job, not a reusable workflow, and the wiring contract admits one such job; 5.5 is deferred, design.md "Deferred".)
- [ ] G-enh The enhancements PR amending 0021 has merged. Check: `gh pr list --repo open-platform-model/enhancements --state merged --search "0021 in:title"` returns it, and `origin/main` of enhancements holds the amended `0021:D4` and the new decision that the install artifact is a registry module on its own train. Record the new decision's number in design.md. Blocks task 5.6 only.

## 1. Record the sandbox evidence (spike)

- [x] 1.1 Check G-dep and G-sandbox. If either is unticked, stop here and report which. (2026-10-04: G-dep's checks pass on `origin/main` `81a640d` and are recorded in design.md "Reconciled with the merged tree"; the box stays for the supervisor. G-sandbox is moved to the reviewer by the supervisor's instruction: the writer may not run anything in the sandbox.)
- [x] 1.2 Write the evidence for U1 to U8 into design.md under "G-sandbox evidence": run URL, observed output and acting token for each item. An item that contradicts the design stops the change here. Examples: U1 finds the root's last release from a module tag, U4's override is ignored, U5's hidden commits open a module release PR, or U7 cannot tell a re-run from changed content. Revise design.md and the spec deltas before section 2. (Done as far as possible without the sandbox: the static U2 evidence and the local U6 evidence are recorded; the sandbox steps for U1 to U8 are in the PR body, and the PR does not merge until the reviewer records them there.)
- [x] 1.3 `openspec validate release-operator-module --strict` passes. The tree is unchanged outside `openspec/`, so the gates confirm it. Then commit `docs(openspec): record the module release sandbox evidence`. (Committed with the reconciliation as `docs(openspec): reconcile release-operator-module with the merged module`.)

## 2. The module releases on its own train

Every commit in this section is a hidden type, so landing it cuts no release of either unit. Section 3 opens the module's first release PR.

- [x] 2.1 `release-please-config.json`:
  - add top-level `"separate-pull-requests": true`;
  - add `"exclude-paths": ["modules/opm_operator"]` to package `"."`;
  - add package `"modules/opm_operator"` exactly as design.md "Separate release PRs and excluded paths" lists it;
  - add `modules/opm_operator/RELEASE` holding `0.1.0 # x-release-please-version` (catalog_opm's shape; release-please's generic updater skips a missing extra file, so without a seed the file would never exist), unless U5 records otherwise.

  Leave `.release-please-manifest.json` unchanged. Verify: `jq -e '.packages["modules/opm_operator"] as $m | $m["bump-minor-pre-major"] == true and $m["bump-patch-for-minor-pre-major"] == true and $m.component == "opm_operator" and $m["include-component-in-tag"] == true and .packages["."]["exclude-paths"] == ["modules/opm_operator"]' release-please-config.json`.
- [x] 2.2 `release.yml`:
  - `image-release`, `publish-examples`, `publish-docs`, `publish-release` and `notify-downstream` gate on `needs.release-please.outputs.release_created == 'true'` (`notify-downstream` keeps `&& vars.CASCADE_NOTIFY != 'off'`), and `.tasks/cascade/wiring-check.sh` expects the new notify `if`;
  - the `release-please` job exports `module_release_created`, `module_tag_name` and `module_version` from the `modules/opm_operator--*` step outputs;
  - `publish-examples` uses `git describe --tags --abbrev=0 --match 'v[0-9]*' "${TAG}^"`.

  Verify: `grep -n "releases_created" .github/workflows/release.yml` finds nothing.
- [x] 2.3 `release.yml` `release-please` job: check out with the App token, then add the step "Advance the module's identity.Version on its release PR" from design.md "Identity advance". It installs opm from `.opm-cli-version`, uses branch `release-please--branches--main--components--opm_operator`, reads the version from that branch's `.release-please-manifest.json`, and commits `chore: advance opm_operator identity.Version to ${VERSION}`. Verify in a scratch clone, never against GitHub: running the step's script twice on a branch already at the version leaves no commit.
- [x] 2.4 `.github/scripts/image-tag-guard.sh` gains a read-only `digest REF` mode that prints the manifest-list digest, factored from `manifest_digest`. Its argument check `[ $# -eq 3 ] || usage` becomes per mode: two arguments for `digest`, three for `probe` and `verify`, and `usage` names all three modes. `.github/scripts/release-guard.sh` gains a read-only `assert-published TAG` mode. Its `publish` mode picks the required assets by tag shape: `install.yaml` for `opm_operator-v*`, and `install.yaml` plus `opm-examples.tar.gz` for `v*`, unchanged. Verify against real releases, from outside any checkout, with `GH_REPO=open-platform-model/opm-operator`:
  - `assert-published v1.0.0-beta.5` passes;
  - `assert-published v9.9.9` fails naming the count;
  - `digest ghcr.io/open-platform-model/opm-operator:v1.0.0-beta.5` prints the digest the cli's embedded manifest names;
  - `probe` and `verify` with two arguments still print the usage.
- [x] 2.5 Add `hack/operator-module/release-check.sh` per design.md "Module release gate inside `Lint`", running its `cue export` calls from the module directory and taking `RELEASE_GUARD`, `IMAGE_TAG_GUARD`, `DRIFT_CHECK` and `MIN_OPERATOR_VERSION_FILE` from the environment with the defaults design.md names, and resolving both tags with `git rev-parse --verify` before the ancestry check, and the task `operator-module:release-check` (it takes `VERSION`) in `.tasks/operator-module.yaml`, the include add-operator-module creates. In `lint.yml`:
  - the checkout step gains `fetch-depth: 0`;
  - add a `cue-lang/setup-cue` step, pinned by full SHA, at the `CUE_VERSION` `.github/workflows/test.yml` names;
  - add a step after the release-pin check, `if: (github.head_ref || github.ref_name) == 'release-please--branches--main--components--opm_operator'`, with `GH_TOKEN: ${{ github.token }}` and `GH_REPO: ${{ github.repository }}`, that reads the proposed version from the branch's `.release-please-manifest.json` and runs the task.

  Verify locally on the merged module with its committed image: the check passes when `VERSION` equals `identity.Version`, and fails naming the digest when the module names a wrong digest.
- [x] 2.6 Add `hack/operator-module/test-release-check.sh`, an offline test of the checks that need no network, over fixture module trees under `hack/testdata/operator-module-release-check/`, with `RELEASE_GUARD`, `IMAGE_TAG_GUARD` and `DRIFT_CHECK` set to stubs that pass. Each case runs inside a scratch git repository the test builds, per design.md: the fixture tree at `modules/opm_operator/`, a min file naming the first of two tags made in order, and the fixture's operator tag on the second unless the case says otherwise. Cases, each asserting the failure is named:
  - a `-0.dev.` pin;
  - a `cue.mod/local-module.cue`;
  - an `identity.Version` that differs from `VERSION`;
  - a `VERSION` whose major is not 0;
  - `1.0.0` while the module names a `-beta.N` operator tag;
  - an operator tag older than the min file (the fixture names the first tag, the min file the second);
  - a min file naming a tag the repository lacks, failing as unresolvable, not as older;
  - two failures at once, both named before it exits;
  - a clean tree passes.

  Add a `Lint` step that runs it on every pull request.
- [x] 2.7 `release.yml` job `module-publish`, per design.md "Publish job" steps 1 to 7:
  - `needs: release-please`, `if: needs.release-please.outputs.module_release_created == 'true'`;
  - permissions `contents: write` and `packages: write` only;
  - every third-party action pinned by full SHA;
  - `CUE_REGISTRY` and `OPM_REGISTRY` mapping `opmodel.dev` to `ghcr.io/open-platform-model`;
  - the reuse branch keyed on the U7 result;
  - `hack/operator-module/defaults.cue` holding the empty values file U6 proved.
- [x] 2.8 `release.yml` job `module-publish-release`: `needs: [release-please, module-publish]`, the same `if`, `permissions: contents: write` only, sparse checkout of `.github/scripts` at the module tag, and one step `release-guard.sh publish "$MODULE_TAG"`.
- [x] 2.9 Prove the install manifest on a fresh cluster with a job in `test-e2e.yml`, or a `module.yml` workflow, on pull requests that change `modules/opm_operator/` or `hack/operator-module/`. The job does the following:
  - installs opm from `.opm-cli-version`;
  - renders the PR's module directory at `hack/operator-module/defaults.cue` as `opm-operator` in `opm-operator-system`;
  - checks that the Namespace and the CRDs come before every namespaced object;
  - creates a kind cluster and runs `kubectl apply --server-side -f`;
  - waits for `deployment/opm-operator-controller-manager` to roll out in `opm-operator-system`.

  It runs in CI only (no egress locally). Name it in the PR body.
- [x] 2.10 `AGENTS.md`, under Registry, after the release-tags bullet: one bullet saying that the repository releases two units. The operator uses `vX.Y.Z` with root `CHANGELOG.md`. The module uses `opm_operator-vX.Y.Z` with `modules/opm_operator/CHANGELOG.md`. Each has its own release PR. The module's release PR gains an identity-advance commit; wait for it before merging. A PR that changes the module must change nothing else, or it releases both units. Module releases wait while `main`'s `config/` differs from the operator release the module deploys. Only `module-publish` publishes `opmodel.dev/modules/opm_operator`. Scan the bullet for a bare at-sign and for em dashes.
- [x] 2.11 Before committing, run these verifications:
  - The publisher search finds only `module-publish` and the task it calls: `grep -rnE 'module publish|modules/opm_operator' .github/workflows .github/scripts .tasks Taskfile.yml hack`, each hit reviewed.
  - The tag-mutation search from the `release-automation` spec finds nothing.
  - Every `gh` call names the repository.
  - `hack/operator-module/test-release-check.sh` passes.
  - `openspec validate release-operator-module --strict` passes.

  Then run actionlint, shellcheck and `task dev:fmt dev:vet dev:lint dev:test`, all green, and commit `ci(release): release the operator module on its own train`.

## 3. The carrier opens the first module release

This section is its own PR, and it changes only `modules/opm_operator/README.md`, so its squash commit releases the module and not the operator. Its commit is prepared on branch `feat/operator-module-release-train` off `origin/main`, outside this change's PR; the supervisor opens that PR once 3.1 holds. Opening it earlier is unsafe: under the single-package config on `main` today, its `feat` would cut an operator release.

- [ ] 3.1 Check that section 2 is on `origin/main`: `git show origin/main:release-please-config.json | jq -e '.packages["modules/opm_operator"]'`. If not, stop.
- [ ] 3.2 `modules/opm_operator/README.md`: add a section on the release train. A module version is `0.y.z` on the `@v0` path and names the one operator release it deploys in `operator/operator.cue`; each module release attaches `install.yaml`, the module rendered at its defaults; a module version's `CHANGELOG.md` entry `fix(deps): deploy operator <tag> ...` marks the operator it moved to. Follow `docs/STYLE.md`. Verify: `git diff --name-only origin/main` names only `modules/opm_operator/README.md`.
- [ ] 3.3 Run `task dev:fmt dev:vet dev:lint dev:test`, all green, and commit `feat(module): publish the operator module on its own release train`. After merge, the module's release PR proposing `0.1.0` opens with the identity-advance commit on it; the supervisor checks this, and the owner merges it after G-owner (a).

## 4. An operator release opens the module's image PR

- [x] 4.1 Add `hack/operator-module/image.sh set <operator-tag>` per design.md "Image-bump PR". It does the following:
  - refuses an unpublished tag (`release-guard.sh assert-published`);
  - refuses, naming the files, when `config/crd/bases` or the RBAC role files at the tag differ from `origin/main`;
  - resolves the digest with `image-tag-guard.sh digest`;
  - writes only `Version` and `Image.digest` in `modules/opm_operator/operator/operator.cue`;
  - prints `breaking=0|1` and `title=<title>` in `GITHUB_OUTPUT` shape. Breaking means the operator CHANGELOG sections after the module's previous operator tag hold a "BREAKING CHANGES" heading, or a CRD under `config/crd/bases` at the tag dropped a version served at the previous tag.

  The script changes no file other than `operator/operator.cue`.
- [x] 4.2 Add `hack/operator-module/test-image.sh`, an offline test of the title, breaking and refusal rules against fixture CHANGELOG excerpts and CRD pairs under `hack/testdata/operator-module-image/`. Run it from the `Lint` job. Cases:
  - plain release gives `fix(deps)`;
  - a breaking CHANGELOG entry gives `fix(deps)!`;
  - a dropped served version gives `fix(deps)!`;
  - a CRD that differs between the tag and `main` fails naming the file;
  - a write outside `operator/operator.cue` fails the script.
- [x] 4.3 Add `release.yml` job `module-image-pr`, calling `module-image.yml` (design.md "Image-bump PR", two-job split):
  - `needs: [release-please, publish-release]`, `if: needs.release-please.outputs.release_created == 'true'`;
  - `compute`, no App token: checks out `main`, runs `hack/operator-module/image.sh set "$TAG"`, uploads the changed module file and the body;
  - `publish`: mints the release App token, checks out `main`, unpacks the change and runs `hack/operator-module/bot-pr.sh`, which rebuilds or extends `module/operator-image` per design.md (offline test `hack/operator-module/test-bot-pr.sh`, a `Lint` step);
  - pushes with `--force-with-lease`;
  - creates or edits the PR with `gh pr create|edit --repo "$GH_REPO"` and the computed title;
  - does nothing when the tree is unchanged.
- [x] 4.4 Add `.github/workflows/module-image.yml`: `workflow_dispatch` with input `tag` (and `workflow_call`, so 4.3 runs the same jobs), `permissions: {}` at the top.
- [x] 4.5 Check by search that no step pushes to `main`: `grep -n 'push' .github/workflows/release.yml .github/workflows/module-image.yml`, each hit reviewed. Then run actionlint, shellcheck, `hack/operator-module/test-image.sh` and `task dev:fmt dev:vet dev:lint dev:test`, all green, and commit `ci(release): open the module image PR after each operator release`.

## 5. The module's own cascade PR and notify

- [ ] 5.1 `.tasks/cascade/cascade.sh`: never list or edit a file under `modules/opm_operator/`. Add an offline case to `.tasks/cascade/test.sh`, "The operator module is left to its own task": with the module's catalog behind, `task -x deps:cascade` leaves `modules/opm_operator/` byte-unchanged.
- [ ] 5.2 Add `task deps:cascade:module` per the `deps-cascade` delta: the same script with the module as its only target, the same holds and frozen keys, the same exit codes. It never touches `operator/operator.cue`, `identity/`, the generated files, `CHANGELOG.md`, `RELEASE` or any path outside the module. Add `.tasks/cascade/module-pins.sh` (two rows, keyed `opmodel.dev/catalogs/opm@v4` and `opmodel.dev/core@v2`, displayed as the operator module's opm catalog and core) and `.tasks/cascade/module-classes` (`modules/opm_operator/` shipped), and the tasks `deps:cascade:module:title` and `deps:cascade:module:body`, which call the resolver with those two files as `deps:cascade:title` and `deps:cascade:body` do. Add offline cases to `.tasks/cascade/test.sh`: "Module pins move, image and version stay", "Catalog hold holds the module", "Nothing to move". Run `CASCADE_TEST_SET=offline task -x deps:cascade:test`, and check the title with the real resolver: `task -x deps:cascade:module:title` on a tree where the catalog moved prints `fix(deps): bump the operator module's opm catalog to ...`.
- [ ] 5.3 Add `.github/workflows/module-deps.yml` per the `deps-cascade` delta: `repository_dispatch` `upstream-released` and `workflow_dispatch`, `permissions: {}` at the top, the resolver checked out at the pinned `.github` SHA (and `.tasks/cascade/wiring-check.sh` listing it), a compute job with no token and a publish job with the release App token, branch `module/deps` rebuilt or extended by `bot-pr.sh` as for the image PR, push with `--force-with-lease`, `gh pr create|edit --repo "$GH_REPO"` with the resolver's title and body, and a dry run while `vars.CASCADE_DRY_RUN != 'false'` that writes the diff to the job summary and pushes nothing.
- [ ] 5.4 `AGENTS.md`: the bullet from 2.10 gains one sentence: the module's core and catalog move only through `module/deps` PRs.
- [ ] 5.5 (Deferred 2026-10-04, design.md "Deferred"; the requirement is out of this change's spec delta.) If G-cascade holds: add `release.yml` job `module-notify` (`needs: [release-please, module-publish-release]`, `if: needs.release-please.outputs.module_release_created == 'true' && vars.CASCADE_NOTIFY != 'off'`, `permissions: contents: read`, calling `cascade-notify.yml` with the module tag). Otherwise record in design.md that the job waits for G-cascade, and leave this box unticked with that note.
- [ ] 5.6 If G-enh holds: add `enhancement.yaml` to this change declaring enhancement `0021` with the decisions this change completes, read from the merged amendment: the new decision that the install artifact is a registry module on its own train, for its release half, and `D4` only if its amended text is fully met by the module release publishing its render (the operator release's own asset is removed by `stop-operator-install-manifest`). Check each claim against proposal.md "Not in this change"; when in doubt, leave the decision out. Otherwise stop and report that the archive waits for G-enh.
- [ ] 5.7 `openspec validate release-operator-module --strict` passes. Then run actionlint, shellcheck and `task dev:fmt dev:vet dev:lint dev:test`, all green, and commit `ci(cascade): give the operator module its own cascade PR and notify`.
- [ ] 5.8 Verify the change with the repo-local `openspec-verify-change` skill. Then archive it with `openspec-archive-change`: the archive commit `docs(openspec): archive release-operator-module` rides this section's PR. From the workspace root, log the delivery with `task enhancements:delivery:log` in explicit mode (`ID=0021 REPO=opm-operator CHANGE=release-operator-module`), and confirm it printed `logged`.
