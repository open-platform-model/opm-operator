# Release units, commit types and the beta line

Covers which commit type releases the operator, the two release units (operator and operator module), the beta rule for breaking changes, the release gates, and operator versions on `docs/site/` pages. The cascade workflows and pins are not covered here.

## Contents

- Commit type decides the release
- Two release units
- Module release gate and bot PRs
- Beta line
- Release gate G1 and immutable releases
- Versions on site pages
- Pitfalls
- Check before you finish

## Commit type decides the release

release-please hides `docs`, `chore`, `test`, `ci` and `build` (`release-please-config.json`). `feat`, `fix`, `deps`, `perf`, `revert` and `refactor` release. Reason (owner decision 2026-10-01, workspace `RELEASING.md`): a docs-only commit must not cut an operator release that cascades into the cli.

| Commit type | Releases the operator |
| --- | --- |
| `feat`, `fix`, `deps`, `perf`, `revert`, `refactor` | yes |
| `docs`, `chore`, `test`, `ci`, `build` | no |

- An OPM `go.mod` bump (library) is `fix(deps)`, because it changes the image.
- A Dependabot third-party Go bump stays `build(deps)` and releases nothing. A human retitles it `fix(deps)` only for a security fix the image must ship (`.github/dependabot.yml`).
- `config/samples/*`, `hack/*`, `test/fixtures/*` and `internal/source/testdata/*` are not part of the operator install artifact. A pin bump there is `test(fixtures)` and must not release the operator.
- `.opm-cli-version` moves as `ci(deps)` and releases nothing.

## Two release units

| Unit | Tag | Changelog | Version rule |
| --- | --- | --- | --- |
| Operator | `vX.Y.Z` | root `CHANGELOG.md` | beta line, see below |
| Operator module `modules/opm_operator` | `opm_operator-vX.Y.Z` | `modules/opm_operator/CHANGELOG.md` | 0.x rule while the path is the v0 major: a break raises the minor, everything else the patch |

Each unit has its own release PR (`separate-pull-requests`). The operator package excludes `modules/opm_operator`.

- **A PR that changes the module changes nothing else.** Otherwise its squash commit releases both units.
- **One forced exception:** a CRD or RBAC change must regenerate `zz_generated*` in `modules/opm_operator/` in the same PR (`task operator-module:drift` in `test.yml`). That commit lands in both changelogs and opens a module release PR. The release gate holds that PR until an operator release carrying the same `config/` reaches the module.
- **Tooling outside `modules/`** (`hack/operator-module/**`, including the defaults that shape the module's install data) belongs to the operator package. Commit it as `ci` or `build`, never `fix` or `feat`, or it cuts an operator release and no module release.
- **Commits under `modules/`** release the module only. Use `feat(module)`, `fix(module)` or `fix(deps)` for a change the module's users receive; use `build`, `test` or `docs` for one they do not.
- The module names one operator release by version, tag and digest in the module's `operator/` package, never below the minimum-operator-version file in `hack/operator-module/` (the first operator that refuses to reconcile its own instance). The render test refuses an older one.
- The module's core and catalog pins move only through `task -x deps:cascade:module` and its `module/deps` PR, never through `task deps:cascade`. A pin that changes the rendered Deployment selector fails the render test.

## Module release gate and bot PRs

- Module releases wait while `main`'s `config/` differs from the operator release the module deploys. `task operator-module:release-check` (a `Lint` step on the module's release PR) refuses them until the image PR of an operator release carrying that `config/` merges.
- The module's release PR gains an identity-advance commit from `release.yml` (`opm module version set`, the only writer of the module version). Wait for it before merging.
- Only the `module-publish` job of `release.yml` publishes `opmodel.dev/modules/opm_operator`. It lands at `ghcr.io/open-platform-model/opmodel.dev/modules/opm_operator`; `publish.sh` takes every reference from `cue mod resolve`, and `task operator-module:publish:test` checks them offline. A module release is published with `make_latest=false` (enhancement 0021, the operator install artifact is a registry module on its own version train).
- Every operator release opens or updates the `module/operator-image` PR (`fix(deps)`, or `fix(deps)!` for a breaking operator release). `module-image.yml` recovers it by hand.
- `module-deps.yml` runs `task -x deps:cascade:module` on the same `upstream-released` dispatch (dry while `CASCADE_DRY_RUN` is not `false`).
- The bot PR script in `hack/operator-module/` pushes both bot branches with the release App token. It and the two `publish` jobs write plain files only and refuse a symlink, a hard link or a mode change.
- A module draft stranded by a broken script at its tag: dispatch `release.yml` on `main` with `module_tag=opm_operator-vX.Y.Z`. Only the module jobs run, with `main`'s scripts against the tag's tree; "Re-run failed jobs" would replay the tag's script. The dispatch has its own concurrency group per tag, so it never replaces a pending push run. Dispatch only when no Release run is queued or running.

## Beta line

From its first beta, a prerelease line (`opmodel.dev/core@v2`, library, cli, opm-operator) is on the path to GA.

- A breaking change is allowed during beta only as `!` in the PR title (`feat!:`). The squash commit carries only the title (`squash_merge_commit_message: BLANK`), so the title is the CHANGELOG entry. The migration note goes in the PR body, which that entry links.
- It advances the `-beta.N` counter and never moves the module path to a new major.
- Stable lines (`opmodel.dev/catalogs/opm@v4` and the module fleets) keep the normal SemVer rule: a break is a new major. A core beta break that would force a catalogs/opm major needs owner sign-off.
- GA drops the suffix: `prerelease: false` plus a visible carrier commit per package, in dependency order.
- An operator `feat!` that the released cli cannot drive merges only after the cli release that can drive it.
- No minor or major hop (such as a `release-as` of `1.1.0-beta.1`) during beta: every operator release stays `1.0.0-beta.N`, so the cli's MAJOR.MINOR operator-version ceiling never refuses.
- A forced version is a `release-as` key in `release-please-config.json`, set by a normal PR and removed by the next PR once that release is cut. A `Release-As:` footer never reaches `main`. Merge by squash only: `gh pr merge --squash --subject "<PR title> (#N)" --body ''` (until the owner applies the Merge settings of workspace `RELEASING.md`).

## Release gate G1 and immutable releases

- A release PR must pass `task deps:release-check` (G1, workspace `RELEASING.md`). It runs as a step of the `Lint` job (`lint.yml`) on the operator's `release-please--*` PRs, not the module's, whose pins its own gate checks. It fails when the release would ship a `go.mod` `replace`, a pseudo-version or untagged OPM Go pin, a `-0.dev.` pin in a published fixture's `cue.mod`, or a tracked `cue.mod/local-module.cue`.
- Keep the job name `Lint` unique: the ruleset on `main` is to require that check.
- Tags are immutable (workspace `references/releases.md`; ADR-018). A release is a draft until the `publish-release` job of `release.yml` makes it public, after every asset is attached. `:vX.Y.Z` on GHCR is written only when absent.
- Before publish, recover a failed run with "Re-run failed jobs", never "Re-run all jobs". Re-running all re-runs release-please, which skips every downstream job and strands the draft. After publish, release the next version.

## Versions on site pages

Each passage in `docs/site/` that names the operator version sits between `<!-- x-release-please-start-version -->` and `<!-- x-release-please-end -->`.

- Each marker is on its own line, outside code fences, at the content indentation of its block (three spaces inside an ordered-list item; a column-0 comment there ends the list).
- The page is in `release-please-config.json` `extra-files`, so the release PR rewrites every version inside the blocks. A block names no other version.
- A test in `internal/version/` enforces both for the install page under `docs/site/start/`.
- A page names no core, catalog or cli version. It links to where the current one is (`/catalogs/opm/4/`, the releases page).

## Pitfalls

- A squash commit that touches `modules/` and anything else releases both units.
- "Re-run all jobs" after a failed release strands the draft.
- `hack/operator-module/**` committed as `fix` or `feat` cuts an operator release and no module release.
- A site page that names the version outside the release-please markers goes stale at the next release.
- A `Release-As:` footer is ignored; use a `release-as` key in a normal PR.

## Check before you finish

- [ ] The commit type matches the table above for every path the PR touches.
- [ ] A module PR changes only the module (or the forced `zz_generated_*` pair).
- [ ] A breaking change has `!` in the PR title and a migration note in the body.
- [ ] A site page that names the operator version has the release-please markers.
