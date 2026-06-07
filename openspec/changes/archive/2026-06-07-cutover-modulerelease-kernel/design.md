## Context

This is stage 3 of the render-core swap: the behavioral cut-over of `ModuleRelease` onto the kernel. The prior slices left everything in place but unwired — `KernelModuleRenderer` (build, behind the `ModuleRenderer` interface), `internal/moduleacquire.Acquire`, and `internal/platform.Store` (populated by the live `PlatformReconciler`). `cmd/main.go` still injects `RegistryRenderer`.

Confirmed wiring from the current tree:

- `ModuleReleaseReconciler` already holds `Kernel *kernel.Kernel`; `cmd/main.go` already constructs `k` and `platformStore`. The renderer swap is a one-line change to the `Renderer:` field; the renderer carries the store, so no reconciler struct field is added.
- `internal/reconcile/modulerelease.go` calls `params.Renderer.RenderModule(...)` and, on error, sets `reason := RenderFailedReason` (or `ResolutionFailedReason` when `isResolutionError`), then `MarkStalled` + `return ctrl.Result{RequeueAfter: StalledRecheckInterval}, nil`. The gate inserts a `render.ErrPlatformNotReady` branch ahead of that.
- `ModuleReleaseReconciler.SetupWithManager` currently does `For(&ModuleRelease{}).Named("modulerelease")` with no extra watches. The re-enqueue adds a `Watches(&Platform{}, ...)`.
- `internal/status` has a `DependenciesNotReadyReason` — the precedent for "blocked on a dependency," which `PlatformNotReady` follows.

## Goals / Non-Goals

**Goals:**

- ModuleRelease renders via `KernelModuleRenderer` against the store-held materialized platform.
- "No platform yet" is a `PlatformNotReady` blocked state (nothing applied/pruned), distinct from render failure.
- Blocked releases re-enqueue promptly when the platform materializes.

**Non-Goals:**

- `Release` CR cut-over; fork deletion; removing the provider field/load; BundleRelease.
- Changing the existing `RenderFailed`/`ResolutionFailed`/apply/prune/status behavior.
- Fixing the platform reconciler's transient-`MaterializeError` no-retry (separate slice; noted as an interaction).

## Decisions

### Renderer swap in main.go; reconciler struct unchanged

**Decision:** Construct `KernelModuleRenderer{Kernel: k, Store: platformStore, Registry: registry, RuntimeName: <controller identity>}` in `cmd/main.go` and assign it to `Renderer:`. Leave the reconciler's `Provider` field and the legacy provider load in place.

**Rationale:** The renderer owns the store, so the seam stays the existing `ModuleRenderer` interface — a one-line swap, no struct churn, trivially revertible. The provider load is still needed by the `Release` reconciler until its cut-over; removing it now would be premature.

**Alternatives considered:** add a `Store` field to the reconciler and build the renderer there — unnecessary indirection; the renderer is the natural owner.

### PlatformNotReady is a blocked state, not a stall

**Decision:** Branch on `errors.Is(err, render.ErrPlatformNotReady)` and set `Ready=False` with reason `PlatformNotReady`, apply/prune nothing, requeue — modelled on `DependenciesNotReadyReason`, not `MarkStalled`.

**Rationale:** A missing platform is a transient dependency gap an admin resolves by applying the `Platform`, not a semantic defect in the release. Treating it as a terminal stall would mislabel healthy releases and muddy the `Stalled` signal (Principle V). The Platform watch plus requeue make recovery automatic.

**Alternatives considered:** reuse `MarkStalled`/`RenderFailedReason` — wrong semantics, conflates "release is broken" with "waiting for platform"; `MarkReconciling` only — loses the explicit blocked reason on `Ready`.

### Re-enqueue via a Platform watch, included in this slice

**Decision:** Add `Watches(&Platform{}, EnqueueRequestsFromMapFunc(mapPlatformToModuleReleases))`; the map lists all `ModuleReleases` and enqueues them.

**Rationale:** Without it, a release blocked on `PlatformNotReady` only retries on `StalledRecheckInterval` backoff, so a freshly-applied platform leaves releases idle for up to that interval. The watch makes unblocking immediate and lands the §8.3 deferred behavior. It is small and cohesive with the gate (the gate creates the blocked state the watch clears), so they ship together.

**Alternatives considered:** rely on backoff only (simpler, but sluggish UX and leaves §8.3 unfinished — could be a separate slice if this one feels large); owner-reference/index-based enqueue (overkill for a single cluster-singleton platform — list-all is fine at this scale).

## Risks / Trade-offs

- **Behavioral change to an exercised path** — ModuleRelease output now comes from the platform, not the startup provider. Mitigation: envtest covering render+apply with a materialized platform, and the blocked path with none; the change is revertible (swap the renderer back).
- **Interaction with the platform reconciler's transient-failure handling** — if `Materialize` fails transiently, the platform stays `Stalled` (no self-heal today) so the store stays empty and every ModuleRelease sits in `PlatformNotReady`. Called out in the proposal; the fix is a separate slice but this cut-over is what makes it user-visible.
- **List-all re-enqueue on every Platform change** — negligible: one cluster-singleton platform, changes are rare (generation predicate), and the release count is bounded.

## Migration Plan

1. `internal/status`: add `PlatformNotReadyReason`.
2. `internal/reconcile/modulerelease.go`: insert the `render.ErrPlatformNotReady` branch (Ready=False/PlatformNotReady, no apply/prune, event, requeue) ahead of the generic render-error handling.
3. `internal/controller/modulerelease_controller.go`: add `mapPlatformToModuleReleases` and the `Watches(&Platform{}, ...)` in `SetupWithManager`; add `platforms` get/list/watch RBAC.
4. `cmd/main.go`: swap `Renderer:` to `KernelModuleRenderer`.
5. Envtest: (a) materialized platform → ModuleRelease renders+applies; (b) no platform → `PlatformNotReady`, nothing applied; (c) apply platform after a blocked release → release re-enqueued and applied.
6. Validation gates.

**Rollback:** revert the `main.go` renderer swap (back to `RegistryRenderer`); the gate branch and watch are inert without the kernel renderer producing `ErrPlatformNotReady`, but revert the whole commit for cleanliness.

## Open Questions

- The exact `RuntimeName` value for `KernelModuleRenderer` (the controller's runtime identity string the kernel stamps into transformer context). Reuse whatever identity the legacy path passed as the render runtime name; confirm the constant during implementation.
