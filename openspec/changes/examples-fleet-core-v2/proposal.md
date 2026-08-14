# Proposal: examples-fleet-core-v2

> Companion to `operator-library-retarget` (slice `operator-library-retarget` of enhancement `0010`): the CUE fixture fleet crosses to core v2 and republishes. Depends on the retarget change; the operator release that ships both is the slice's graduation gate.

## Why

The operator's test fixtures are the workspace's published module test artifacts: `test/fixtures/modules/{hello,hello-web,podinfo,redis}` publish to GHCR as `opmodel.dev/modules/test/*` on every release (`publish-examples` job) and on every e2e run (`-e2e.g<sha>` prerelease tags), and the CLI's `render-parity` and `instance-handoff` tests pull them by exact version. All eight fixture modules (four `modules/`, four `modulepackages/` mirrors) are core-v1-authored — after the retarget they render `no matching transformer` against a v2 platform, and downstream cannot un-gate until v2-authored artifacts are published.

Two traps make this more than an import sweep. **Immutability**: `examples:publish` reads each module's declared `metadata.version` (by grep) and treats an existing registry tag as success — a v2 reauthoring without version bumps publishes nothing, silently. **Schema impossibility**: `hello-web` cannot exist on the v2 line under its current name — the hyphen violates `#SnakeNameType` and the leaf-equals-name assertion. The fixture **renames to `hello_web`**: a new name is a new registry path, so the already-published hyphenated artifacts are untouched, and the source-side half of 0011 `registry-cleanup`'s D17 item 2 is delivered early (recorded cross-slice; the registry-side residue — relocating/deleting the old `hello-web` artifacts — stays with 0011).

## What Changes

- **All four fixture modules reauthored to core v2** (`hello`, `hello_web`, `podinfo`, `redis`): `cue.mod` deps → `opmodel.dev/core@v2` (`v2.0.0-alpha.4`) + `opmodel.dev/catalogs/opm@v2` (`v2.0.0-alpha.3`); imports → `core@v2` and the D49 versioned catalog packages (`…/resources/v1beta1`, `…/blueprints/v1beta1`, `…/traits/v1beta1` — all 11 import sites); `metadata.modulePath` becomes the full major-suffixed address (`opmodel.dev/modules/test/podinfo@v0`); hand-authored `core.opmodel.dev/workload-type` labels removed (the blueprint stamps the transitional duplicate; the v2 exemplar `modules/jellyfin` authors none).
- **`hello-web` renames to `hello_web`**: directory, `cue.mod` module line (`opmodel.dev/modules/test/hello_web@v0`), `metadata.name`/`modulePath`, `moduleinstance.yaml` coordinate — the CUE package inside was already `hello_web`, so the source rename is near-cosmetic. `.tasks/release.yaml` `PKGS`, the `kernel_package_renderer_test` loop, and the `example-test-modules` spec enumerate the new name. The published hyphenated `hello-web@v0` artifacts remain untouched until 0011's `registry-cleanup`.
- **Versions bump so the publish actually ships**: `hello` `0.0.4` → `0.0.5`, `hello_web` continues its line at `0.1.3` (first tag under the renamed path — a new registry path has no immutability conflict, and keeping the lineage's next patch number keeps history legible), `podinfo` `0.1.3` → `0.1.4`, `redis` `0.1.6` → `0.1.7` — patch bumps on the existing `@v0` line, following this repo's own v0→v1 precedent (the crossing commit kept the major and moved the patch). `moduleinstance.yaml` pins updated to match (fixing podinfo's already-stale `v0.1.2` pin and the two-patches-stale `config/samples` ModuleInstance).
- **Four `modulepackages/` mirrors follow**: core dep → v2, embedded test-module pins → the bumped versions; the `hello-web` mirror renames to `opmodel.dev/releases/test/hello_web@v0` and its instance import drops the explicit `:hello_web` package qualifier (path leaf and package now agree).
- **Publish pipeline fixes**: `examples:bundle`'s `RELEASES_DIR` corrected (`test/fixtures/releases` → `test/fixtures/modulepackages` — the OCIRepository/ModulePackage manifests have never actually been bundled); the version-grep convention documented against the v2 metadata shape.
- **Tests flip to v2 identity**: `acquire_test`'s `ModulePath` assertion moves to the full address (`opmodel.dev/modules/test/hello@v0`); integration version pins bump (`hello v0.0.5`, `redis v0.1.7`); registry-backed specs re-run green against the republished fleet.
- **Identity subpackages NOT added** (0011 D12's publish-gate conformance): the fixtures publish via bare `cue mod publish`, not `opm module publish`; the gate arrives with 0011's cutover slices, and core records absence as an accepted exposure. Recorded as future 0011-side work, not smuggled in here.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `example-test-modules`: the fleet is `hello`, `hello_web`, `podinfo`, `redis` on the core-v2 identity shape; the hyphenated `hello-web` name is retired at the source (registry-side residue owned by 0011 `registry-cleanup`).
- `example-module-publishing`: version bumps are load-bearing on any schema crossing (immutable tags + idempotent publish); the bundle task packages the `modulepackages/` manifests it always claimed to.

## Impact

- **SemVer: PATCH-shaped for the operator binary** (no Go behavior change — `test:`/`chore:` commits); the *published fixture artifacts* gain patch versions on their own lines.
- **API types / controllers**: none.
- **Cross-repo consumers**: the CLI's pins (`render-parity` `v0.1.3`, `ssa-ownership` `v0.1.3`, e2e handoff `cue.mod` `v0.1.3`, `examples/` `podinfo v0.1.3` + `hello-web v0.1.2`) move in `cli-coordinate-adoption` to `podinfo v0.1.4`, examples going podinfo-only — already coordinated with that track. The CLI's vendored `tests/fixtures/modules/podinfo` copy re-vendors independently (drift-by-design).
- **Ordering**: after `operator-library-retarget` merges (the fixtures need the v2-capable operator for the registry-backed tiers to go green). The release cut follows both; its `publish-examples` job ships the v2 artifacts, un-gating the CLI's pulling tests.
- **Registry note**: fixtures republish under `opmodel.dev/modules/test/*` (the namespace 0011 plans to relocate to `testing.opmodel.dev`) — accepted: relocation is `registry-cleanup`'s scope and the pulling consumers pin the current namespace; four new patch tags (one on a new `hello_web` path) do not widen 0011's cleanup beyond its inventory, and the `hello_web` publish delivers D17 item 2's rename half early (recorded cross-slice).
