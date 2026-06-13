## Context

Stage 4 of the render-core swap: cut the `Release` CR onto the kernel, mirroring the `ModuleRelease` cut-over (`cutover-modulerelease-kernel`) but lighter. The `Release` reconcile already fetches and extracts a Flux artifact to a local `packageDir` and calls `ReleaseRenderer.Render(ctx, packageDir, prov)` (`renderReleasePackage` in `internal/reconcile/release.go`). So there is no module acquisition — the release package is on disk — and the reusable primitives (`core.ResourceFromCompiled`, `render.ErrPlatformNotReady`, `status.PlatformNotReadyReason`, `buildInventoryEntries`, the `mapPlatform…` watch pattern) already exist.

Confirmed surface:

- `ReleaseRenderer.Render(ctx, packageDir, prov) (kind string, result *RenderResult, err error)` — returns the detected kind so the reconciler can reject non-ModuleRelease.
- Today's `RenderLoadedModuleRelease` passes **nil values** to `ParseModuleRelease` ("Release CRDs carry no values — the CUE package already specifies them"). The kernel path mirrors this: no `SynthesizeRelease`, no values.
- `Kernel.LoadReleasePackage(ctx, dir, LoadOptions{Registry})` (gated to the release shape) and `Kernel.NewReleaseFromValue(v)` exist; `Compile(CompileInput{ModuleRelease, Platform, RuntimeName})` is the same call ModuleRelease uses.
- `renderReleasePackage` already branches on render error (`renderErrorReason` → `MarkStalled`) and on `kind != KindModuleRelease` (`UnsupportedKindReason`). The gate inserts an `ErrPlatformNotReady` branch ahead of `renderErrorReason`.
- The Release controller already `Watches` OCIRepository/GitRepository/Bucket via `mapSourceToReleases`; the Platform watch mirrors that.
- `cmd/main.go` wires the Release reconciler with `Renderer: render.PackageReleaseRenderer{}` and already passes `Kernel: k`; the ModuleRelease reconciler is already wired to `KernelModuleRenderer{… RuntimeName: core.LabelManagedByControllerValue}` — the same runtime identity applies here.

## Goals / Non-Goals

**Goals:**

- `KernelReleaseRenderer` rendering `kind: ModuleRelease` packages via the kernel + store-held platform.
- Same `PlatformNotReady` gating + Platform watch as `ModuleRelease`.
- `BundleRelease` still rejected with `UnsupportedKind`.

**Non-Goals:**

- Fork deletion / provider removal; BundleRelease implementation; the transient-retry platform fix.
- Any change to artifact fetch, path navigation, apply/prune/status.

## Decisions

### Render in the kernel context via LoadReleasePackage → NewReleaseFromValue → Compile

**Decision:** For `kind: ModuleRelease`, load the package with `Kernel.LoadReleasePackage` (kernel context), build the release with `NewReleaseFromValue`, read the platform from the store, and `Compile`. No `SynthesizeRelease`, no values.

**Rationale:** A `Release` package is an authored `#ModuleRelease`, not a `#Module`, so it is loaded-and-compiled, not synthesized. Loading in the kernel's context is mandatory: `Compile` fills the release against the store's materialized platform, and cross-context fills are illegal — the value must share the Kernel's `*cue.Context`. This mirrors `RenderLoadedModuleRelease`'s nil-values behavior exactly, minus the fork.

**Alternatives considered:** reuse the fork's `LoadReleaseFromPath` (uses a fresh `cuecontext` + `pkg/loader`) — produces a value in the wrong context for the kernel's `Compile`; rejected. Inject values — Release CRs have no values field; not applicable.

### Kind detection alongside the gated loader

**Decision:** Detect the package `kind` and reject `BundleRelease` with `ErrUnsupportedKind` before/around the gated kernel load; only `ModuleRelease` proceeds to `Compile`.

**Rationale:** `Kernel.LoadReleasePackage` shape-gates to the release kind, so a `BundleRelease` package would fail the gate with a less specific error. Detecting kind first preserves today's clean `UnsupportedKind` signal. The renderer must return the kind regardless (the interface and reconciler depend on it).

**Open question (mechanism) — RESOLVED:** kind detection rides on the loader's own shape gate. `Kernel.LoadReleasePackage` gates to the `#ModuleRelease` kind, so a `BundleRelease` package fails with the library's exported `loaderfile.ErrWrongKind` sentinel (its doc explicitly blesses controllers branching on it via `errors.Is` rather than string matching). The renderer calls `LoadReleasePackage` once and, on `ErrWrongKind`, returns `(KindBundleRelease, nil, ErrUnsupportedKind)`. This avoids both a non-gated peek and any re-coupling to the fork loader, and needs only a single package load on the happy path. The chosen direction is cleaner than the tolerated transitional peek.

### Reuse the ModuleRelease gating + watch patterns verbatim

**Decision:** Gate with the existing `PlatformNotReadyReason` and add a `mapPlatformToReleases` watch mirroring `mapSourceToReleases`.

**Rationale:** Consistency across both consumer CRs; no new status vocabulary; the patterns are proven by the ModuleRelease cut-over.

### One slice (build + wire + gate)

**Decision:** Land build, wire, and gate together rather than splitting build from cutover as `ModuleRelease` did.

**Rationale:** The Release renderer is materially smaller (no acquisition; adapter/gate/watch already exist), so the combined surface stays within a cohesive "Release on the kernel" concern. Splittable into a build slice then a wire+gate slice if review prefers.

## Risks / Trade-offs

- **Behavioral change to an exercised path** — Release output now comes from the platform, not the startup provider. Mitigation: envtest covering render+apply with a materialized platform, the blocked path with none, and the BundleRelease rejection; revert is a one-line renderer swap.
- **Stalled-platform interaction** — like ModuleRelease, a platform stuck `Stalled` from a transient `MaterializeError` keeps the store empty and holds Releases in `PlatformNotReady` with no self-heal until the platform fix slice lands. Called out; not fixed here.
- **Kind-detection mechanism** — see the open question; the wrong choice could re-couple to the fork or surface a less clear error for BundleRelease. Resolved at implementation with a test for the BundleRelease path.

## Migration Plan

1. `internal/render/kernel_release_renderer.go`: `KernelReleaseRenderer{Kernel, Store, Registry, RuntimeName}` implementing `Render` (kind detect → ModuleRelease: `LoadReleasePackage` → `NewReleaseFromValue` → store gate → `Compile` → adapt → entries; BundleRelease: `ErrUnsupportedKind`).
2. `internal/reconcile/release.go`: add the `errors.Is(err, render.ErrPlatformNotReady)` branch in `renderReleasePackage` (Ready=False/PlatformNotReady, no apply/prune, event, requeue).
3. `internal/controller/release_controller.go`: `mapPlatformToReleases` + `Watches(&Platform{}, ...)`; add `platforms` get/list/watch RBAC; `task dev:manifests`.
4. `cmd/main.go`: swap the Release `Renderer` to `KernelReleaseRenderer` (RuntimeName `core.LabelManagedByControllerValue`).
5. Envtest: (a) materialized platform + ModuleRelease-kind package → renders+applies; (b) no platform → `PlatformNotReady`, nothing applied; (c) blocked release re-enqueued after platform applied; (d) BundleRelease-kind package → `UnsupportedKind`.
6. Validation gates.

**Rollback:** revert the `main.go` renderer swap (back to `PackageReleaseRenderer`); revert the whole commit for cleanliness.

## Open Questions

- ~~Kind-detection mechanism in the kernel context~~ — **resolved** (see the decision above): branch on `loaderfile.ErrWrongKind` from the gated `LoadReleasePackage`; no peek, no fork re-coupling.
