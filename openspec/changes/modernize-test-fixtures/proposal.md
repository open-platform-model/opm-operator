# Proposal: modernize-test-fixtures

## Why

The kernel cutover left the test fixtures behind: `test/fixtures/modules/hello` (and `test/fixtures/releases/hello`) are still authored against the retired `opmodel.dev/core/v1alpha1@v1` / `opmodel.dev/opm/v1alpha1@v1` schema era, while the kernel resolves `opmodel.dev/core@v0` and materializes transformers from `opmodel.dev/catalogs/opm@v0`. The published old-era fixture no longer loads through the kernel — `task dev:test:local` currently fails (`Module Acquisition Integration: import failed … missing ',' in argument list`), and the kernel-renderer happy-path spec can never run because the kernel-era catalog is absent from the local registry. The only registry-backed verification of the production render path is red or skipped.

## What Changes

- Rewrite `test/fixtures/modules/hello` against `opmodel.dev/core@v0`: embed `m.#Module`, keep the minimal "renders a single ConfigMap" shape by attaching the catalog's `#ConfigMaps` component (`opmodel.dev/catalogs/opm/resources`), matched by the catalog's `configmap-transformer` (which has `requiredLabels: {}` — no workload-type label needed). Dependencies become `opmodel.dev/core@v0` + `opmodel.dev/catalogs/opm@v0` (mirroring `library/testdata/modules/web_app`, the authoritative kernel-era module example).
- Bump the fixture version to `v0.0.2` and update its references (`acquire_test.go` metadata assertions, `kernel_module_renderer_test.go` render coordinates, `test/fixtures/modules/hello/modulerelease.yaml`). Bumping instead of republishing `v0.0.1` avoids stale-artifact and CUE module-cache (`~/.cache/cue`) poisoning on developer machines.
- Make the kernel-era deps locally resolvable. **Correction discovered during implementation:** the proposal originally assumed `opmodel.dev/core@v0` was already published locally — it was not (the local `opmodel.dev/core` held only `v1.0.x` tags), and neither was the catalog. Both `opmodel.dev/core@v0 v0.4.0` (from `core/`) and `opmodel.dev/catalogs/opm@v0 v0.5.0` (from `catalog_opm/`) are published to `localhost:5000`. This requires a `cue` CLI at the kernel's language version (v0.17.x); the workspace's v0.16.1 CLI cannot read the v0.17.0 catalog.
- Update the `test-registry-lifecycle` spec: the "fixture catalog imports unchanged" requirement is inverted (imports move to core@v0/catalogs-opm@v0), and the registry-contents requirement gains both kernel-era modules.
- **Acceptance (revised — see Impact):** `task dev:test` (CI/ghcr) green with registry-gated specs skipping; the module fixture is kernel-era and concrete-evaluates correctly; the `KernelModuleRenderer` happy-path spec *runs* instead of skipping (the catalog now resolves). Full `task dev:test:local` green is **deferred** — it is blocked by a pre-existing production bug the modernized fixture exposed (see Impact), tracked in a separate follow-up change.

### Descoped during implementation

- **Release fixture rewrite** (`test/fixtures/releases/hello`) — reverted to old-era and moved to the moduleacquire follow-up. A hand-authored core@v0 release package (`#module: hello`) hits the same self-referential `#Module` constraint that blocks acquisition, and the canonical core@v0 release-package shape is unresolved.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `test-registry-lifecycle`: fixture module schema era moves from `core/v1alpha1@v1` + `opm/v1alpha1@v1` to `core@v0` + `catalogs/opm@v0`; published fixture coordinates move to `v0.0.2`; the local registry contract additionally requires `opmodel.dev/catalogs/opm@v0` to be present so acquisition and platform materialization resolve locally.

## Impact

- **API types / controllers:** none. No production Go code changes. Test fixtures, two integration test files' coordinates/assertions, `.tasks` documentation for the publish prerequisites, and one spec delta.
- **SemVer:** PATCH (test-and-tooling only; no shipped behavior change).
- **Dependencies:** local dev flow gains one-time prerequisites — publish `core@v0` and `catalogs/opm@v0` to the local registry, using a `cue` CLI at v0.17.x. CI (`task dev:test`) is unaffected: registry-gated specs continue to skip there.
- **Discovered production bug (drives the split):** modernizing the fixture exposed that `internal/moduleacquire/shim.go` cannot load **any** core@v0 `#Module`. Its shim writes a throwaway package that embeds the target module via `import mod; mod`, which re-evaluates `#Module`; core@v0's self-referential `metadata.modulePath`/`version` are then rejected as `field not allowed` — the same admission failure `library/opm/helper/synth/release.go` documents and works around in Go. This was previously masked by the old fixture's parse error. It fails `acquire` and the `KernelModuleRenderer` happy-path. The fix (a Go scope-trick in `moduleacquire`, or a kernel "load module from registry" API) is production kernel-integration work with its own tests, tracked in the **`fix-moduleacquire-core-v0`** follow-up change. `task dev:test:local` goes green once that lands.
- **Out of scope (follow-up changes):** the moduleacquire core@v0 fix + release-fixture rewrite (`fix-moduleacquire-core-v0`); wiring `OPM_TEST_CATALOG_PATH` into `dev:test:local` to activate the platform success-path/recovery specs (catalog-test-harness); triaging the nine pre-kernel stub specs; Release-renderer happy-path coverage; kernel-era e2e; restoring `docs/TESTING.md`.
- **Complexity:** none added to shipped code — fixture content swap, version bump, and documented publish prerequisites (Principle VII). Scope fits a single short session (Principle VIII).
