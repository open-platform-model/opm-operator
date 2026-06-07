## Why

`kernel-module-renderer` built a complete kernel-backed `KernelModuleRenderer` behind the `ModuleRenderer` interface, but left it unwired — `cmd/main.go` still injects `RegistryRenderer`, so `ModuleRelease` reconciliation still runs on the pre-0001 fork. This slice performs the behavioral cut-over: it makes the ModuleRelease reconciler render through the kernel against the materialized platform held in `internal/platform.Store`, and it teaches the reconcile loop to treat "no platform yet" as a blocked-on-dependency state rather than a render failure. This is the first slice where the platform store gets a live reader and where applying a `ModuleRelease` without a `Platform` produces the §8.2 inert behavior.

## What Changes

- **Wire**: in `cmd/main.go`, construct `&render.KernelModuleRenderer{Kernel: k, Store: platformStore, Registry: registry, RuntimeName: <controller runtime identity>}` and set it as the `ModuleReleaseReconciler.Renderer`, replacing `&render.RegistryRenderer{}`. Both `k` and `platformStore` already exist in `main.go`; no reconciler struct field changes (the renderer holds the store).
- **Gate**: in `internal/reconcile/modulerelease.go`, before the existing render-error branch (which maps to `RenderFailed`/`ResolutionFailed` → `MarkStalled`), add a branch on `errors.Is(err, render.ErrPlatformNotReady)`: set `Ready=False` with a new `PlatformNotReady` reason, apply and prune nothing, emit an event, and requeue. This is a blocked-on-dependency state (modelled on the existing `DependenciesNotReadyReason`), not a terminal stall.
- **Re-enqueue**: in `ModuleReleaseReconciler.SetupWithManager`, add `Watches(&Platform{}, handler.EnqueueRequestsFromMapFunc(mapPlatformToModuleReleases))` so that when the `Platform` becomes ready (generation change), all `ModuleReleases` are re-enqueued and blocked ones retry promptly instead of only on backoff. This lands the §8.3 "re-enqueue all releases on platform change" behavior that was deferred until releases read the store.
- **New status reason** `PlatformNotReadyReason = "PlatformNotReady"` in `internal/status`.

After this slice, `ModuleRelease` runs entirely on the kernel. The `Provider` field on the reconciler and the legacy provider load in `main.go` remain (the `Release` CR still uses the fork until its own cut-over); `KernelModuleRenderer` already ignores the `*provider.Provider` parameter.

**Out of scope (later slices):** the `Release` CR cut-over (`KernelReleaseRenderer`); deleting the fork (`pkg/render`, `pkg/module`, `pkg/loader`, `pkg/validate`, `internal/synthesis`, `internal/catalog`, `pkg/provider`) and the legacy provider load/flags; BundleRelease. Also out of scope: fixing the platform reconciler's transient-`MaterializeError` no-retry behavior (tracked separately) — relevant because a platform stuck `Stalled` keeps the store empty and holds `ModuleReleases` in `PlatformNotReady`.

## Capabilities

### New Capabilities

- `platform-gated-rendering`: the ModuleRelease reconciler renders through the kernel against the materialized platform from the store; when no platform is materialized it blocks with a `PlatformNotReady` condition and applies nothing; and it re-enqueues blocked releases when the platform becomes ready.

### Modified Capabilities

None — the new gate branch is additive (a new error case alongside the existing `RenderFailed`/`ResolutionFailed` handling, which is unchanged). The renderer swap is a wiring change, not a spec-level redefinition of the existing render path.

## Impact

- **Code**: `cmd/main.go` (renderer swap); `internal/reconcile/modulerelease.go` (gate branch); `internal/controller/modulerelease_controller.go` (`SetupWithManager` Platform watch + a `mapPlatformToModuleReleases` map func); `internal/status` (`PlatformNotReadyReason`).
- **Dependencies**: requires `kernel-module-renderer` implemented (provides `KernelModuleRenderer` + `render.ErrPlatformNotReady`); the platform store and shared Kernel already exist.
- **Behavioral change**: `ModuleRelease` now resolves and renders via the library kernel and the materialized platform instead of the fork + startup provider. Applying a `ModuleRelease` with no `Platform` present is now inert (blocked, nothing applied) where previously it rendered against the startup-loaded provider.
- **APIs/CRDs**: none; new RBAC for the ModuleRelease controller to `get`/`list`/`watch` `platforms` (for the watch + map).
- **Enhancement**: completes 0001 §8.2 (inert-until-platform) and §8.3 (re-enqueue on platform change) for `ModuleRelease`.
- **SemVer**: MINOR — additive condition + watch; behavior of an alpha CRD shifts to the intended 0001 model.
- **Complexity justification (Principle VII)**: gate + re-enqueue are tightly coupled to the cut-over (without the watch, blocked releases only retry on backoff); kept in one cohesive slice. The watch map func is the minimal mechanism for prompt unblocking.
