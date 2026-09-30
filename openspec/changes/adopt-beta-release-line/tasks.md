# Tasks: adopt-beta-release-line

Worktree `opm-operator/.claude/worktrees/beta-adopt-beta-release-line`, branch `beta/adopt-beta-release-line` (carrier). Every command runs inside the worktree. Every registry-backed test run exports the mapping on two lines, `export CUE_REGISTRY='opmodel.dev=ghcr.io/open-platform-model,testing.opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'` then `export OPM_REGISTRY="$CUE_REGISTRY"`, plus `OPM_TEST_REGISTRY_FORCE=1`, `OPM_TEST_CATALOG_PATH='opmodel.dev/catalogs/opm@v4'` and an absolute `KUBEBUILDER_ASSETS`. Commit messages: no bare at-sign, no body line starting with a word followed by an opening parenthesis, no `Release-As:` footer in any branch commit, only the plain `Co-Authored-By: Claude <noreply@anthropic.com>` trailer. This change's deliverable is a release, so the push, PR and hold steps below are part of it (rules.tasks exception). Never merge a PR; never run a root workspace task.

## Gates

Supervisor ticks each after confirming it; a worker never ticks these.

- [ ] G1 core `v2.0.0-beta.1` on GHCR (context only; reaches this repo through the library)
- [ ] G2 library `v1.0.0-beta.1` resolvable on the Go proxy (blocks section 1)
- [ ] G3 catalogs `k8s-v1.0.0-beta.1` and `opm-v4.4.4` on GHCR (blocks section 5 and the carrier merge)
- [ ] G4 cli `v1.0.0-beta.1` released with goreleaser assets, templates 1.0.3 on GHCR (blocks merging #161)
- [ ] G5 opm-operator `v1.0.0-beta.1`: signed image, `install.yaml` asset, GitHub Release flagged Pre-release (produced by merging #161)

## 1. Move to library v1.0.0-beta.1 (go.mod)

- [ ] 1.1 After G2: `go get github.com/open-platform-model/library@v1.0.0-beta.1 && go mod tidy`. Verify: `git diff --stat` touches only `go.mod` and `go.sum`, and `go list -m github.com/open-platform-model/library` prints `v1.0.0-beta.1`.
- [ ] 1.2 Spike for the design.md risk (library beta.1 against the unrepublished fixtures): `task dev:fmt dev:vet dev:lint dev:test` with the registry exported. Verify: green with no registry-backed spec skipped. A red run stops the change here: report the failing specs and do not work around them (the fixture republish track may have to merge first).
- [ ] 1.3 Gates of 1.2 green, then commit `fix(deps): move to library v1.0.0-beta.1` (body: the library beta renders against core v2.0.0-beta.1 through its default schema module).

## 2. Switch release-please to the beta label

- [ ] 2.1 In `release-please-config.json`, set `"prerelease-type": "beta"` in package `"."`; leave `versioning`, `prerelease`, `bump-minor-pre-major` and every other key as is. Do not add `release-as`; do not touch `.release-please-manifest.json` or the `Version` constant. Verify: `jq -e '.packages["."]["prerelease-type"] == "beta"' release-please-config.json` and `! grep -qi 'release-as' release-please-config.json`; `git diff --stat` touches only that file.
- [ ] 2.2 `task dev:fmt dev:vet dev:lint dev:test` green (config-only; confirms the tree), then commit `chore(release): switch the prerelease label to beta` (body: the label flip alone keeps counting alpha; the carrier squash footer crosses the line).

## 3. README and install page on the beta line

- [ ] 3.1 `README.md` Installing: replace the `releases/latest/download/install.yaml` command with `opm operator install` and the tagged form `kubectl apply --server-side -f https://github.com/open-platform-model/opm-operator/releases/download/v1.0.0-beta.1/install.yaml`, with one sentence saying why `releases/latest` must not be used (GitHub's Latest skips Pre-releases and serves the retired v0.7.5 manifest). The `:latest` row of the tag table says it tracks the newest release, betas included. Verify: `grep -n 'releases/latest' README.md` finds only the warning sentence.
- [ ] 3.2 `docs/site/operating/install-the-operator.md` (authoring comments only, page structure unchanged): step 1 pairing becomes opm-operator `v1.0.0-beta.1` embedded by the first cli release that pins it (expected `v1.0.0-beta.2`, verify at publish time); replace the "likely returns 404" note with the Latest-serves-v0.7.5 fact; step 3 sample versions become opm `4.4.4` and k8s `1.0.0-beta.1`; the check section's OPERATOR example becomes `v1.0.0-beta.1`. Verify: `grep -n 'alpha' docs/site/operating/install-the-operator.md` returns nothing, and no em-dash was added.
- [ ] 3.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs: install a tagged operator release instead of the Latest alias`.

## 4. Version doc examples (internal/version)

- [ ] 4.1 `internal/version/version.go`: the `Full` doc comment example becomes `"v1.0.0-beta.1"`. `internal/version/version_test.go`: the `TestVersionIsSemver` failure message example becomes `1.0.0-beta.1`. The line carrying `x-release-please-version` stays byte-identical. Verify: `git diff -U0 internal/version/version.go` shows no change on the `const Version` line; `go test ./internal/version` passes.
- [ ] 4.2 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `chore(version): use a beta example in the version docs`.

## 5. Sample Platform fixture PR (separate branch, supervisor patch)

- [ ] 5.1 SUPERVISOR PATCH: after G3 the supervisor runs the root `deps:pins:platform-pins` task on the main checkouts and hands over `opm-operator-platform-pins.patch`. Create `git -C <repo> worktree add <repo>/.claude/worktrees/beta-sample-platform-beta-pins -b beta/sample-platform-beta-pins origin/main` and `git apply` the patch there. Verify: `git diff --stat` touches only `config/samples/opmodel.dev_v1alpha1_platform.yaml`, which pins catalogs opm `4.4.4` and k8s `1.0.0-beta.1`; the comments in that file are unchanged except where a version is named.
- [ ] 5.2 `task dev:fmt dev:vet dev:lint dev:test` green on that branch, then commit `test(fixtures): pin the sample Platform to catalogs opm 4.4.4 and k8s 1.0.0-beta.1`. No footer. Do not add the `test/fixtures/catalog.go` `CatalogVersion` default: it belongs to the supervisor-run fixture republish track (`deps:pins:fixtures`), not to this PR.

## 6. Verification (carrier branch)

- [ ] 6.1 `task dev:manifests dev:generate` leaves no diff (no API type changed), then `task dev:fmt dev:vet dev:lint dev:test` green on the branch head with no registry-backed spec skipped.
- [ ] 6.2 `openspec validate adopt-beta-release-line --strict` passes, and run the opsx verify skill on this change. Verify: no CRITICAL finding; `git diff origin/main --stat` lists only `go.mod`, `go.sum`, `release-please-config.json`, `README.md`, `docs/site/operating/install-the-operator.md`, `internal/version/version.go`, `internal/version/version_test.go` and `openspec/`; nothing under `openspec/changes/add-cross-namespace-source-grants/` changed.
- [ ] 6.3 Mechanical checks, all silent: `git diff origin/main -- .release-please-manifest.json CHANGELOG.md config/ test/`; `grep -rni 'release-as' release-please-config.json`; `git log origin/main..HEAD --format=%B | grep -i '^release-as'`; `git log origin/main..HEAD --format=%B | grep -nE '(^|[^[:alnum:]_])@[[:alnum:]]'`.

## 7. Archive the change

- [ ] 7.1 `openspec archive adopt-beta-release-line --yes` (the Gates and section 8 boxes are still open by design). Verify: `openspec/specs/release-automation/spec.md` now carries "Beta prerelease line" and "Version bump determination per release line", no longer carries "Version bump determination from Conventional Commits", and keeps every other scenario heading; `openspec validate --specs --strict` passes. No `enhancement.yaml` exists, so no delivery logging.
- [ ] 7.2 Commit `chore(openspec): archive adopt-beta-release-line`.

## 8. Open the PRs

Supervisor merges both; a worker never merges. PR bodies stay under 250 words of prose, with no commit list, out-of-scope section or test-plan list.

- [ ] 8.1 Carrier PR. Push `git push -u origin beta/adopt-beta-release-line` and open it titled `fix(deps): adopt the beta release line on library v1.0.0-beta.1`. Squash type `fix(deps)`; carrier YES; the supervisor writes the squash message from design.md (Reconcile phase impact block) ending in the footer block `Release-As: 1.0.0-beta.1` then `Co-Authored-By: Claude <noreply@anthropic.com>`. Merge gate: G2 and G3. Expected release PR: #161 retitled `chore(main): release 1.0.0-beta.1`, touching only `.release-please-manifest.json`, `CHANGELOG.md` and `internal/version/version.go`.
- [ ] 8.2 Fixture PR. Push `beta/sample-platform-beta-pins` and open it titled `test(fixtures): pin the sample Platform to catalogs opm 4.4.4 and k8s 1.0.0-beta.1`. Squash type `test(fixtures)`; carrier NO, no footer (the supervisor greps the squash body for `release-as`, case-insensitive, and aborts on a hit). Merge gate: G3. Expected release PR: none (hidden type; #161 unchanged by it).
- [ ] 8.3 SUPERVISOR, after the carrier merges: read the merge commit body (`gh pr view <N> --json mergeCommit`, `git show -s --format=%B`), confirm #161 now reads `chore(main): release 1.0.0-beta.1`, and hold #161 until G4. Merging #161 after G4 makes G5; then the cli embeds the operator (`fix(deps): embed opm-operator v1.0.0-beta.1`) and, after G6, the root `deps:pins:opm-cli` bumps this repo's four workflow pins in a separate `ci` PR.
