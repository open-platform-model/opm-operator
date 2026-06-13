## Why

`render-acquire-module` (registry path → `*module.Module`) and `add-platform-reconciler` (materialized platform held in `internal/platform.Store`) gave the operator both inputs the library kernel needs to render a `ModuleRelease`. This slice assembles them into a `KernelModuleRenderer` that implements the existing `ModuleRenderer` interface entirely via the kernel — the heart of the render-core swap for `ModuleRelease`. It is built and tested end-to-end but deliberately not yet wired: `cmd/main.go` keeps using `RegistryRenderer`, so reconcile behavior is unchanged until the following slice flips the wiring and adds platform-readiness gating.

## What Changes

- New `KernelModuleRenderer` (in `internal/render`) implementing `ModuleRenderer`. It holds the shared `*kernel.Kernel`, the `*platform.Store`, and the registry mapping; it ignores the legacy `*provider.Provider` parameter (kept only so the interface is unchanged this slice — the provider type is deleted in the fork-removal slice). `RenderModule` chains: read `*MaterializedPlatform` from the store; `moduleacquire.Acquire(ctx, k, modulePath, moduleVersion, registry)` → `*module.Module`; convert `*RawValues` to a `cue.Value` (via `k.CueContext().CompileBytes`; zero value when no values, letting `#config` defaults apply); `Kernel.SynthesizeRelease(synth.ReleaseInput{Module, Name, Namespace, Values})`; `Kernel.Compile(kernel.CompileInput{ModuleRelease, Platform: mp, RuntimeName})`; adapt `out.Compiled` to `[]*core.Resource`; build inventory entries; return `*RenderResult`.
- New `core.ResourceFromCompiled(*librarycore.Compiled) *core.Resource` adapter in `pkg/core` — a field copy (`Value`, `Release`, `Component`, `Transformer` are identical on both types), folded in here because it is trivial and inert alone. The operator's existing `Resource` methods and `ToUnstructured()` stay.
- When the store holds no materialized platform, `RenderModule` returns a typed `ErrPlatformNotReady`. The reconciler-side mapping of that error to a CR condition is the next slice; this slice only defines the renderer's behavior.
- Reuse, not reinvention: `buildInventoryEntries` (→ `r.ToUnstructured()` → `inventory.NewEntryFromResource`) is unchanged and consumed as-is; `RenderResult` is unchanged.

**Out of scope (later slices):** wiring `KernelModuleRenderer` into the ModuleRelease reconciler (still `RegistryRenderer`); mapping `ErrPlatformNotReady` to a `PlatformNotReady`/`NoPlatform` CR condition and the release-gating behavior; the `Release` CR renderer (`KernelReleaseRenderer`); deleting the fork (`pkg/render`, `pkg/module`, `pkg/loader`, `pkg/validate`, `internal/synthesis`, `internal/catalog`) and the legacy provider load/flags; BundleRelease.

## Capabilities

### New Capabilities

- `kernel-module-renderer`: render a `ModuleRelease` into `[]*core.Resource` + inventory entries entirely through the library kernel — store-held materialized platform + acquired module → `SynthesizeRelease` → `Compile` → `Compiled`→`Resource` adapter — behind the existing `ModuleRenderer` interface, returning `ErrPlatformNotReady` when no platform is materialized.

### Modified Capabilities

None — additive. The legacy `cue-rendering` / `module-release-synthesis` path (`RegistryRenderer`) is unchanged and still drives reconciliation this slice.

## Impact

- **Code**: new `internal/render/kernel_module_renderer.go`; new `core.ResourceFromCompiled` in `pkg/core`. No change to `cmd/main.go`, reconcilers, or the legacy render path.
- **Dependencies**: this slice depends on `render-acquire-module` (`internal/moduleacquire.Acquire`) and `add-platform-reconciler` (`internal/platform.Store`) being implemented; uses library `kernel.SynthesizeRelease`, `kernel.Compile`, `kernel.CompileInput`/`CompileResult`, `core.Compiled`, `synth.ReleaseInput`.
- **APIs/CRDs**: none.
- **Tests**: integration test (reuse `test-registry-lifecycle` with a published module + a materialized platform in the store) asserting `KernelModuleRenderer.RenderModule` returns the expected resources, provenance, and inventory entries; unit test for `ResourceFromCompiled`; test that an empty store yields `ErrPlatformNotReady`.
- **Enhancement**: implements the core of 0001 §5.2's render rewrite for `ModuleRelease`; consumes the prior two slices' outputs.
- **SemVer**: MINOR — additive renderer + adapter; no behavior change (unwired).
- **Complexity justification (Principle VII)**: the renderer is the natural atomic unit of the swap (one implementation behind one interface for one CR kind); the adapter is a field copy, not an abstraction.
