# Tasks: release-operator-module

Worktree `opm-operator/.claude/worktrees/release-operator-module`, branch `feat/release-operator-module`. Run every command inside that worktree.

Delivery is one PR per section (proposal.md). Before starting a section, rebase the branch on `origin/main`.

Every section's commit task runs these checks:

- workflow lint: `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12` over `.github/workflows/`, with `shellcheck` on PATH;
- `shellcheck` over every script this change adds or edits;
- the validation gates `task dev:fmt dev:vet dev:lint dev:test`, plus `task docs:bundle:check` where docs or `docs-kit.cue` change.

The local kind cluster `opm-dev` and the host's containers have no internet egress. Cluster e2e is proven only in CI, and each PR body says so.

## Gates

The supervisor ticks each gate after confirming it with the read-only check given. A worker never ticks these.

- [ ] G-dep **GATED: after add-operator-module is merged.** Check: `gh pr list --repo open-platform-model/opm-operator --state merged --search "add-operator-module in:title"` returns the PR, and `git ls-tree -r --name-only origin/main` lists `modules/opm_operator/cue.mod/module.cue`, `identity/identity.cue`, `operator/operator.cue`, `zz_generated_crds.cue` and `zz_generated_rbac.cue` under it. Compare every name in design.md "Interface with add-operator-module" with the merged tree, including the fields of `operator/operator.cue`, the generate and drift-check scripts and tasks, and the drift check's `--ref` mode. Record the result in that section and substitute any name that changed everywhere in this change. If the drift check cannot compare against an operator tag, stop and report to the supervisor: the release gate needs a check against the deployed operator release, not `HEAD`. Blocks section 1.
- [ ] G-sandbox Unknowns U1 to U9 (design.md, "Release-flow sandbox unknowns") are proven in `open-platform-model/release-flow-sandbox` with this change's config and job shape, U1 to U5 and U9 run as the App. Preconditions: `gh api repos/open-platform-model/release-flow-sandbox/immutable-releases` returns `"enabled":true`, and the sandbox's `includes_parents` ruleset listing shows `tags-immutable` and `tags-create-app-only` active. Record run URLs, outputs and the acting token for each item. Blocks section 2.
- [ ] G-owner (a) The owner approves the tag shape `opm_operator-vX.Y.Z`, the `0.1.0` start, the 0.x bump rule and documenting `#config` at the module's own version (design.md, open question 4). Blocks merging section 2.
- [ ] G-owner (b) After the first module release, the owner sets the GHCR package `open-platform-model/modules/opm_operator`: linked to opm-operator only, Actions access for opm-operator only, visibility public. Check: `gh api orgs/open-platform-model/packages/container/modules%2Fopm_operator --jq '[.visibility, .repository.full_name]'` prints `["public","open-platform-model/opm-operator"]`. Blocks section 4.
- [ ] G-cascade `.github` `cascade-notify.yml` is on `main` and opm-operator `join-release-cascade` has merged. Check: `gh api repos/open-platform-model/.github/contents/.github/workflows/cascade-notify.yml --jq .path` succeeds, and `notify-downstream` is in `origin/main:.github/workflows/release.yml`. Blocks task 4.4 only. Without it, 4.4 is skipped, and the skip is recorded in design.md.
- [ ] G-cli Every one of these holds:
  - a published cli release installs the operator from the module;
  - the cli on `main` no longer embeds or downloads an operator release's `install.yaml`: `git grep -n 'install.yaml' origin/main -- internal/operator Taskfile.yml` in cli finds no download or embed;
  - the cli's `deps:cascade` no longer resolves the operator with `--asset install.yaml`;
  - the cli's release-pin check keys on the module pin, not the embedded manifest.

  Blocks section 5.

## 1. Record the sandbox evidence (spike)

- [ ] 1.1 Check G-dep and G-sandbox. If either is unticked, stop here and report which.
- [ ] 1.2 Write the evidence for U1 to U9 into design.md under "G-sandbox evidence": run URL, observed output and acting token for each item. An item that contradicts the design stops the change here. Examples: U1 finds the root's last release from a module tag, U4's override is ignored, or U7 cannot tell a re-run from changed content. Revise design.md and the spec deltas before section 2.
- [ ] 1.3 `openspec validate release-operator-module --strict` passes. The tree is unchanged outside `openspec/`, so the gates confirm it. Then commit `docs(openspec): record the module release sandbox evidence`.

## 2. The module releases on its own train

Every commit in this section is a hidden type, so landing it cuts no operator release. It does open the module's first release PR (G-owner (a)).

- [ ] 2.1 `release-please-config.json`:
  - add top-level `"separate-pull-requests": true`;
  - add `"exclude-paths": ["modules/opm_operator"]` to package `"."`;
  - add package `"modules/opm_operator"` exactly as design.md "Separate release PRs and excluded paths" lists it;
  - add `modules/opm_operator/RELEASE` holding `0.0.0 # x-release-please-version` if U5 showed release-please needs a seed, otherwise as U5 recorded.

  Leave `.release-please-manifest.json` unchanged. Verify: `jq -e '.packages["modules/opm_operator"] as $m | $m["bump-minor-pre-major"] == true and $m["bump-patch-for-minor-pre-major"] == true and $m.component == "opm_operator" and $m["include-component-in-tag"] == true and .packages["."]["exclude-paths"] == ["modules/opm_operator"]' release-please-config.json`.
- [ ] 2.2 `release.yml`:
  - `image-release`, `publish-examples`, `publish-docs`, `publish-release`, and `notify-downstream` if present, gate on `needs.release-please.outputs.release_created == 'true'`;
  - the `release-please` job exports `module_release_created`, `module_tag_name` and `module_version` from the `modules/opm_operator--*` step outputs;
  - `publish-examples` uses `git describe --tags --abbrev=0 --match 'v[0-9]*' "${TAG}^"`.

  Verify: `grep -n "releases_created" .github/workflows/release.yml` finds nothing.
- [ ] 2.3 `release.yml` `release-please` job: check out with the App token, then add the step "Advance the module's identity.Version on its release PR" from design.md "Identity advance". It installs opm from `.opm-cli-version`, uses branch `release-please--branches--main--components--opm_operator` and commits `chore: advance opm_operator identity.Version to ${VERSION}`. Verify in a scratch clone, never against GitHub: running the step's script twice on a branch already at the version leaves no commit.
- [ ] 2.4 `.github/scripts/image-tag-guard.sh` gains a read-only `digest REF` mode that prints the manifest-list digest, factored from `manifest_digest`. `.github/scripts/release-guard.sh` gains a read-only `assert-published TAG` mode. Its `publish` mode picks the required assets by tag shape: `install.yaml` for `opm_operator-v*`, and `install.yaml` plus `opm-examples.tar.gz` for `v*`, unchanged until section 5. Verify against real releases, from outside any checkout, with `GH_REPO=open-platform-model/opm-operator`:
  - `assert-published v1.0.0-beta.5` passes;
  - `assert-published v9.9.9` fails naming the count;
  - `digest ghcr.io/open-platform-model/opm-operator:v1.0.0-beta.5` prints the digest the cli's embedded manifest names.
- [ ] 2.5 Add `hack/module-release-check.sh` per design.md "Module release gate inside Lint", and `task module:release-check` in `.tasks/` (it takes `VERSION`). In `lint.yml`, add a step after the release-pin check, `if: (github.head_ref || github.ref_name) == 'release-please--branches--main--components--opm_operator'`, that reads the proposed version from the branch's `modules/opm_operator/RELEASE` and runs the task. Verify locally on the merged module with its committed image:
  - the check passes when `VERSION` equals `identity.Version`;
  - each failure is reported by name, one at a time in a scratch copy: a wrong digest, a `-0.dev.` pin, a mismatched version, `1.0.0` with a beta operator tag, and one edited CRD line;
  - with two failures at once, both are named before it exits.
- [ ] 2.6 `release.yml` job `module-publish`, per design.md "Publish job" steps 1 to 9:
  - `needs: release-please`, `if: needs.release-please.outputs.module_release_created == 'true'`;
  - permissions `contents: write`, `packages: write`, `id-token: write`, `attestations: write`;
  - every third-party action pinned by full SHA;
  - `CUE_REGISTRY` and `OPM_REGISTRY` mapping `opmodel.dev` to `ghcr.io/open-platform-model`;
  - the reuse branch keyed on the U7 result;
  - `hack/module-defaults.cue` holding the empty values file U6 proved.
- [ ] 2.7 `release.yml` job `module-publish-release`: `needs: [release-please, module-publish]`, the same `if`, `permissions: contents: write` only, sparse checkout of `.github/scripts` at the module tag, and one step `release-guard.sh publish "$MODULE_TAG"`.
- [ ] 2.8 Prove the install manifest on a fresh cluster with a job in `test-e2e.yml`, or a `module.yml` workflow, on pull requests that change `modules/opm_operator/` or `hack/module-*`. The job does the following:
  - installs opm from `.opm-cli-version`;
  - renders the PR's module directory at `hack/module-defaults.cue` as `opm-operator` in `opm-operator-system`;
  - checks that the Namespace and the CRDs come before every namespaced object;
  - creates a kind cluster and runs `kubectl apply --server-side -f`;
  - waits for `deployment/opm-operator-controller-manager` to roll out in `opm-operator-system`.

  It runs in CI only (no egress locally). Name it in the PR body.
- [ ] 2.9 `AGENTS.md`, under Registry, after the release-tags bullet: one bullet saying that the repository releases two units. The operator uses `vX.Y.Z` with root `CHANGELOG.md`. The module uses `opm_operator-vX.Y.Z` with `modules/opm_operator/CHANGELOG.md`. Each has its own release PR. The module's release PR gains an identity-advance commit; wait for it before merging. Only `module-publish` publishes `opmodel.dev/modules/opm_operator`. Scan the bullet for a bare at-sign and for em dashes.
- [ ] 2.10 Before committing, run these verifications:
  - The publisher search finds only `module-publish` and the task it calls: `grep -rnE 'module publish|modules/opm_operator' .github/workflows .github/scripts .tasks Taskfile.yml hack`, each hit reviewed.
  - The tag-mutation search from the `release-automation` spec finds nothing.
  - Every `gh` call names the repository.
  - `openspec validate release-operator-module --strict` passes.

  Then run actionlint, shellcheck and `task dev:fmt dev:vet dev:lint dev:test`, all green, and commit `ci(release): release the operator module on its own train`.

## 3. An operator release opens the module's image PR

- [ ] 3.1 If G-dep recorded that the module's generator has no way to run over another source tree, add a `--ref <tag>` mode to it that extracts `config/` from the tag with `git archive`, as the drift check's release mode does. Verify: `--ref v1.0.0-beta.5` reproduces the committed generated files when the module names `v1.0.0-beta.5`.
- [ ] 3.2 Add `hack/module-image.sh set <operator-tag>` per design.md "Image-bump PR". It does the following:
  - refuses an unpublished tag (`release-guard.sh assert-published`);
  - resolves the digest with `image-tag-guard.sh digest`;
  - writes only `Version` and `Image.digest` in `modules/opm_operator/operator/operator.cue`;
  - regenerates the module's CRDs and RBAC from the operator source at the tag;
  - prints `breaking=0|1` and `title=<title>` in `GITHUB_OUTPUT` shape. Breaking means the operator CHANGELOG sections after the previous tag hold a "BREAKING CHANGES" heading, or a regenerated CRD dropped a served version.

  The script changes no file outside `modules/opm_operator/`.
- [ ] 3.3 Add `hack/test-module-image.sh`, an offline test of the title and breaking rules against fixture CHANGELOG excerpts and CRD pairs under `hack/testdata/module-image/`. Run it from the `Lint` job. Cases:
  - plain release gives `fix(deps)`;
  - a breaking CHANGELOG entry gives `fix(deps)!`;
  - a dropped served version gives `fix(deps)!`;
  - a change outside `modules/opm_operator/` fails the script.
- [ ] 3.4 Add `release.yml` job `module-image-pr`:
  - `needs: [release-please, publish-release]`, `if: needs.release-please.outputs.release_created == 'true'`;
  - mints the release App token, checks out `main`, runs `hack/module-image.sh set "$TAG"`;
  - rebuilds or extends `module/operator-image` per design.md;
  - pushes with `--force-with-lease`;
  - creates or edits the PR with `gh pr create|edit --repo "$GH_REPO"` and the computed title;
  - does nothing when the tree is unchanged.
- [ ] 3.5 Add `.github/workflows/module-image.yml`: `workflow_dispatch` with input `tag`, `permissions: {}` at the top, and one job running the same steps as 3.4 for the given tag.
- [ ] 3.6 Check by search that no step pushes to `main`: `grep -n 'push' .github/workflows/release.yml .github/workflows/module-image.yml`, each hit reviewed. Then run actionlint, shellcheck, `hack/test-module-image.sh` and `task dev:fmt dev:vet dev:lint dev:test`, all green, and commit `ci(release): open the module image PR after each operator release`.

## 4. The module's docs and its place in the cascade

- [ ] 4.1 Check G-owner (b). If it is unticked, stop and report.
- [ ] 4.2 `docs-kit.cue`: add the `opm-operator-module` bundle per the `docs-bundle` delta, with a `markdown` source over `modules/opm_operator/docs/` (or the directory G-dep recorded). Write its pages:
  - the `#config` reference, one row per field with its default;
  - how a module version maps to the operator version it deploys;
  - the install manifest and where it is published.

  Follow `docs/STYLE.md`. In `docs.yml`, add the module bundle to `check` and `edge`, and make `dispatch` pick the project from the tag prefix (`opm_operator-v` gives `opm-operator-module`). In `release.yml`, add job `module-publish-docs` (`needs: [release-please, module-publish]`, module tag). Verify `task docs:bundle:check` builds both bundles.
- [ ] 4.3 `.tasks/cascade/cascade.sh` moves the module's catalog and core per the `deps-cascade` delta, and never touches `operator/operator.cue`, `identity.Version` or the generated files. `.tasks/cascade/pins.sh` prints the module's two shipped rows. Add an offline case to `.tasks/cascade/test.sh` for "Module pins move, image and version stay", and run `CASCADE_TEST_SET=offline task -x deps:cascade:test`.
- [ ] 4.4 If G-cascade holds: add `release.yml` job `module-notify` (`needs: [release-please, module-publish-release]`, `if: needs.release-please.outputs.module_release_created == 'true' && vars.CASCADE_NOTIFY != 'off'`, `permissions: contents: read`, calling `cascade-notify.yml` with the module tag). Otherwise record in design.md that the job waits for G-cascade, and leave this box unticked with that note.
- [ ] 4.5 `openspec validate release-operator-module --strict` passes. Then run actionlint, shellcheck, `task docs:bundle:check` and `task dev:fmt dev:vet dev:lint dev:test`, all green, and commit `ci(cascade): publish the module docs and join the module to the release cascade`.

## 5. GATED: operator releases stop publishing install.yaml

This section moves the kubectl install manifest off the operator release for good. It lands only after G-cli. If the owner wants the change archived before G-cli holds, move this section and its spec deltas into a new change, `stop-operator-install-manifest`. The deltas to move are the `release-automation` "Release published once after every release job" and "Beta prerelease line" changes, and the `container-image-publish` REMOVED requirement. Then archive this one without them.

- [ ] 5.1 Check G-cli. If it is unticked, stop and report which item fails.
- [ ] 5.2 `release.yml` `image-release`: remove "Render digest-pinned install manifest" and "Upload install.yaml to the draft release". Keep `task operator:installer` as a development task. `release-guard.sh publish` then requires only `opm-examples.tar.gz` for `v*` tags.
- [ ] 5.3 `release.yml` `notify-downstream` (operator): stop dispatching to the cli. The operator release now reaches the cli through `module-image-pr`, the module release and `module-notify`. Remove the job, or leave it with no target if the shared workflow requires one. Record which in design.md.
- [ ] 5.4 `docs/site/start/install-the-operator.md` and `README.md`: the kubectl path applies `install.yaml` from the newest module release. Link the module's releases; name no module version. State that the first apply over an operator installed from an earlier manifest needs `opm operator install`: the module's Deployment selector differs from the manifest's, so the Deployment is recreated once and the three old `*-rolebinding` objects are deleted. Remove the warning about `releases/latest`, because a module release is Latest (U9). Keep every remaining operator-version passage inside its release-please block. `TestInstallPageNamesThisRelease` stays green. `AGENTS.md`: the install manifest is a module release asset. Verify `task docs:bundle:check`.
- [ ] 5.5 Run actionlint, shellcheck, `task docs:bundle:check` and `task dev:fmt dev:vet dev:lint dev:test`, all green. Then commit `ci(release): stop attaching install.yaml to operator releases`. Use `ci(release)!:` instead if the owner answered open question 1 that way.
- [ ] 5.6 Verify the change with the repo-local `openspec-verify-change` skill. Then archive it with `openspec-archive-change`: the archive commit `docs(openspec): archive release-operator-module` rides this section's PR. The change declares no enhancement, so there is no delivery log to write.
