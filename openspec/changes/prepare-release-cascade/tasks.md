## 1. Pin the opm CLI in `.opm-cli-version` (beta.2 to beta.4)

- [ ] 1.1 Add repo-root `.opm-cli-version` containing the single line `v1.0.0-beta.4`
- [ ] 1.2 Replace the `go install github.com/open-platform-model/cli/cmd/opm@v1.0.0-beta.2` step in `.github/workflows/test.yml:58-59` with the read-validate-install block from design.md ("`.opm-cli-version` and how workflows read it")
- [ ] 1.3 Same replacement in `.github/workflows/test-e2e.yml:97-98` and `.github/workflows/publish-fixtures.yml:65-66`
- [ ] 1.4 Same replacement in `.github/workflows/release.yml:271-274` (`publish-examples`, which checks out the release tag), keeping its comment about the gated publish pipeline
- [ ] 1.5 Confirm `grep -rn 'cli/cmd/opm@v' .github/workflows` returns nothing and `grep -rn 'v1.0.0-beta.2' .github/workflows` no longer names the cli
- [ ] 1.6 Install the pinned CLI locally (`go install github.com/open-platform-model/cli/cmd/opm@$(cat .opm-cli-version)`) and run `task examples:check` against it; if a fixture now fails a publish gate, stop and record the failure in design.md instead of editing `hack/fixtures.sh`
- [ ] 1.7 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(deps): read the opm CLI version from .opm-cli-version and move it to v1.0.0-beta.4`

## 2. G1 release-pin gate

- [ ] 2.1 Add `hack/release-pin-check.sh` (executable) implementing the four checks in design.md ("The four checks"): replace directives, OPM Go pseudo-versions and untagged versions via `git ls-remote`, `-0.dev.` pins in `test/fixtures/modules/*` and `test/fixtures/modulepackages/*` cue.mods, tracked `cue.mod/local-module.cue`; report every failure, exit non-zero if any
- [ ] 2.2 Add `.tasks/deps.yaml` with task `release-check` (desc names G1 and workspace RELEASING.md, section "Gates") running the script, and include it from `Taskfile.yml` as `deps`
- [ ] 2.3 Add the step `Release-pin gate (G1, release PRs only)` to the `lint` job in `.github/workflows/lint.yml` after "Install Task", with `if: startsWith(github.head_ref || github.ref_name, 'release-please--')` and `run: task deps:release-check`
- [ ] 2.4 Run `task deps:release-check` on the current tree: it passes (no replace, library `v1.0.0-beta.1` is a tag, no dev pins, no tracked `local-module.cue`)
- [ ] 2.5 Negative checks in a scratch copy, not committed: add `replace github.com/open-platform-model/library => ../library` to go.mod; set the library pin to `v1.0.0-beta.9`; add `v: "v2.0.0-0.dev.1"` to `test/fixtures/modules/hello/cue.mod/module.cue`; `git add -f` a `cue.mod/local-module.cue`. Each run fails and names the offender; restore the tree
- [ ] 2.6 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(release): gate release PRs on reproducible OPM pins`

## 3. Leave OPM Go modules to the cascade

- [ ] 3.1 Add `ignore: [{dependency-name: "github.com/open-platform-model/*"}]` to the `gomod` entry of `.github/dependabot.yml`, with a comment that the release cascade owns these bumps (workspace RELEASING.md, section "Pin classes"); keep the `build` prefix and the `kubernetes` group
- [ ] 3.2 Validate the YAML parses (`yq . .github/dependabot.yml` or `python3 -c 'import yaml,sys; yaml.safe_load(open(sys.argv[1]))' .github/dependabot.yml`)
- [ ] 3.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(deps): leave OPM Go module bumps to the release cascade`

## 4. Stop doc-only commits from releasing

- [ ] 4.1 Set `"hidden": true` on the `docs` entry of `release-please-config.json:23`; leave `refactor` visible and leave CHANGELOG.md untouched
- [ ] 4.2 Validate the JSON (`jq . release-please-config.json`)
- [ ] 4.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(release): hide the docs changelog section so doc-only commits do not release`
