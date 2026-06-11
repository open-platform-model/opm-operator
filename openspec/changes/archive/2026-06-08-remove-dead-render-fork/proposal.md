## Why

Both consumer CRs now render through the kernel — `cmd/main.go` wires only `KernelModuleRenderer` and `KernelReleaseRenderer`, and the legacy `RegistryRenderer`/`PackageReleaseRenderer` implementations are unwired dead code. This slice (stage 5a of fork removal) deletes the now-orphaned render fork: the old renderer implementations, the `pkg/render`/`pkg/module`/`pkg/validate` packages, and `internal/synthesis` — which removes the hardcoded `CatalogVersion = "v1.3.4"` pin. It is pure dead-code removal: nothing live depends on these mechanisms once a single nil-renderer fallback and the fork-based integration tests are cleaned up. The provider plumbing (`pkg/provider`, `internal/catalog`, `pkg/loader`, the `prov` parameter and `Provider` fields) is intentionally left for stage 5b, which is a signature refactor rather than deletion.

## What Changes

- **Trim `internal/render`, preserving the shared pieces the kernel renderers reuse.** Delete `RegistryRenderer`, `PackageReleaseRenderer`, `RenderModuleFromRegistry`, `RenderLoadedModuleRelease`, `extractModuleFromRelease`, and `LoadReleaseFromPath`. Keep `type RenderResult` and `buildInventoryEntries` (`module.go`), `KindModuleRelease`/`KindBundleRelease` and `ErrUnsupportedKind` (`release.go`), and the `ModuleRenderer`/`ReleaseRenderer` interfaces (`renderer.go`) — all consumed by `KernelModuleRenderer`/`KernelReleaseRenderer`.
- **Delete the orphaned fork packages**: `pkg/render`, `pkg/module`, `pkg/validate` (their only importers were the deleted impls and each other).
- **Delete `internal/synthesis`** (its only importer was the deleted `RenderModuleFromRegistry`) — this removes the `CatalogVersion = "v1.3.4"` pin and the temp-module synthesis entirely.
- **Fix the live nil-renderer fallback**: `internal/reconcile/release.go` currently defaults `renderer = render.PackageReleaseRenderer{}` when the injected renderer is nil. Remove the fallback (production always injects `KernelReleaseRenderer`; a nil renderer is a programming error). The analogous ModuleRelease path has no such fallback.
- **Remove the fork-based integration tests** that exercise the deleted mechanisms: `test/integration/reconcile/{e2e_registry_test.go, runtime_identity_test.go, synthesis_test.go}` (they construct `&render.RegistryRenderer{}` / use `internal/synthesis`). The kernel renderers carry their own tests; any still-relevant runtime-identity assertion is re-covered there.
- Update stale doc comments in `kernel_module_renderer.go`/`kernel_release_renderer.go` that reference the now-deleted impls.

**Out of scope (stage 5b):** removing the `prov *provider.Provider` parameter from the `ModuleRenderer`/`ReleaseRenderer` interfaces and the kernel renderers; removing the `Provider` field from the reconcilers/params/controllers; deleting `pkg/provider`, `internal/catalog`, and `pkg/loader`; removing the provider load in `main.go`. Also out of scope: BundleRelease.

## Capabilities

### New Capabilities

None.

### Removed Capabilities

- `cue-rendering`: the directory-and-values fork render bridge (`internal/render.RenderModule`, `pkg/render`) is deleted; rendering is now `Kernel.Compile` via the kernel renderers (`kernel-module-renderer`, `platform-gated-rendering`, `release-kernel-rendering`), which already cover rendering output, inventory entries, and values conversion.

### Modified Capabilities

- `module-release-synthesis`: the "Release synthesis" requirement (the temp-module-with-catalog-pin mechanism) is removed; release construction is now kernel `SynthesizeRelease` over a module acquired by `module-acquisition`. The capability's other requirements (CR spec shape, registry config, reconcile behavior, status, BundleRelease, e2e) are unchanged.
- `module-renderer-interface`: the "Production wiring" requirement (which mandated `Renderer: &render.RegistryRenderer{}`) is removed — superseded by `platform-gated-rendering`'s requirement that the manager wires `KernelModuleRenderer`. The interface itself is preserved.

## Impact

- **Code (deletions)**: `pkg/render/`, `pkg/module/`, `pkg/validate/`, `internal/synthesis/`; the old impls/functions in `internal/render/{module,release,renderer}.go`; the three fork integration tests.
- **Code (edits)**: `internal/reconcile/release.go` (drop nil-renderer fallback); doc comments in the kernel renderers.
- **Preserved**: `RenderResult`, `buildInventoryEntries`, kind consts, `ErrUnsupportedKind`, the renderer interfaces; `pkg/core`, `pkg/resourceorder`; and (for 5b) `pkg/provider`, `pkg/loader`, `internal/catalog`.
- **APIs/CRDs**: none.
- **Enhancement**: stage 5a of 0001's fork removal; kills the `v1.3.4` catalog pin. Stage 5b completes provider removal.
- **SemVer**: PATCH/MINOR — internal dead-code removal; no API or behavior change (the live render paths already run on the kernel).
- **Complexity justification (Principle VII)**: deleting dead code is the simplification; the only live edits are removing one fallback and obsolete tests so the tree still compiles.
