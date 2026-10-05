Every section runs `actionlint` on the workflows it changes, `task cascade:wiring:check`, and the repo gates `task dev:fmt dev:vet dev:lint`. No Go code changes, so `task dev:test` runs once, in section 5. Every commit is `ci` or `docs` typed, so no section cuts an operator release.

## 1. Proposal

- [x] 1.1 Write proposal.md, design.md, the `workflow-hardening` and `container-image-publish` deltas and this file.
- [x] 1.2 `openspec validate harden-release-workflows --strict`, then commit `docs(openspec): propose harden-release-workflows`

## 2. Release key Environment and explicit permissions

- [x] 2.1 `release.yml`: workflow-level `permissions: {}`; `release-please` gets `environment: release` and `permissions: {}` (design D1, D2).
- [x] 2.2 `lint.yml`, `test.yml`, `test-e2e.yml`: top-level `permissions: contents: read` (the e2e job keeps its own grant until section 4).
- [x] 2.3 Every file under `.github/workflows/` has a top-level `permissions:` of `{}` or `contents: read`. Only `release-please` declares `release`, and only it reads `RELEASE_APP_PRIVATE_KEY`.
- [x] 2.4 Gates green, then commit `ci: declare least-privilege permissions and the release environment`

## 3. No Actions cache in publishing jobs

- [x] 3.1 `release.yml` `image-release`: drop `cache-from`/`cache-to`; `publish-examples`: setup-go `cache: false` (design D3).
- [x] 3.2 `publish-fixtures.yml`: setup-go `cache: false`; `image-pr.yml`: drop `cache-from`/`cache-to`.
- [x] 3.3 `grep -rn 'type=gha\|actions/cache' .github/workflows` finds nothing.
- [x] 3.4 Gates green, then commit `ci: build and publish without the Actions cache`

## 4. Bot heads hold no write token

- [x] 4.1 `image-pr.yml`: skip `deps/cascade` and `release-please--*` heads (design D5).
- [x] 4.2 `test-e2e.yml`: split into `publish-fixtures` (`packages: write`, trusted events only, setup-go `cache: false`) and `test-e2e` (`contents: read`, `packages: read`, pins and credentials only after a successful publish) (design D4).
- [x] 4.3 Gates green, then commit `ci: keep write tokens away from cascade and release heads`

## 5. Code owners and docs

- [x] 5.1 Add `.github/CODEOWNERS` (design D6).
- [x] 5.2 `AGENTS.md`: record the release Environment, explicit permissions, the no-cache rule, the bot-head rule and the e2e split.
- [x] 5.3 `task dev:test`, gates green, `openspec validate harden-release-workflows --strict`, then commit `ci: add code owners for the release machinery`

## 6. Verify and archive

- [x] 6.1 Run openspec verify for `harden-release-workflows` and resolve its findings.
- [x] 6.2 Archive on this branch; check `openspec/specs/workflow-hardening/spec.md` has a real Purpose and `openspec validate workflow-hardening --strict` passes. Commit `chore(openspec): archive harden-release-workflows`

## 7. Review fixes

- [x] 7.1 Skip `dependabot/*` heads in `image-pr.yml` and the e2e `publish-fixtures` job, and add a 7-day `cooldown` to the `github-actions` Dependabot ecosystem (design D5). Commit `ci: keep write tokens away from Dependabot heads`
- [x] 7.2 `test-e2e.yml`: when `publish-fixtures` did not succeed, seed a job-local registry from the tree, connect it to kind and set `LOCAL_REGISTRY`, so the podinfo and redis specs run on bot heads and forks (design D4). Commit `ci: run the podinfo e2e specs on heads without a publish`
- [x] 7.3 Pin `go-task/setup-task` to `3.54.0` in every workflow and drop `repo-token` from `publish-fixtures.yml` (design D7). Commit `ci: install Task at an exact version`
- [x] 7.4 `test-e2e.yml` `test-e2e` job: setup-go `cache: false`, so the whole workflow can go on the wiring check's `publish-workflows` list with `image-pr.yml` (design D3). Commit `ci: drop the Go cache from the e2e suite job`
- [x] 7.5 `.github/CODEOWNERS`: add `/.opm-cli-version`, `/.opm-docs-version` and `/docs-kit.cue` (design D6). Commit `ci: code-own the opm and docs-kit pins`
- [x] 7.6 `container-image-publish`: drop the `paths` claim, name the `dependabot/*` skip, and say the PR image is amd64 and arm64. Commit `docs(openspec): match the PR image spec to image-pr.yml`
- [x] 7.7 `test-e2e.yml`: install `kind` `v0.33.0` and the flux CLI from release assets checked against in-tree sha256 digests (`.tasks/flux.yaml` gains `FLUX_CLI_SHA256_LINUX_AMD64`) (design D8). Commit `ci: install kind and flux from checked release assets`

## 8. Merge main's operator module release train

- [x] 8.1 `git merge origin/main`; resolve `lint.yml` (keep `permissions` and `CUE_VERSION`), `release.yml` (main's outputs, this branch's Environment and grant) and `AGENTS.md`.
- [x] 8.2 `environment: release` on `module-identity-advance` and the `publish` jobs of `module-image.yml` and `module-deps.yml`; drop `secrets:` from `module-image-pr` and the `workflow_call` secret from `module-image.yml` (design D9). Commit `ci: read the release key only in the release environment everywhere`
- [x] 8.3 `cache: false` on setup-go in `module-identity-advance`, `module-publish`, `module-manifest`; Task `3.54.0` in `module-publish` and `module-deps.yml`.
- [x] 8.4 `image-pr.yml` and the e2e `publish-fixtures` job skip `module/*` heads. Commit `ci: keep write tokens away from the operator module's bot heads`
- [x] 8.5 Canonical wiring check at `.github` `7b9ad1b` with the refreshed config: the release-key and cache rules pass; update `workflow-hardening`, `container-image-publish`, `AGENTS.md` and this change's files.

## 9. Review fixes on the operator module publish path

- [x] 9.1 `bot-pr.sh` refuses symlinks, hard links and mode changes; `test-bot-pr.sh` cases `symlink`, `hardlink`, `mode` fail against the old script. Commit `fix(cascade): refuse symlinks, hard links and mode changes in bot-pr.sh`
- [x] 9.2 The `publish` jobs of `module-image.yml` and `module-deps.yml` refuse tarball members that are not plain files or directories. Commit `fix(ci): refuse link members in the operator module change tarball`
- [x] 9.3 Record the key-move merge order and the bounded module publish path in design D9 and `workflow-hardening`.
