## 1. Trim internal/render (preserve shared pieces)

- [x] 1.1 `internal/render/renderer.go`: delete `RegistryRenderer` and `PackageReleaseRenderer`; keep the `ModuleRenderer`/`ReleaseRenderer` interfaces; fix imports
- [x] 1.2 `internal/render/module.go`: delete `RenderModuleFromRegistry` and `extractModuleFromRelease`; keep `type RenderResult` and `buildInventoryEntries`; fix imports (drop pkg/module, pkg/loader, internal/synthesis, cue/cuecontext as they become unused)
- [x] 1.3 `internal/render/release.go`: delete `RenderLoadedModuleRelease` and `LoadReleaseFromPath`; keep `KindModuleRelease`/`KindBundleRelease` and `ErrUnsupportedKind`; fix imports
- [x] 1.4 Update stale doc comments in `kernel_module_renderer.go`/`kernel_release_renderer.go` referencing the deleted impls

## 2. Fix the live nil-renderer fallback

- [x] 2.1 `internal/reconcile/release.go`: remove the `renderer == nil → render.PackageReleaseRenderer{}` fallback (and its comment); rely on the injected renderer

## 3. Delete orphaned packages

- [x] 3.1 Delete `pkg/render/` (whole package + tests)
- [x] 3.2 Delete `pkg/module/` (whole package + tests)
- [x] 3.3 Delete `pkg/validate/` (whole package + tests)
- [x] 3.4 Delete `internal/synthesis/` (whole package + tests) — removes the `CatalogVersion = "v1.3.4"` pin

## 4. Remove fork-based integration tests

- [x] 4.1 Delete `test/integration/reconcile/e2e_registry_test.go` (uses `RegistryRenderer`)
- [x] 4.2 Delete `test/integration/reconcile/runtime_identity_test.go` (uses `RegistryRenderer`); if it asserts something not covered by a kernel-renderer test, re-add that assertion against `KernelModuleRenderer`
- [x] 4.3 Delete `test/integration/reconcile/synthesis_test.go` (uses `internal/synthesis`)

## 5. Verify + validation gates

- [x] 5.1 `grep -r "opm-operator/pkg/render\|opm-operator/pkg/module\|opm-operator/pkg/validate\|opm-operator/internal/synthesis\|RegistryRenderer\|PackageReleaseRenderer" --include=*.go .` returns no hits
- [x] 5.2 Confirm `CatalogVersion`/`v1.3.4` no longer appears anywhere
- [x] 5.3 `task dev:fmt dev:vet`
- [x] 5.4 `task dev:lint`
- [x] 5.5 `task dev:test`
