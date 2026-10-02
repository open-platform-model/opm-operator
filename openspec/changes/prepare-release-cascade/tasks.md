## 1. Pin the opm CLI in `.opm-cli-version` (beta.2 to beta.4)

- [x] 1.1 Add repo-root `.opm-cli-version` containing the single line `v1.0.0-beta.4`
- [x] 1.2 Replace the `go install github.com/open-platform-model/cli/cmd/opm@<current literal>` step (the current literal is `v1.0.0-beta.2` today) in `.github/workflows/test.yml:58-59` with the read-validate-install block from design.md ("`.opm-cli-version` and how workflows read it")
- [x] 1.3 Same replacement in `.github/workflows/test-e2e.yml:97-98` and `.github/workflows/publish-fixtures.yml:65-66`
- [x] 1.4 Same replacement in `.github/workflows/release.yml:271-274` (`publish-examples`, which checks out the release tag), keeping its comment about the gated publish pipeline
- [x] 1.5 Confirm `grep -rn 'cli/cmd/opm@v' .github/workflows` returns nothing and `grep -rn 'v1.0.0-beta.2' .github/workflows` no longer names the cli; confirm every workflow parses: `python3 -c 'import yaml,sys;[yaml.safe_load(open(f)) for f in sys.argv[1:]]' .github/workflows/*.yml` (and `actionlint` if available)
- [x] 1.6 Install the pinned CLI into a throwaway GOBIN so the developer's own `opm` is not replaced (`GOBIN=$(mktemp -d) go install github.com/open-platform-model/cli/cmd/opm@$(cat .opm-cli-version)`, keeping that GOBIN) and run `OPM=$GOBIN/opm task examples:check` (`.tasks/examples.yaml` passes `OPM` to `hack/fixtures.sh` as `OPM_BIN`); if a fixture now fails a publish gate, stop and record the failure in design.md instead of editing `hack/fixtures.sh`
- [x] 1.7 Add one bullet to `AGENTS.md` "Registry": the opm CLI pin lives only in `.opm-cli-version`, and moving it is a `ci(deps)` commit (workspace RELEASING.md, section "Cascade files")
- [x] 1.8 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(deps): pin the opm CLI in .opm-cli-version at v1.0.0-beta.4`

## 2. G1 release-pin gate

- [x] 2.1 Add `hack/release-pin-check.sh` (executable) implementing the four checks in design.md ("The four checks"): replace directives, OPM Go pseudo-versions and untagged versions via `git ls-remote`, `-0.dev.` pins in `test/fixtures/modules/*` and `test/fixtures/modulepackages/*` cue.mods, tracked `cue.mod/local-module.cue`; report every failure, exit non-zero if any
- [x] 2.2 Add `.tasks/deps.yaml` with task `release-check` (desc names G1 and workspace RELEASING.md, section "Gates") running the script, and include it from `Taskfile.yml` as `deps`
- [x] 2.3 In `.github/workflows/lint.yml`, set the `lint` job's `name:` from `Run on Ubuntu` to `Lint` (unique among the repo's jobs; `test.yml` and `test-e2e.yml` keep `Run on Ubuntu`), and add the step `Release-pin gate (G1, release PRs only)` after "Install Task", with `if: startsWith(github.head_ref || github.ref_name, 'release-please--')` and `run: task deps:release-check`; confirm `grep -rn 'name: Lint$' .github/workflows` names only `lint.yml` jobs and the workflows parse (`python3 -c 'import yaml,sys;[yaml.safe_load(open(f)) for f in sys.argv[1:]]' .github/workflows/*.yml`, `actionlint` if available)
- [x] 2.4 Run `task deps:release-check` on the current tree: it passes (no replace, library `v1.0.0-beta.1` is a tag, no dev pins, no tracked `local-module.cue`)
- [x] 2.5 Negative checks in a scratch copy, not committed: add `replace github.com/open-platform-model/library => ../library` to go.mod; set the library pin to `v1.0.0-beta.9`; add `v: "v2.0.0-0.dev.1"` to `test/fixtures/modules/hello/cue.mod/module.cue`; `git add -f` a `cue.mod/local-module.cue`. Each run fails and names the offender; restore the tree
- [x] 2.6 Add one bullet to `AGENTS.md` "Registry": a release PR must pass `task deps:release-check` (G1, the `Lint` check), so it ships no `replace`, pseudo-version, untagged OPM Go pin, `-0.dev.` fixture pin or tracked `local-module.cue`
- [x] 2.7 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(release): gate release PRs on reproducible OPM pins`

## 3. Leave OPM Go modules to the cascade

- [ ] 3.1 Add `ignore: [{dependency-name: "github.com/open-platform-model/*"}]` to the `gomod` entry of `.github/dependabot.yml`, with a comment that the release cascade owns these bumps (workspace RELEASING.md, section "Pin classes"); keep the `build` prefix and the `kubernetes` group
- [ ] 3.2 Validate the YAML parses (`yq . .github/dependabot.yml` or `python3 -c 'import yaml,sys; yaml.safe_load(open(sys.argv[1]))' .github/dependabot.yml`)
- [ ] 3.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(deps): leave OPM Go module bumps to the release cascade`

## 4. Stop doc-only commits from releasing

Depends on: opmodel.dev change `build-docs-from-branch-head` merged before this section's commit merges (proposal, "Depends on / gates"); until then a docs-only fix in this repo reaches opmodel.dev only with the next release.

- [ ] 4.1 Set `"hidden": true` on the `docs` entry of `release-please-config.json:24`; leave `refactor` visible and leave CHANGELOG.md untouched
- [ ] 4.2 Validate the JSON (`jq . release-please-config.json`)
- [ ] 4.3 Rewrite `AGENTS.md:146` ("Commit type decides the release") to "release-please hides `docs`, `chore`, `test`, `ci` and `build`; `feat`, `fix`, `deps`, `perf` and `refactor` release", citing workspace RELEASING.md, section "Pin classes"; verify with `grep -n 'docs' AGENTS.md` that no line still lists `docs` as releasing
- [ ] 4.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(release): hide the docs changelog section so doc-only commits do not release`

## 5. Archive

- [ ] 5.1 Archive the change on this branch (openspec archive), so the archive rides the implementing PR; never push to main (owner decision 2026-10-01 (RELEASING.md, Owner settings))
