## Why

The OPM prerelease lines (core on the v2 line, catalogs/k8s, library, cli, opm-operator) cut their first beta together, and the operator is the last runtime to cross. Flipping `prerelease-type` to `beta` alone does nothing on a version that already carries a suffix (release-please keeps the old label and proposes `1.0.0-alpha.23`), and the `release-automation` spec still names config `release-as`, the mechanism that bit this workspace twice by being sticky. The README install command also resolves GitHub's Latest alias, which serves the retired v0.7.5 manifest today.

## What Changes

- Move `github.com/open-platform-model/library` to `v1.0.0-beta.1` (`go.mod`, `go.sum`), so the operator renders against the library beta and, through its default schema module, core `v2.0.0-beta.1`.
- Set `prerelease-type` to `beta` in `release-please-config.json`. No `release-as` key is added; the manifest and `internal/version/version.go` stay release-please-owned.
- The work PR's squash commit is the release carrier: type `fix(deps)`, footer `Release-As: 1.0.0-beta.1`. It retitles the open release PR #161 from `1.0.0-alpha.23` to `1.0.0-beta.1`.
- Modify `release-automation`: manual overrides are a one-shot `Release-As:` commit footer and never config `release-as`; the beta line, its promise and its GA exit are stated; the Release PR scenario names the beta counter; the stale 0.x bump-minor-pre-major scenario (contradicted by `bump-minor-pre-major: false`) and the docs-is-not-releasable claim (contradicted by the visible Documentation section) are corrected.
- README: install from a tagged release asset URL or `opm operator install`, never `releases/latest`; note that `:latest` tracks beta builds.
- `docs/site/operating/install-the-operator.md`: pairing text, prerelease/Latest note and version examples move to the beta line.
- `internal/version`: doc-comment and test-message examples read `1.0.0-beta.1`. The `Version` constant is not touched.
- Separate `test(fixtures)` PR, no footer: `config/samples/opmodel.dev_v1alpha1_platform.yaml` pins catalogs k8s `1.0.0-beta.1` and opm `4.4.4`, applied from a supervisor patch (root `platform-pins` run after G3).

Release class: the operator goes from `1.0.0-alpha.22` to `1.0.0-beta.1`, a prerelease-label step on the 1.0.0 line. No API type, CRD, controller or reconcile behavior changes; the CRD API stays `v1alpha1` until GA.

This change's deliverable is itself a release, so `tasks.md` carries the push, PR and release-hold steps (the documented exception in `openspec/config.yaml` rules.tasks).

Out of scope, referenced only: the fixture republish track (root `deps:pins:fixtures` plus the `test/fixtures/catalog.go` `CatalogVersion` default, supervisor-run after G3); the workflow opm CLI pins (root `deps:pins:opm-cli` after G6); the active change `add-cross-namespace-source-grants`, which is not touched.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-automation`: override mechanism becomes the one-shot `Release-As:` footer; beta prerelease line, promise and GA exit added; bump determination restated per release line; releasable-commit set and Release PR constant scenario aligned with the config.

## Impact

- Code: `go.mod`, `go.sum`, `release-please-config.json`, `README.md`, `docs/site/operating/install-the-operator.md`, `internal/version/version.go` (comment only), `internal/version/version_test.go` (message only), `config/samples/opmodel.dev_v1alpha1_platform.yaml` (separate PR).
- Release: #161 becomes `chore(main): release 1.0.0-beta.1` once the carrier merges and must stay open until G4 (cli `v1.0.0-beta.1` released). Merging it publishes image `v1.0.0-beta.1`, moves `:latest`, and uploads a digest-pinned `install.yaml` (G5).
- Downstream: the cli embeds this operator after G5 (`fix(deps): embed opm-operator v1.0.0-beta.1`).
- Dependencies: requires G2 (library `v1.0.0-beta.1` on the Go proxy) and G3 (catalogs `k8s-v1.0.0-beta.1`, `opm-v4.4.4` on GHCR).
