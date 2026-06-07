## Why

`cutover-modulerelease-kernel` moved the `ModuleRelease` CR onto the kernel; the `Release` CR is still on the fork (`PackageReleaseRenderer` → `pkg/render`). This slice completes the consumer-side swap by cutting `Release` over too: it renders the Flux-fetched release package through the kernel against the materialized platform, and gates it on platform readiness exactly as `ModuleRelease` now is. After this, both release-bearing CRs run on the kernel and only the fork-deletion slice remains before the legacy runtime can go.

It is lighter than the `ModuleRelease` cut-over: the `Release` package is already on disk (Flux artifact — no module acquisition), and the `Compiled→Resource` adapter, `ErrPlatformNotReady`, and the `PlatformNotReady` gating pattern already exist. So build + wire + gate land as one cohesive slice.

## What Changes

- **Build `KernelReleaseRenderer`** (in `internal/render`) implementing the existing `ReleaseRenderer` interface (`Render(ctx, packageDir, prov) (kind string, result *RenderResult, err error)`). It detects the package `kind`; for `KindModuleRelease` it loads the release in the kernel's context via `Kernel.LoadReleasePackage(packageDir, {Registry})` → `Kernel.NewReleaseFromValue` → reads the `*MaterializedPlatform` from `internal/platform.Store` → `Kernel.Compile(CompileInput{ModuleRelease, Platform, RuntimeName})` → adapts `Compiled`→`Resource` (reusing `core.ResourceFromCompiled`) → builds inventory entries (reusing `buildInventoryEntries`). For `KindBundleRelease` it returns `ErrUnsupportedKind` (unchanged). No `SynthesizeRelease` and no values injection — a `Release` package is an authored `#ModuleRelease` that already carries its values (mirrors today's `RenderLoadedModuleRelease`, which passes nil values).
- **Gate**: in `internal/reconcile/release.go` `renderReleasePackage`, branch on `errors.Is(err, render.ErrPlatformNotReady)` ahead of the generic `renderErrorReason`/`MarkStalled` path: set `Ready=False` with the existing `PlatformNotReadyReason`, apply/prune nothing, emit an event, and requeue (blocked-on-dependency, not a stall).
- **Re-enqueue**: in `ReleaseReconciler.SetupWithManager`, add `Watches(&Platform{}, EnqueueRequestsFromMapFunc(mapPlatformToReleases))` (mirroring the existing `mapSourceToReleases`) so releases blocked on `PlatformNotReady` retry promptly when the platform materializes.
- **Wire**: in `cmd/main.go`, swap the Release reconciler's `Renderer` from `render.PackageReleaseRenderer{}` to `&render.KernelReleaseRenderer{Kernel: k, Store: platformStore, Registry: registry, RuntimeName: core.LabelManagedByControllerValue}` (same runtime identity the wired `KernelModuleRenderer` uses). The legacy provider load stays until fork deletion.

**Out of scope (later slices):** deleting the fork (`pkg/render`, `pkg/module`, `pkg/loader`, `pkg/validate`, `internal/synthesis`, `internal/catalog`, `pkg/provider`) and the legacy provider load/flags; BundleRelease implementation (still `ErrUnsupportedKind`); the platform-reconciler transient-`MaterializeError` retry fix (separate slice — but the same availability interaction applies: a stalled platform blocks Releases too).

## Capabilities

### New Capabilities

- `release-kernel-rendering`: the `Release` reconciler renders its Flux-fetched `#ModuleRelease` package through the kernel against the materialized platform, blocks with `PlatformNotReady` (applying nothing) when no platform is materialized, re-enqueues on platform readiness, and continues to reject `BundleRelease` with `UnsupportedKind`.

### Modified Capabilities

None — additive renderer + gate + watch. The artifact-fetch, path navigation, apply/prune/status, and `BundleRelease` rejection behaviors are unchanged.

## Impact

- **Code**: new `internal/render/kernel_release_renderer.go`; `internal/reconcile/release.go` (`renderReleasePackage` gate branch); `internal/controller/release_controller.go` (`mapPlatformToReleases` + `Watches(&Platform{})`); `cmd/main.go` (renderer swap). Reuses `core.ResourceFromCompiled`, `render.ErrPlatformNotReady`, `status.PlatformNotReadyReason`, `buildInventoryEntries`.
- **Dependencies**: requires `cutover-modulerelease-kernel` (provides `PlatformNotReadyReason` + the gating pattern + the `mapPlatform…` precedent) and `kernel-module-renderer` (adapter + `ErrPlatformNotReady`, archived). The platform store and shared Kernel already exist in `main.go`.
- **Behavioral change**: `Release` now renders via the kernel + materialized platform instead of the fork + startup provider; applying a `Release` with no `Platform` is now inert (blocked).
- **APIs/CRDs**: none; new `platforms` get/list/watch RBAC on the Release controller.
- **Enhancement**: completes 0001 §8.2/§8.3 for the `Release` CR; leaves only fork deletion before the legacy runtime is removable.
- **SemVer**: MINOR — additive renderer + condition + watch.
- **Complexity justification (Principle VII)**: build+wire+gate are one cohesive concern ("Release on the kernel"); each part is small because the patterns and primitives already exist. It can be split into a build slice then a wire+gate slice if preferred, but the reduced surface (no acquisition) makes one slice reasonable.
