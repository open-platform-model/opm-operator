## Why

The OPM prerelease lines (core on the v2 line, catalogs/k8s, library, cli, opm-operator) cut their first beta together, and the operator is the last runtime to cross. Flipping `prerelease-type` to `beta` alone does nothing on a version that already carries a suffix (release-please keeps the old label and proposes `1.0.0-alpha.23`), and the `release-automation` spec still names config `release-as`, the mechanism that bit this workspace twice by being sticky. The README install command also resolves GitHub's Latest alias, which serves the retired v0.7.5 manifest today.

## What Changes

- Move `github.com/open-platform-model/library` to `v1.0.0-beta.1` (`go.mod`, `go.sum`), so the operator renders against the library beta and, through its default schema module, core `v2.0.0-beta.1`.
- Set `prerelease-type` to `beta` in `release-please-config.json`. No `release-as` key is added; the manifest and `internal/version/version.go` stay release-please-owned.
- The work PR's squash commit is the release carrier: type `fix(deps)`, footer `Release-As: 1.0.0-beta.1`. It retitles the open release PR #161 from `1.0.0-alpha.23` to `1.0.0-beta.1`.
- Modify `release-automation`: manual overrides are a one-shot `Release-As:` commit footer and never config `release-as`; the beta line, its promise and its GA exit are stated; the Release PR scenario names the beta counter; the stale 0.x bump-minor-pre-major scenario (contradicted by `bump-minor-pre-major: false`), the docs-is-not-releasable claim (contradicted by the visible Documentation section) and the changelog section list are corrected; the 0.1.0 initial-version baseline is retired.
- README: `opm operator install` as the primary path, else the tagged asset URL `.../releases/download/<tag>/install.yaml` with a link to the Releases page, never `releases/latest`; note that `:latest` tracks beta builds.
- `docs/site/operating/install-the-operator.md`: pairing text, prerelease/Latest note and version examples move to the beta line.
- Governance docs carry the beta promise (canon wording, stable-lines rule and GA clause included): `CONSTITUTION.md` Principle VI and its proposal-shaping rule, `openspec/config.yaml` Principle VI and `rules.proposal`, and `AGENTS.md`. The "state MAJOR/MINOR/PATCH" rules gain a pre-GA note: a proposal states its class as it would after GA and notes that beta ships it as the next `-beta.N`. `AGENTS.md` and the `release-automation` beta requirement also state that an operator `feat!` the released cli cannot drive merges only after the cli release that can drive it, and that no minor or major hop (such as `Release-As: 1.1.0-beta.1`) happens during beta.
- Separate `test(fixtures)` PR (the supervisor-patch sample Platform PR), no footer: `config/samples/opmodel.dev_v1alpha1_platform.yaml` pins catalogs k8s `1.0.0-beta.1` and opm `4.4.4`, applied from the operator hunk of the supervisor's post-G3 `task deps:update` run (which includes the platform-pins step; no separate `deps:pins:platform-pins` run).

Release class: the operator goes from `1.0.0-alpha.22` to `1.0.0-beta.1`, a prerelease-label step on the 1.0.0 line. No API type, CRD, controller or reconcile behavior changes; the CRD API stays `v1alpha1` until GA.

SemVer: prerelease-identifier change on 1.0.0; not a MAJOR, MINOR or PATCH bump.

This change's deliverable is itself a release, so `tasks.md` carries the push, PR and release-hold steps (the documented exception in `openspec/config.yaml` rules.tasks).

Out of scope, referenced only: the fixture republish track FX (root `deps:pins:fixtures`, run once and unconditionally by the supervisor after cli PR-C and this repo's sample Platform PR merge; its operator `test(fixtures)` PR also hand-edits the `test/fixtures/catalog.go` `CatalogVersion` default, and it is the supervisor's PR, not part of this change); the workflow opm CLI pins (root `deps:pins:opm-cli` after G6); the active change `add-cross-namespace-source-grants`, which is not touched.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-automation`: override mechanism becomes the one-shot `Release-As:` footer; beta prerelease line, promise and GA exit added; bump determination restated per release line; releasable-commit set, changelog sections and Release PR constant scenario aligned with the config; initial version baseline retired.

## Impact

- Code: `go.mod`, `go.sum`, `release-please-config.json`, `README.md`, `docs/site/operating/install-the-operator.md`, `CONSTITUTION.md`, `openspec/config.yaml`, `AGENTS.md`, `config/samples/opmodel.dev_v1alpha1_platform.yaml` (separate PR).
- Release: #161 becomes `chore(main): release 1.0.0-beta.1` once the carrier merges and must stay open until G4 (cli `v1.0.0-beta.1` released). Merging it publishes image `v1.0.0-beta.1`, moves `:latest`, and uploads a digest-pinned `install.yaml` (G5).
- Downstream: the cli embeds this operator after G5 (`fix(deps): embed opm-operator v1.0.0-beta.1`).
- Versions: the targets named here (`1.0.0-beta.1`, `4.4.4`, `v2.0.0-beta.1`) are expected gate versions; if a gate lands on a different version (burned tag), the recorded gate version replaces it everywhere.
- Dependencies: requires G2 (library `v1.0.0-beta.1` on the Go proxy) and G3 (catalogs `k8s-v1.0.0-beta.1`, `opm-v4.4.4` on GHCR).
