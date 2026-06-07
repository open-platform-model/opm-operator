## 1. Status reason

- [x] 1.1 Add `PlatformNotReadyReason = "PlatformNotReady"` in `internal/status/conditions.go` (alongside `DependenciesNotReadyReason`)

## 2. Gate the reconcile loop

- [x] 2.1 In `internal/reconcile/modulerelease.go`, before the existing render-error branch, add `if errors.Is(err, render.ErrPlatformNotReady)`: `MarkNotReady`/`Ready=False` with `PlatformNotReadyReason`, set outcome to a blocked (non-stalled) classification, emit a warning event, `return ctrl.Result{RequeueAfter: ...}, nil`
- [x] 2.2 Ensure the blocked path applies nothing and prunes nothing (return before the apply/prune phases)

## 3. Re-enqueue on platform change

- [x] 3.1 Add `mapPlatformToModuleReleases` map func in `internal/controller/modulerelease_controller.go` (list all `ModuleReleases`, enqueue each)
- [x] 3.2 In `SetupWithManager`, add `Watches(&releasesv1alpha1.Platform{}, handler.EnqueueRequestsFromMapFunc(r.mapPlatformToModuleReleases))`
- [x] 3.3 Add `platforms` get/list/watch RBAC marker on the ModuleRelease controller; `task dev:manifests`

## 4. Wire the renderer

- [x] 4.1 In `cmd/main.go`, replace `Renderer: &render.RegistryRenderer{}` for the ModuleRelease reconciler with `Renderer: &render.KernelModuleRenderer{Kernel: k, Store: platformStore, Registry: registry, RuntimeName: <controller identity>}`
- [x] 4.2 Leave the `Provider` field and the legacy provider load in place (still used by the Release reconciler)

## 5. Tests + validation gates

- [x] 5.1 Envtest: materialized platform in the store + a `ModuleRelease` for a published module → renders and applies via the kernel; inventory/status as expected
- [x] 5.2 Envtest: no platform → `ModuleRelease` goes `Ready=False`/`PlatformNotReady`, nothing applied or pruned
- [x] 5.3 Envtest: apply a `Platform` after a blocked `ModuleRelease` → the release is re-enqueued and applies on the next reconcile
- [x] 5.4 `task dev:fmt dev:vet`
- [x] 5.5 `task dev:lint`
- [x] 5.6 `task dev:test`
