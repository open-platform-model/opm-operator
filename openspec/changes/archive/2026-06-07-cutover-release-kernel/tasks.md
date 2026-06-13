## 1. KernelReleaseRenderer

- [x] 1.1 Create `internal/render/kernel_release_renderer.go`: `KernelReleaseRenderer{Kernel *kernel.Kernel; Store *platform.Store; Registry string; RuntimeName string}` implementing `ReleaseRenderer`
- [x] 1.2 Detect package `kind`; `KindBundleRelease` → return `(KindBundleRelease, nil, ErrUnsupportedKind)` (resolve the kind-peek mechanism per design open question, preferring the kernel context)
- [x] 1.3 `KindModuleRelease`: `Kernel.LoadReleasePackage(ctx, packageDir, file.LoadOptions{Registry})` → `Kernel.NewReleaseFromValue` (no values, no SynthesizeRelease)
- [x] 1.4 `Store.Get()`; if absent return `(kind, nil, render.ErrPlatformNotReady)` before Compile
- [x] 1.5 `Kernel.Compile(kernel.CompileInput{ModuleRelease: rel, Platform: mp, RuntimeName})` → adapt `out.Compiled` via `core.ResourceFromCompiled` → `buildInventoryEntries` → return `(KindModuleRelease, *RenderResult, nil)`

## 2. Gate the reconcile loop

- [x] 2.1 In `internal/reconcile/release.go` `renderReleasePackage`, before `renderErrorReason`/`MarkStalled`, add `if errors.Is(err, render.ErrPlatformNotReady)`: `Ready=False` with `status.PlatformNotReadyReason`, blocked (non-stalled) classification, warning event, requeue; apply/prune nothing

## 3. Re-enqueue on platform change

- [x] 3.1 Add `mapPlatformToReleases` map func in `internal/controller/release_controller.go` (list all `Releases`, enqueue each), mirroring `mapSourceToReleases`
- [x] 3.2 In `SetupWithManager`, add `Watches(&releasesv1alpha1.Platform{}, handler.EnqueueRequestsFromMapFunc(r.mapPlatformToReleases))`
- [x] 3.3 Add `platforms` get/list/watch RBAC marker on the Release controller; `task dev:manifests`

## 4. Wire the renderer

- [x] 4.1 In `cmd/main.go`, replace the Release reconciler's `Renderer: render.PackageReleaseRenderer{}` with `Renderer: &render.KernelReleaseRenderer{Kernel: k, Store: platformStore, Registry: registry, RuntimeName: core.LabelManagedByControllerValue}`
- [x] 4.2 Leave the `Provider` field and legacy provider load in place (removed at fork deletion)

## 5. Tests + validation gates

- [x] 5.1 Envtest: materialized platform + a `kind: ModuleRelease` package → renders and applies via the kernel; inventory/status as expected
- [x] 5.2 Envtest: no platform → `Release` goes `Ready=False`/`PlatformNotReady`, nothing applied or pruned
- [x] 5.3 Envtest: apply a `Platform` after a blocked `Release` → re-enqueued and applies on the next reconcile
- [x] 5.4 Envtest: a `kind: BundleRelease` package → `UnsupportedKind`, nothing applied
- [x] 5.5 `task dev:fmt dev:vet`
- [x] 5.6 `task dev:lint`
- [x] 5.7 `task dev:test`
