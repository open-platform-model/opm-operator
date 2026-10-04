# Tasks: stop-operator-install-manifest

> **BLOCKED: revise before implementing.** This plan predates `0021:D11:R12` and assumes a `module-notify` job that `release-operator-module` deferred. Its kubectl path through `releases/latest/download/install.yaml` (design.md, task 1.4) is ruled out, since a module release never takes the latest mark, and its reliance on `module-notify` (design.md) has nothing to rely on. It also fails `openspec validate --strict`: its `operator-module` MODIFIED drops the scenario "An object added to the kustomize tree only fails the test". Revise all three with `openspec-update` before any task here runs.

Worktree `opm-operator/.claude/worktrees/stop-operator-install-manifest`, branch `feat/stop-operator-install-manifest`, created from `origin/main` when the gates hold. Run every command inside that worktree.

The local kind cluster `opm-dev` and the host's containers have no internet egress. Cluster e2e is proven only in CI, and the PR body says so.

## Gates

The supervisor ticks each gate after confirming it with the read-only check given. A worker never ticks these.

- [ ] G-dep `release-operator-module` and `add-operator-module` are archived on `origin/main`: `git ls-tree --name-only origin/main openspec/changes/archive/` lists both, and `openspec/specs/operator-module/spec.md` exists. Compare this change's MODIFIED requirements with the main specs: each must carry the main text with only this change's edits. Where the main text moved on, carry the same edits onto it. Blocks section 1.
- [ ] G-cli Every one of these holds:
  - a published cli release installs the operator from the module;
  - the cli on `main` no longer embeds or downloads an operator release's `install.yaml`: `git grep -n 'install.yaml' origin/main -- internal/operator Taskfile.yml` in cli finds no download or embed;
  - the cli's `deps:cascade` no longer resolves the operator with `--asset install.yaml`;
  - the cli's release-pin check keys on the module pin, not the embedded manifest.

  Blocks section 1.
- [ ] G-resolver The `.github` resolver no longer requires `install.yaml` to count an operator release as published: `git grep -n 'install.yaml' origin/main -- .github/scripts/cascade` in `.github` finds no such check. Blocks section 1.

## 1. Operator releases stop publishing install.yaml

- [ ] 1.1 Check G-dep, G-cli and G-resolver. If any is unticked, stop and report which item fails.
- [ ] 1.2 `release.yml` `image-release`: remove "Render digest-pinned install manifest" and "Upload install.yaml to the draft release". Keep `task operator:installer` as a development task. `release-guard.sh publish` requires only `opm-examples.tar.gz` for `v*` tags, and `install.yaml` for `opm_operator-v*` tags as before.
- [ ] 1.3 `release.yml` `notify-downstream` (operator): stop dispatching to the cli. Remove the job, or leave it with no cli target if the shared workflow requires one. Record which in design.md.
- [ ] 1.4 `docs/site/start/install-the-operator.md` and `README.md`: the kubectl path applies `releases/latest/download/install.yaml`, the newest module release. Link the module's releases; name no module version. State that the first apply over an operator installed from an operator release's manifest needs `opm operator install`, because the module's Deployment selector differs: the Deployment is recreated once and the three old `*-rolebinding` objects are deleted. Remove the warning about `releases/latest`. Keep every remaining operator-version passage inside its release-please block. `TestInstallPageNamesThisRelease` stays green. `AGENTS.md`: the install manifest is a module release asset only.
- [ ] 1.5 Verify: `grep -n 'install.yaml' .github/workflows/release.yml` names only module jobs; the tag-mutation search from the `release-automation` spec finds nothing; `openspec validate stop-operator-install-manifest --strict` passes.
- [ ] 1.6 Run actionlint (`go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12`, `shellcheck` on PATH), `shellcheck` over the edited scripts, `task docs:bundle:check` and `task dev:fmt dev:vet dev:lint dev:test`, all green. Then commit `feat(release)!: publish the install manifest only from the module release`. The PR body carries the migration note: download `install.yaml` from the newest module release, or use `opm operator install`.
- [ ] 1.7 Verify the change with the repo-local `openspec-verify-change` skill. If enhancement 0021's merged amendment holds a decision this change completes (the amended `0021:D4`: the install manifest is the module's render), add `enhancement.yaml` declaring it, checked against proposal.md's out-of-scope list. Then archive with `openspec-archive-change`: the archive commit `docs(openspec): archive stop-operator-install-manifest` rides this PR, and, with `enhancement.yaml`, log the delivery from the workspace root with `task enhancements:delivery:log` in explicit mode (`ID=0021 REPO=opm-operator CHANGE=stop-operator-install-manifest`).
