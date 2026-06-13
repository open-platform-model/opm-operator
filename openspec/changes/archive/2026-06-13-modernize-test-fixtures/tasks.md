# Tasks: modernize-test-fixtures

## 1. Rewrite the module fixture (core@v0)

- [x] 1.1 Rewrite `test/fixtures/modules/hello/module.cue`: import `m "opmodel.dev/core@v0"`, embed `m.#Module`, `metadata: {modulePath: "testing.opmodel.dev/modules", name: "hello", version: "0.0.2", description}`, keep `#config: {message: string | *"hello from opm"}` and `debugValues` (model on `library/testdata/modules/web_app/module.cue`). Dropped `defaultNamespace` (not a core@v0 field).
- [x] 1.2 Rewrite `test/fixtures/modules/hello/components.cue`: import `res "opmodel.dev/catalogs/opm/resources"`, component `hello` embeds `res.#ConfigMaps` with `spec: configMaps: hello: data: message: #config.message` (no workload-type labels — `configmap-transformer` has `requiredLabels: {}`)
- [x] 1.3 Regenerate `test/fixtures/modules/hello/cue.mod/module.cue` deps via `cue mod tidy`: pins `opmodel.dev/core@v0 v0.4.0` + `opmodel.dev/catalogs/opm@v0 v0.5.0` (catalog published locally first — see 2.1). Required a `cue` CLI at v0.17.x; the system v0.16.1 cannot read the v0.17.0 catalog. **Update:** subsequently re-pinned to `catalogs/opm@v0 v0.5.1` by `fix-moduleacquire-core-v0` after the catalog `immutable` concreteness fix shipped — the working-tree file reads `v0.5.1`.
- [x] 1.4 Validate: `cue eval . --concrete` inside the fixture dir resolves full metadata (fqn, uuid) cleanly — the module is correct standalone.

## 2. Local registry prerequisites + publish

- [x] 2.1 Publish the kernel-era deps to the local registry. **The proposal's premise that `core@v0` was already local was wrong** — neither dep was present. Published `opmodel.dev/core@v0 v0.4.0` (from `core/`) and `opmodel.dev/catalogs/opm@v0 v0.5.0` (from `catalog_opm/`, `task publish VERSION=v0.5.0`) to `localhost:5000`. Both repos clean at their respective tags. Requires a `cue` CLI at v0.17.x.
- [x] 2.2 Bump `MODULE_VERSION` default in `.tasks/module.yaml` from `v0.0.1` to `v0.0.2`
- [x] 2.3 Publish the fixture: `task module:publish`; `testing.opmodel.dev/modules/hello` tag `v0.0.2` confirmed in the local registry
- [x] 2.4 Document the prerequisites: extended the prerequisites comment block in `.tasks/module.yaml` (core@v0 + catalog@v0 publish steps, v0.17 cue requirement) and the `skipIfNoTestRegistry` doc comment in `test/integration/reconcile/registry_helpers_test.go`

## 3. Update test coordinates to v0.0.2

- [x] 3.1 `test/integration/reconcile/acquire_test.go`: acquire version argument → `"v0.0.2"`, `mod.Metadata.Version` assertion → `"0.0.2"`
- [x] 3.2 `test/integration/reconcile/kernel_module_renderer_test.go`: render coordinates → `"testing.opmodel.dev/modules/hello@v0", "v0.0.2"`
- [x] 3.3 `test/fixtures/modules/hello/modulerelease.yaml`: `spec.module.version` → `v0.0.2`

## 4. Release fixture — DESCOPED to the moduleacquire follow-up

- [~] 4.1 **Descoped.** Rewriting `release.cue` to core@v0 (`#module: hello`) hits the same self-referential `#Module` metadata constraint that blocks module acquisition (`field not allowed` at `cue vet`). The canonical hand-authored core@v0 release-package shape is unresolved, and the Release happy-path has no test harness. The release fixture is reverted to its old-era state and moved to the `fix-moduleacquire-core-v0` follow-up change, which must resolve the self-reference loading story first. (Design D5 superseded.)
  - **Status (2026-06-13):** `fix-moduleacquire-core-v0` also deferred this (its tasks 4.1/4.2) and is now archived, so `test/fixtures/releases/hello` remains old-era and **unowned by any active change**. The module-acquisition fix unblocks it (the immutable gap is gone; the `#module` self-reference question via `LoadReleasePackage` → `Compile` is now investigable). Needs a **dedicated follow-up change** — do not let it fall through the cracks.

## 5. Validation gates

- [x] 5.1 `task dev:test` (ghcr/CI mapping) — green; registry-gated specs skip (CI parity preserved).
- [x] 5.2 `task dev:test:local` — **green end to end.** Was blocked by the acquire embedding bug (core@v0 `#Module` self-reference → `field not allowed`); that landed via `fix-moduleacquire-core-v0` (acquisition now delegates to `Kernel.LoadModuleFromRegistry`) and the catalog `immutable` concreteness fix (`catalogs/opm@v0.5.1`). With the fixture re-pinned to v0.5.1, all packages incl. `test/integration/reconcile`'s registry-gated acquire + renderer specs pass. Confirmed 2026-06-13.
- [x] 5.3 `task dev:fmt dev:vet dev:lint` — clean.
